package main

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/apognu/gocal"
)

// Limits on the text of an Event, which comes from a third party and is not
// to be trusted.
const (
	maxShortText       = 200  // title, location
	maxDescriptionText = 1000 // description
)

// clean makes text from a third party safe to hand to an assistant: control and
// invisible format characters are removed (they can hide instructions), line
// breaks are kept only if multiline, and the text is cut to limit characters.
func clean(s string, limit int, multiline bool) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\U00002028', r == '\U00002029':
			if multiline {
				return '\n'
			}
			return ' '
		case r == '\t':
			return ' '
		case r == '\r':
			if multiline {
				return -1
			}
			return ' '
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)

	if runes := []rune(s); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return s
}

// window is a half-open interval [start, end) made of whole days of one
// timezone. An Occurrence is in the Window if it overlaps it.
type window struct{ start, end time.Time }

// newWindow covers the days from firstDay to lastDay, both included, in loc.
// Only the date of each is used.
func newWindow(firstDay, lastDay time.Time, loc *time.Location) window {
	y1, m1, d1 := firstDay.Date()
	y2, m2, d2 := lastDay.Date()
	return window{
		start: time.Date(y1, m1, d1, 0, 0, 0, 0, loc),
		end:   time.Date(y2, m2, d2+1, 0, 0, 0, 0, loc),
	}
}

// textNewlines turns the escaped line breaks of iCal text into real ones: gocal
// unescapes everything else but them.
var textNewlines = strings.NewReplacer(`\n`, "\n", `\N`, "\n")

// errInvalidFeed is all that is said about a Feed that cannot be read: the
// parser's own errors quote the text of the Feed, which comes from a third party.
var errInvalidFeed = errors.New("invalid iCal data")

// Occurrence is one dated instance of an Event. Timed Occurrences carry ISO 8601
// times with the offset of the configured timezone; all-day ones carry dates,
// and their end is exclusive, as in iCal.
type Occurrence struct {
	UID         string `json:"uid"`
	Calendar    string `json:"calendar"`
	Title       string `json:"title"`
	Start       string `json:"start"`
	End         string `json:"end"`
	AllDay      bool   `json:"all_day"`
	Location    string `json:"location"`
	Description string `json:"description,omitempty"`
	Organizer   string `json:"organizer,omitempty"`

	start time.Time // for sorting and matching
}

// occurrences returns the Occurrences of a Feed that overlap the Window, sorted
// by start. The text of each is cleaned.
//
// gocal is asked for everything (SkipBounds, wide bounds) and the Window is
// applied here: its own bounds exclude an Event starting exactly at the start,
// and an Occurrence moved out of the bounds by a RECURRENCE-ID brings back the
// original one.
func occurrences(feed []byte, calendar string, win window, loc *time.Location) ([]Occurrence, error) {
	if !bytes.Contains(feed, []byte("BEGIN:VCALENDAR")) {
		return nil, errInvalidFeed
	}
	from, to := win.start.AddDate(0, 0, -1).Add(-time.Second), win.end.Add(time.Second)
	parser := gocal.NewParser(bytes.NewReader(feed))
	parser.Start, parser.End, parser.SkipBounds, parser.AllDayEventsTZ = &from, &to, true, loc
	if err := parser.Parse(); err != nil {
		return nil, errInvalidFeed
	}

	var found []Occurrence
	for _, e := range parser.Events {
		if e.Status == "CANCELLED" {
			continue
		}
		start, end := *e.Start, *e.End
		allDay := e.RawStart.Params["VALUE"] == "DATE"
		layout := time.RFC3339
		if allDay {
			// gocal ends a day-long Event at 23:59:59.999 of its last day, but at
			// midnight when DTEND is absent: the date one second later is the exclusive
			// end in both cases.
			y, m, d := end.Add(time.Second).Date()
			end = time.Date(y, m, d, 0, 0, 0, 0, loc)
			layout = time.DateOnly
		}
		if !start.Before(win.end) || !end.After(win.start) {
			continue
		}
		organizer := ""
		if e.Organizer != nil {
			organizer = cmp.Or(e.Organizer.Cn, e.Organizer.Value)
		}
		found = append(found, Occurrence{
			UID:         e.Uid,
			Calendar:    calendar,
			Title:       clean(textNewlines.Replace(e.Summary), maxShortText, false),
			Start:       start.In(loc).Format(layout),
			End:         end.In(loc).Format(layout),
			AllDay:      allDay,
			Location:    clean(textNewlines.Replace(e.Location), maxShortText, false),
			Description: clean(textNewlines.Replace(e.Description), maxDescriptionText, true),
			Organizer:   clean(organizer, maxShortText, false),
			start:       start,
		})
	}
	slices.SortFunc(found, func(a, b Occurrence) int {
		return cmp.Or(a.start.Compare(b.start), strings.Compare(a.UID, b.UID))
	})
	return found, nil
}
