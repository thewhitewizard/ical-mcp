package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const feedBody = "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"

// fakeClock is a settable clock.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// testFeed is an HTTPS Feed server that counts the requests it receives.
type testFeed struct {
	*httptest.Server
	hits atomic.Int32
}

func newTestFeed(t *testing.T, handler http.HandlerFunc) *testFeed {
	t.Helper()
	f := &testFeed{}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		handler(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

// serveBody answers every request with body.
func serveBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

// feedURL is the address of the Feed: its path carries the secret.
func (f *testFeed) feedURL() string { return f.URL + "/ical/secret-token-123/basic.ics" }

// storeFixture is a feedStore wired to a test Feed, with the logs it wrote.
type storeFixture struct {
	store *feedStore
	clock *fakeClock
	logs  *bytes.Buffer
}

func newTestStore(t *testing.T, feed *testFeed, feedURL string) *storeFixture {
	t.Helper()
	fx := &storeFixture{
		clock: &fakeClock{now: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)},
		logs:  &bytes.Buffer{},
	}
	cfg := Config{CacheTTLSeconds: 60, Calendars: map[string]string{"perso": feedURL}}
	fx.store = newFeedStore(cfg, feed.Client().Transport, fx.clock.Now, log.New(fx.logs, "", 0))
	return fx
}

func TestFeedStore_Get_Success(t *testing.T) {
	t.Parallel()

	feed := newTestFeed(t, serveBody(feedBody))
	fx := newTestStore(t, feed, feed.feedURL())

	body, stale, err := fx.store.Get(context.Background(), "perso")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(body) != feedBody {
		t.Errorf("body = %q, want %q", body, feedBody)
	}
	if stale {
		t.Error("stale = true, want false")
	}
}

func TestFeedStore_Get_Failures(t *testing.T) {
	t.Parallel()

	plain := httptest.NewServer(serveBody(feedBody)) // not https
	t.Cleanup(plain.Close)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		setup   func(fx *storeFixture, feed *testFeed)
		alias   string
		wantErr string
	}{
		{
			name:    "HTTP status",
			handler: func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) },
			wantErr: "HTTP status 404",
		},
		{
			name:    "timeout",
			handler: func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() },
			setup:   func(fx *storeFixture, _ *testFeed) { fx.store.client.Timeout = 50 * time.Millisecond },
			wantErr: "timed out",
		},
		{
			name:    "too large, announced by Content-Length",
			handler: serveBody(strings.Repeat("x", 100)),
			setup:   func(fx *storeFixture, _ *testFeed) { fx.store.maxBytes = 10 },
			wantErr: "too large",
		},
		{
			name: "too large, streamed",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				for range 10 {
					_, _ = w.Write([]byte(strings.Repeat("x", 10)))
					_ = http.NewResponseController(w).Flush()
				}
			},
			setup:   func(fx *storeFixture, _ *testFeed) { fx.store.maxBytes = 50 },
			wantErr: "too large",
		},
		{
			name: "redirect to http",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, plain.URL+"/secret-token-123.ics", http.StatusFound)
			},
			wantErr: "non-https",
		},
		{
			name:    "unreachable",
			handler: serveBody(feedBody),
			setup:   func(_ *storeFixture, feed *testFeed) { feed.Close() },
			wantErr: "unreachable",
		},
		{
			name:    "unknown calendar",
			handler: serveBody(feedBody),
			alias:   "travail",
			wantErr: "unknown calendar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			feed := newTestFeed(t, tt.handler)
			fx := newTestStore(t, feed, feed.feedURL())
			if tt.setup != nil {
				tt.setup(fx, feed)
			}
			alias := tt.alias
			if alias == "" {
				alias = "perso"
			}

			body, stale, err := fx.store.Get(context.Background(), alias)
			if err == nil {
				t.Fatalf("Get() = %q, nil; want an error", body)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
			if body != nil || stale {
				t.Errorf("Get() = (%q, %v), want no data", body, stale)
			}
			for _, out := range []string{err.Error(), fx.logs.String()} {
				if strings.Contains(out, "secret-token-123") {
					t.Errorf("leaks the Feed address: %q", out)
				}
			}
		})
	}
}

func TestFeedStore_Get_FollowsHTTPSRedirect(t *testing.T) {
	t.Parallel()

	feed := newTestFeed(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved.ics" {
			_, _ = w.Write([]byte(feedBody))
			return
		}
		http.Redirect(w, r, "/moved.ics", http.StatusFound)
	})
	fx := newTestStore(t, feed, feed.feedURL())

	body, _, err := fx.store.Get(context.Background(), "perso")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if string(body) != feedBody {
		t.Errorf("body = %q, want %q", body, feedBody)
	}
}

// switchableFeed serves a body that tests can change, or fail on demand.
type switchableFeed struct {
	body atomic.Value // string
	fail atomic.Bool
}

func (s *switchableFeed) handler(w http.ResponseWriter, _ *http.Request) {
	if s.fail.Load() {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	body, _ := s.body.Load().(string)
	_, _ = w.Write([]byte(body))
}

func newSwitchableFeed() *switchableFeed {
	s := &switchableFeed{}
	s.body.Store("v1")
	return s
}

func mustGet(t *testing.T, fx *storeFixture) (string, bool) {
	t.Helper()
	body, stale, err := fx.store.Get(context.Background(), "perso")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	return string(body), stale
}

func TestFeedStore_Get_Cache(t *testing.T) {
	t.Parallel()

	t.Run("a fresh copy is served without downloading", func(t *testing.T) {
		t.Parallel()
		feed := newTestFeed(t, serveBody(feedBody))
		fx := newTestStore(t, feed, feed.feedURL())

		mustGet(t, fx)
		fx.clock.Advance(59 * time.Second)
		body, stale := mustGet(t, fx)

		if got := feed.hits.Load(); got != 1 {
			t.Errorf("downloads = %d, want 1", got)
		}
		if body != feedBody || stale {
			t.Errorf("Get() = (%q, %v), want (%q, false)", body, stale, feedBody)
		}
	})

	t.Run("an expired copy is downloaded again", func(t *testing.T) {
		t.Parallel()
		sw := newSwitchableFeed()
		feed := newTestFeed(t, sw.handler)
		fx := newTestStore(t, feed, feed.feedURL())

		mustGet(t, fx)
		sw.body.Store("v2")
		fx.clock.Advance(60 * time.Second)
		body, stale := mustGet(t, fx)

		if got := feed.hits.Load(); got != 2 {
			t.Errorf("downloads = %d, want 2", got)
		}
		if body != "v2" || stale {
			t.Errorf("Get() = (%q, %v), want (v2, false)", body, stale)
		}
	})

	t.Run("a failure is not cached", func(t *testing.T) {
		t.Parallel()
		sw := newSwitchableFeed()
		sw.fail.Store(true)
		feed := newTestFeed(t, sw.handler)
		fx := newTestStore(t, feed, feed.feedURL())

		if _, _, err := fx.store.Get(context.Background(), "perso"); err == nil {
			t.Fatal("Get() error = nil, want an error")
		}
		sw.fail.Store(false)
		body, stale := mustGet(t, fx)

		if body != "v1" || stale {
			t.Errorf("Get() = (%q, %v), want (v1, false)", body, stale)
		}
	})

	t.Run("an expired copy is served stale when the download fails", func(t *testing.T) {
		t.Parallel()
		sw := newSwitchableFeed()
		feed := newTestFeed(t, sw.handler)
		fx := newTestStore(t, feed, feed.feedURL())

		mustGet(t, fx)
		sw.fail.Store(true)
		fx.clock.Advance(time.Hour)
		body, stale := mustGet(t, fx)

		if body != "v1" || !stale {
			t.Errorf("Get() = (%q, %v), want (v1, true)", body, stale)
		}
		if !strings.Contains(fx.logs.String(), `"perso"`) {
			t.Errorf("logs = %q, want a warning naming the Calendar", fx.logs)
		}
		if strings.Contains(fx.logs.String(), "secret-token-123") {
			t.Errorf("logs leak the Feed address: %q", fx.logs)
		}

		// The stale copy keeps being retried: once the Feed is back, it is fresh.
		sw.fail.Store(false)
		sw.body.Store("v2")
		body, stale = mustGet(t, fx)
		if body != "v2" || stale {
			t.Errorf("after recovery Get() = (%q, %v), want (v2, false)", body, stale)
		}
	})

	t.Run("a canceled call is not answered with a stale copy", func(t *testing.T) {
		t.Parallel()
		sw := newSwitchableFeed()
		feed := newTestFeed(t, sw.handler)
		fx := newTestStore(t, feed, feed.feedURL())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		mustGet(t, fx)
		fx.clock.Advance(time.Hour)
		if _, _, err := fx.store.Get(ctx, "perso"); !errors.Is(err, errCanceled) {
			t.Errorf("with a copy, error = %v, want %v", err, errCanceled)
		}
	})

	t.Run("concurrent calls are safe", func(t *testing.T) {
		t.Parallel()
		feed := newTestFeed(t, serveBody(feedBody))
		fx := newTestStore(t, feed, feed.feedURL())

		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				if _, _, err := fx.store.Get(context.Background(), "perso"); err != nil {
					t.Errorf("Get() error = %v", err)
				}
			})
		}
		wg.Wait()
	})
}
