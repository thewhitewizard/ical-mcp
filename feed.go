package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	feedTimeout  = 10 * time.Second
	feedMaxBytes = 10 << 20 // 10 MiB
	maxRedirects = 10
)

// The download errors are generic on purpose: the errors of net/http quote the
// Feed address, which is a secret.
var (
	errUnknownCalendar = errors.New("unknown calendar")
	errTimedOut        = errors.New("timed out")
	errCanceled        = errors.New("canceled")
	errUnreachable     = errors.New("unreachable")
	errTooLarge        = errors.New("too large")
	errRedirectNotTLS  = errors.New("redirect to a non-https address refused")
)

// feedStore downloads the Feed of each Calendar and keeps the last good copy of
// each in memory.
type feedStore struct {
	feeds    map[string]string // alias -> Feed
	client   *http.Client
	maxBytes int64
	ttl      time.Duration
	now      func() time.Time
	logger   *log.Logger

	mu    sync.Mutex // guards cache, never held during a download
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	fetched time.Time
}

// newFeedStore builds a store for the Calendars of cfg. transport and now are
// injectable for tests.
func newFeedStore(cfg Config, transport http.RoundTripper, now func() time.Time, logger *log.Logger) *feedStore {
	return &feedStore{
		feeds: cfg.Calendars,
		client: &http.Client{
			Transport: transport,
			Timeout:   feedTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return errUnreachable
				}
				if req.URL.Scheme != "https" {
					return errRedirectNotTLS
				}
				return nil
			},
		},
		maxBytes: feedMaxBytes,
		ttl:      time.Duration(cfg.CacheTTLSeconds) * time.Second,
		now:      now,
		logger:   logger,
		cache:    make(map[string]cacheEntry),
	}
}

// Get returns the iCal data of a Calendar: the cached copy while it is fresh,
// otherwise a new download. If the download fails and an older copy exists, that
// copy is returned with stale set. The slice is shared with the cache: callers
// must not modify it. Errors never contain the Feed.
func (s *feedStore) Get(ctx context.Context, alias string) (body []byte, stale bool, err error) {
	feed, ok := s.feeds[alias]
	if !ok {
		return nil, false, errUnknownCalendar
	}

	s.mu.Lock()
	cached, haveCopy := s.cache[alias]
	s.mu.Unlock()
	if haveCopy && s.now().Sub(cached.fetched) < s.ttl {
		return cached.body, false, nil
	}

	body, err = s.download(ctx, feed)
	if err != nil {
		if haveCopy {
			s.logger.Printf("calendar %q: download failed (%v), serving a stale copy", alias, err)
			return cached.body, true, nil
		}
		return nil, false, err
	}

	s.mu.Lock()
	s.cache[alias] = cacheEntry{body: body, fetched: s.now()}
	s.mu.Unlock()
	return body, false, nil
}

func (s *feedStore) download(ctx context.Context, feed string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed, nil)
	if err != nil {
		return nil, errUnreachable
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, genericError(err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do with a close error on a read-only body

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}
	if resp.ContentLength > s.maxBytes {
		return nil, errTooLarge
	}
	// One byte more than the limit tells a Feed of exactly maxBytes from a larger one.
	body, err := io.ReadAll(io.LimitReader(resp.Body, s.maxBytes+1))
	if err != nil {
		return nil, genericError(err)
	}
	if int64(len(body)) > s.maxBytes {
		return nil, errTooLarge
	}
	return body, nil
}

// genericError reduces a net/http error, which quotes the Feed address, to one
// of the generic errors.
func genericError(err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, errRedirectNotTLS):
		return errRedirectNotTLS
	case errors.Is(err, context.Canceled):
		return errCanceled
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return errTimedOut
	default:
		return errUnreachable
	}
}
