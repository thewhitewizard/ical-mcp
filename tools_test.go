package main

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// fakeFeeds is a feedSource that serves what each test gives it.
type fakeFeeds map[string]fakeFeed

type fakeFeed struct {
	body  []byte
	stale bool
	err   error
}

func (f fakeFeeds) Get(_ context.Context, alias string) ([]byte, bool, error) {
	feed, ok := f[alias]
	if !ok {
		return nil, false, errUnknownCalendar
	}
	return feed.body, feed.stale, feed.err
}

// testNow is "today" for the tests: 10 March 2026 in Paris.
var testNow = time.Date(2026, 3, 10, 12, 0, 0, 0, paris)

// newTestServer builds the server over fake feeds. tune changes the default
// configuration, which has the two Calendars "perso" and "travail".
func newTestServer(feeds fakeFeeds, tune func(*Config)) *server.MCPServer {
	cfg := Config{
		Timezone: "Europe/Paris", Location: paris,
		DefaultWindowDays: 30, MaxRangeDays: 366, MaxEvents: 100, CacheTTLSeconds: 300,
		Calendars: map[string]string{"perso": "https://example.com/a", "travail": "https://example.com/b"},
	}
	if tune != nil {
		tune(&cfg)
	}
	return newServer(cfg, feeds, func() time.Time { return testNow })
}

// rpc sends one JSON-RPC request to the server and returns its result.
func rpc(t *testing.T, srv *server.MCPServer, method string, params any) json.RawMessage {
	t.Helper()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(srv.HandleMessage(context.Background(), request))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || response.Error != nil {
		t.Fatalf("%s: response %s", method, raw)
	}
	return response.Result
}

func TestTools_List(t *testing.T) {
	t.Parallel()

	var listed struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
			} `json:"inputSchema"`
			Annotations struct {
				ReadOnlyHint bool `json:"readOnlyHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc(t, newTestServer(nil, nil), "tools/list", nil), &listed); err != nil {
		t.Fatal(err)
	}

	if len(listed.Tools) != 1 || listed.Tools[0].Name != "list_events" {
		t.Fatalf("tools = %+v, want only list_events", listed.Tools)
	}
	tool := listed.Tools[0]
	if !tool.Annotations.ReadOnlyHint || !strings.Contains(tool.Description, "exclusive") {
		t.Errorf("want a read-only tool whose description says that end is exclusive: %+v", tool)
	}
	if got := tool.InputSchema.Properties["calendar"].Enum; !slices.Equal(got, []string{"perso", "travail"}) {
		t.Errorf("calendar enum = %v, want the configured aliases", got)
	}
	if got := slices.Sorted(maps.Keys(tool.InputSchema.Properties)); !slices.Equal(got, []string{"calendar", "from", "to"}) {
		t.Errorf("parameters = %v, want calendar, from and to: a Feed address must not be accepted", got)
	}
}

// callList calls list_events and returns the text it answered and whether it is an error.
func callList(t *testing.T, srv *server.MCPServer, args map[string]any) (text string, isError bool) {
	t.Helper()
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(rpc(t, srv, "tools/call", map[string]any{"name": "list_events", "arguments": args}), &result); err != nil || len(result.Content) != 1 {
		t.Fatalf("list_events: unexpected result %+v (%v)", result, err)
	}
	return result.Content[0].Text, result.IsError
}

// listEvents calls list_events and decodes a successful answer.
func listEvents(t *testing.T, srv *server.MCPServer, args map[string]any) listResult {
	t.Helper()
	text, isError := callList(t, srv, args)
	var result listResult
	if err := json.Unmarshal([]byte(text), &result); err != nil || isError {
		t.Fatalf("list_events %v: isError = %v, text = %q", args, isError, text)
	}
	return result
}

func testFeeds(t *testing.T) fakeFeeds {
	t.Helper()
	return fakeFeeds{
		"perso":   {body: readTestdata(t, "events.ics")},
		"travail": {body: readTestdata(t, "overrides.ics")},
	}
}

func TestListEvents_MergesCalendarsSortedByStart(t *testing.T) {
	t.Parallel()

	got := listEvents(t, newTestServer(testFeeds(t), nil), map[string]any{"from": "2026-03-10", "to": "2026-03-10"})

	var lines []string
	for _, e := range got.Events {
		lines = append(lines, e.Calendar+" "+e.Start+" "+e.Title)
	}
	want := []string{
		"perso 2026-03-10T14:00:00+01:00 Standup",
		"perso 2026-03-10T15:00:00+01:00 New York meeting",
		"travail 2026-03-10T15:00:00+01:00 Series A moved",
	}
	if !slices.Equal(lines, want) {
		t.Errorf("events = %q, want %q", lines, want)
	}
	for _, e := range got.Events {
		if e.Description != "" || e.Organizer != "" {
			t.Errorf("list_events must not return the description or the organizer: %+v", e)
		}
	}
}

func TestListEvents_WireFormat(t *testing.T) {
	t.Parallel()

	text, isError := callList(t, newTestServer(testFeeds(t), nil),
		map[string]any{"calendar": "travail", "from": "2026-03-10", "to": "2026-03-10"})

	want := `{"events":[{"uid":"a","calendar":"travail","title":"Series A moved","start":"2026-03-10T15:00:00+01:00",` +
		`"end":"2026-03-10T16:00:00+01:00","all_day":false,"location":""}]}`
	if isError || text != want {
		t.Errorf("list_events = (%q, %v), want (%q, false)", text, isError, want)
	}
}

func TestListEvents_DefaultWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args map[string]any
		want []string
	}{
		{"from today to today plus the default window", map[string]any{}, []string{
			"Standup", "New York meeting", "Series A moved", "Ninety minutes", "Series A", "Series B", "Overlaps window end", "Starts at window end",
		}},
		{"to defaults from from", map[string]any{"from": "2026-03-31"}, []string{"Overlaps window end", "Starts at window end"}},
		{"from defaults to today", map[string]any{"to": "2026-03-10"}, []string{"Standup", "New York meeting", "Series A moved"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var titles []string
			for _, e := range listEvents(t, newTestServer(testFeeds(t), nil), tt.args).Events {
				titles = append(titles, e.Title)
			}
			if !slices.Equal(titles, tt.want) {
				t.Errorf("titles = %q, want %q", titles, tt.want)
			}
		})
	}
}

func TestListEvents_InvalidArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"to before from", map[string]any{"from": "2026-03-10", "to": "2026-03-09"}, "before"},
		{"range above max_range_days", map[string]any{"from": "2026-03-01", "to": "2026-03-12"}, "at most 10 days"},
		{"from is not a date", map[string]any{"from": "10/03/2026"}, "from"},
		{"to is not a date", map[string]any{"to": "tomorrow"}, "to must"},
		{"unknown calendar", map[string]any{"calendar": "famille"}, "unknown calendar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newTestServer(testFeeds(t), func(c *Config) { c.MaxRangeDays, c.DefaultWindowDays = 10, 5 })
			if text, isError := callList(t, srv, tt.args); !isError || !strings.Contains(text, tt.wantErr) {
				t.Errorf("list_events %v = (%q, %v), want an error mentioning %q", tt.args, text, isError, tt.wantErr)
			}
		})
	}

	t.Run("a range of exactly max_range_days is accepted", func(t *testing.T) {
		t.Parallel()

		srv := newTestServer(testFeeds(t), func(c *Config) { c.MaxRangeDays, c.DefaultWindowDays = 10, 5 })
		listEvents(t, srv, map[string]any{"from": "2026-03-01", "to": "2026-03-11"})
	})
}

func TestListEvents_Truncation(t *testing.T) {
	t.Parallel()

	args := map[string]any{"from": "2026-03-10", "to": "2026-03-10"} // 3 events in all
	tests := []struct {
		name          string
		maxEvents     int
		wantTitles    []string
		wantTruncated bool
	}{
		{"cut after the sort, keeping the earliest", 2, []string{"Standup", "New York meeting"}, true},
		{"exactly max_events is not truncated", 3, []string{"Standup", "New York meeting", "Series A moved"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := listEvents(t, newTestServer(testFeeds(t), func(c *Config) { c.MaxEvents = tt.maxEvents }), args)

			var titles []string
			for _, e := range got.Events {
				titles = append(titles, e.Title)
			}
			if !slices.Equal(titles, tt.wantTitles) || got.Truncated != tt.wantTruncated {
				t.Errorf("list_events = (%q, truncated %v), want (%q, truncated %v)", titles, got.Truncated, tt.wantTitles, tt.wantTruncated)
			}
		})
	}
}

func TestListEvents_UnreliableCalendars(t *testing.T) {
	t.Parallel()

	args := map[string]any{"from": "2026-03-10", "to": "2026-03-10"}
	perso := readTestdata(t, "events.ics")

	t.Run("a failing Calendar does not fail the others", func(t *testing.T) {
		t.Parallel()
		feeds := fakeFeeds{"perso": {body: perso}, "travail": {err: errUnreachable}}

		got := listEvents(t, newTestServer(feeds, nil), args)

		if len(got.Events) != 2 || !slices.Equal(got.Errors, []string{"travail: unreachable"}) {
			t.Errorf("list_events = %+v, want 2 events from perso and the error of travail", got)
		}
	})

	t.Run("an invalid Feed is reported by its Calendar", func(t *testing.T) {
		t.Parallel()
		feeds := fakeFeeds{"perso": {body: perso}, "travail": {body: []byte("<html>sign in</html>")}}

		got := listEvents(t, newTestServer(feeds, nil), args)

		if !slices.Equal(got.Errors, []string{"travail: invalid iCal data"}) {
			t.Errorf("errors = %q, want the error of travail", got.Errors)
		}
	})

	t.Run("a stale Calendar is listed", func(t *testing.T) {
		t.Parallel()
		feeds := fakeFeeds{"perso": {body: perso, stale: true}, "travail": {body: readTestdata(t, "overrides.ics")}}

		got := listEvents(t, newTestServer(feeds, nil), args)

		if len(got.Events) != 3 || !slices.Equal(got.Stale, []string{"perso"}) || got.Errors != nil {
			t.Errorf("list_events = %+v, want 3 events and perso stale", got)
		}
	})

	t.Run("all Calendars failing is an error", func(t *testing.T) {
		t.Parallel()
		feeds := fakeFeeds{"perso": {err: errTimedOut}, "travail": {err: errUnreachable}}

		text, isError := callList(t, newTestServer(feeds, nil), args)

		if want := "perso: timed out; travail: unreachable"; !isError || text != want {
			t.Errorf("list_events = (%q, %v), want (%q, true)", text, isError, want)
		}
	})
}
