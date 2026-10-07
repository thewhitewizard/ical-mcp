package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// feedSource gives the iCal data of a Calendar, and whether it is stale (see
// feedStore.Get).
type feedSource interface {
	Get(ctx context.Context, alias string) (body []byte, stale bool, err error)
}

// newServer builds the MCP server and its read-only tools. now gives
// "today". No tool takes a Feed address: Feeds are secrets.
func newServer(cfg Config, feeds feedSource, now func() time.Time) *server.MCPServer {
	h := &handlers{cfg: cfg, feeds: feeds, now: now}
	s := server.NewMCPServer("ical-mcp", version, server.WithRecovery())
	s.AddTool(mcp.NewTool("list_events", mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("List calendar events between two days, both included, sorted by start. Times are ISO 8601 in the "+
			cfg.Timezone+" timezone. An all-day event has dates (YYYY-MM-DD) instead of times, and its end is exclusive: the day after its last day."),
		mcp.WithString("calendar", mcp.Enum(slices.Sorted(maps.Keys(cfg.Calendars))...), mcp.Description("Only this calendar. Default: all calendars, merged.")),
		mcp.WithString("from", mcp.Description("First day, YYYY-MM-DD. Default: today.")),
		mcp.WithString("to", mcp.Description("Last day, YYYY-MM-DD. Default: from plus the default window.")),
	), h.listEvents)
	s.AddTool(mcp.NewTool("get_event", mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDescription("Get one event occurrence with its description and organizer. Same fields and time format as list_events: "+
			"an all-day event has dates instead of times. The end is exclusive: the first moment (or day) after the event."),
		mcp.WithString("calendar", mcp.Required(), mcp.Enum(slices.Sorted(maps.Keys(cfg.Calendars))...), mcp.Description("The calendar of the event.")),
		mcp.WithString("uid", mcp.Required(), mcp.Description("The uid given by list_events.")),
		mcp.WithString("start", mcp.Required(), mcp.Description("The start given by list_events: ISO 8601 with offset, or YYYY-MM-DD for an all-day event.")),
	), h.getEvent)
	return s
}

// handlers are the tool handlers, with what they need.
type handlers struct {
	cfg   Config
	feeds feedSource
	now   func() time.Time
}

// listResult is the answer of list_events.
type listResult struct {
	Events    []Occurrence `json:"events"`
	Truncated bool         `json:"truncated,omitempty"`
	Stale     []string     `json:"stale,omitempty"`
	Errors    []string     `json:"errors,omitempty"`
}

func (h *handlers) listEvents(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	win, err := h.window(req.GetString("from", ""), req.GetString("to", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	aliases, err := h.aliases(req.GetString("calendar", ""))
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	result := listResult{Events: []Occurrence{}}
	for _, c := range h.fetchAll(ctx, aliases, win) {
		if c.err != nil {
			result.Errors = append(result.Errors, c.alias+": "+c.err.Error())
			continue
		}
		if c.stale {
			result.Stale = append(result.Stale, c.alias)
		}
		for _, o := range c.found {
			o.Description, o.Organizer = "", ""
			result.Events = append(result.Events, o)
		}
	}
	if len(result.Errors) == len(aliases) {
		return mcp.NewToolResultError(strings.Join(result.Errors, "; ")), nil
	}
	slices.SortStableFunc(result.Events, func(a, b Occurrence) int {
		return cmp.Or(a.start.Compare(b.start), strings.Compare(a.Calendar, b.Calendar), strings.Compare(a.UID, b.UID))
	})
	if len(result.Events) > h.cfg.MaxEvents {
		result.Events, result.Truncated = result.Events[:h.cfg.MaxEvents], true
	}
	return jsonResult(result), nil
}

// getResult is the answer of get_event.
type getResult struct {
	Event Occurrence `json:"event"`
	Stale []string   `json:"stale,omitempty"`
}

func (h *handlers) getEvent(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args [3]string
	for i, name := range []string{"calendar", "uid", "start"} {
		if args[i] = req.GetString(name, ""); args[i] == "" {
			return mcp.NewToolResultError(name + " is required"), nil
		}
	}
	calendar, uid, rawStart := args[0], args[1], args[2]
	start, err := time.Parse(time.RFC3339, rawStart)
	if err != nil {
		if start, err = time.ParseInLocation(time.DateOnly, rawStart, h.cfg.Location); err != nil {
			return mcp.NewToolResultError("start must be as given by list_events: YYYY-MM-DDTHH:MM:SS+HH:MM, or YYYY-MM-DD"), nil
		}
	}
	aliases, err := h.aliases(calendar)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	// The Occurrence starts within the day of its start.
	day := start.In(h.cfg.Location)
	c := h.fetchAll(ctx, aliases, newWindow(day, day, h.cfg.Location))[0]
	if c.err != nil {
		return mcp.NewToolResultError(c.alias + ": " + c.err.Error()), nil
	}
	for _, o := range c.found {
		if o.UID == uid && o.start.Equal(start) {
			result := getResult{Event: o}
			if c.stale {
				result.Stale = []string{c.alias}
			}
			return jsonResult(result), nil
		}
	}
	return mcp.NewToolResultError("occurrence not found"), nil
}

// fetched is what one Calendar gave: its Occurrences in the Window, or an error.
type fetched struct {
	alias string
	stale bool
	found []Occurrence
	err   error
}

// fetchAll reads the Calendars at the same time, so that a slow Feed does not
// add its delay to the others. The result is in the order of aliases.
func (h *handlers) fetchAll(ctx context.Context, aliases []string, win window) []fetched {
	results := make([]fetched, len(aliases))
	var wg sync.WaitGroup
	for i, alias := range aliases {
		wg.Go(func() {
			results[i] = fetched{alias: alias}
			var body []byte
			if body, results[i].stale, results[i].err = h.feeds.Get(ctx, alias); results[i].err == nil {
				results[i].found, results[i].err = occurrences(body, alias, win, h.cfg.Location)
			}
		})
	}
	wg.Wait()
	return results
}

// jsonResult answers a tool call with v as compact JSON.
func jsonResult(v any) *mcp.CallToolResult {
	data, _ := json.Marshal(v) // the results are plain structs: they always marshal
	return mcp.NewToolResultText(string(data))
}

// aliases returns the Calendars asked for: all of them if calendar is empty.
func (h *handlers) aliases(calendar string) ([]string, error) {
	all := slices.Sorted(maps.Keys(h.cfg.Calendars))
	switch {
	case calendar == "":
		return all, nil
	case slices.Contains(all, calendar):
		return []string{calendar}, nil
	}
	return nil, fmt.Errorf("unknown calendar %q, use one of: %s", calendar, strings.Join(all, ", "))
}

// window is the Window between the days from and to, both YYYY-MM-DD and both
// optional: from defaults to today, to to from plus the default window.
func (h *handlers) window(from, to string) (window, error) {
	today := h.now().In(h.cfg.Location)
	first := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if from != "" {
		var err error
		if first, err = time.Parse(time.DateOnly, from); err != nil {
			return window{}, errors.New("from must be a date, YYYY-MM-DD")
		}
	}
	last := first.AddDate(0, 0, h.cfg.DefaultWindowDays)
	if to != "" {
		var err error
		if last, err = time.Parse(time.DateOnly, to); err != nil {
			return window{}, errors.New("to must be a date, YYYY-MM-DD")
		}
	}
	if last.Before(first) {
		return window{}, errors.New("to must not be before from")
	}
	if days := int(last.Sub(first) / (24 * time.Hour)); days > h.cfg.MaxRangeDays {
		return window{}, fmt.Errorf("the range is %d days, at most %d days are allowed", days, h.cfg.MaxRangeDays)
	}
	return newWindow(first, last, h.cfg.Location), nil
}
