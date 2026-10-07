package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		max       int
		multiline bool
		want      string
	}{
		{"plain text is kept", "Réunion équipe 🎉", 200, false, "Réunion équipe 🎉"},
		{"control characters are removed", "a\x00b\x07c\x1bd\x7fe\u0085f", 200, false, "abcdef"},
		{"single line: newlines and tabs become spaces", "a\nb\r\nc\td", 200, false, "a b  c d"},
		{"multiline: newlines are kept, carriage returns removed", "a\r\nb\tc", 200, true, "a\nb c"},
		{"line separators follow the newline rule", "a\U00002028b\U00002029c", 200, false, "a b c"},
		{"invisible format characters are removed", "a\U0000200bb\U0000202ec\U0000feffd\U000E0041e", 200, false, "abcde"},
		{"truncated to max characters, not bytes", strings.Repeat("é", 10), 4, false, "éééé…"},
		{"exactly max is not truncated", "abcd", 4, false, "abcd"},
		{"truncation counts what is left after cleaning", "a\x00bcdef", 3, false, "abc…"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := clean(tt.in, tt.max, tt.multiline); got != tt.want {
				t.Errorf("clean(%q, %d, %v) = %q, want %q", tt.in, tt.max, tt.multiline, got, tt.want)
			}
		})
	}
}

var paris = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		panic(err)
	}
	return loc
}()

// march2026 is the Window of the whole of March 2026, in Paris.
var march2026 = newWindow(time.Date(2026, 3, 1, 0, 0, 0, 0, paris), time.Date(2026, 3, 31, 0, 0, 0, 0, paris), paris)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test data directory
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// show renders an Occurrence as "start -> end title", to compare lists briefly.
func show(occs []Occurrence) []string {
	lines := make([]string, len(occs))
	for i, o := range occs {
		lines[i] = o.Start + " -> " + o.End + " " + o.Title
		if o.AllDay {
			lines[i] += " [all day]"
		}
	}
	return lines
}

func TestOccurrences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
		win  window
		want []string
	}{
		{
			name: "events, time zones, window edges and cancellation",
			file: "events.ics",
			win:  march2026,
			want: []string{
				"2026-02-28T23:00:00+01:00 -> 2026-03-01T02:00:00+01:00 Overlaps window start",
				"2026-03-01T00:00:00+01:00 -> 2026-03-01T01:00:00+01:00 Starts at window start",
				"2026-03-10T14:00:00+01:00 -> 2026-03-10T15:00:00+01:00 Standup",
				"2026-03-10T15:00:00+01:00 -> 2026-03-10T16:00:00+01:00 New York meeting",
				"2026-03-12T10:00:00+01:00 -> 2026-03-12T11:30:00+01:00 Ninety minutes",
				"2026-03-31T23:30:00+02:00 -> 2026-04-01T00:30:00+02:00 Overlaps window end",
			},
		},
		{
			name: "weekly across the March daylight saving change, with an EXDATE",
			file: "dst.ics",
			win:  march2026,
			want: []string{
				"2026-03-16T09:00:00+01:00 -> 2026-03-16T10:00:00+01:00 Weekly Paris",
				"2026-03-16T09:30:00+01:00 -> 2026-03-16T10:30:00+01:00 Weekly UTC",
				"2026-03-23T09:30:00+01:00 -> 2026-03-23T10:30:00+01:00 Weekly UTC",
				"2026-03-30T09:00:00+02:00 -> 2026-03-30T10:00:00+02:00 Weekly Paris",
				"2026-03-30T10:30:00+02:00 -> 2026-03-30T11:30:00+02:00 Weekly UTC",
			},
		},
		{
			name: "weekly across the October daylight saving change",
			file: "dst.ics",
			win:  newWindow(time.Date(2026, 10, 1, 0, 0, 0, 0, paris), time.Date(2026, 11, 30, 0, 0, 0, 0, paris), paris),
			want: []string{
				"2026-10-19T10:00:00+02:00 -> 2026-10-19T11:00:00+02:00 Autumn",
				"2026-10-26T10:00:00+01:00 -> 2026-10-26T11:00:00+01:00 Autumn",
				"2026-11-02T10:00:00+01:00 -> 2026-11-02T11:00:00+01:00 Autumn",
			},
		},
		{
			name: "an Occurrence moved inside the Window, and one moved out of it",
			file: "overrides.ics",
			win:  march2026,
			want: []string{
				"2026-03-02T10:00:00+01:00 -> 2026-03-02T11:00:00+01:00 Series A",
				"2026-03-03T11:00:00+01:00 -> 2026-03-03T12:00:00+01:00 Series B",
				"2026-03-10T15:00:00+01:00 -> 2026-03-10T16:00:00+01:00 Series A moved",
				"2026-03-16T10:00:00+01:00 -> 2026-03-16T11:00:00+01:00 Series A",
				"2026-03-17T11:00:00+01:00 -> 2026-03-17T12:00:00+01:00 Series B",
			},
		},
		{
			name: "all-day Events keep their dates, with an exclusive end",
			file: "allday.ics",
			win:  march2026,
			want: []string{
				"2026-02-27 -> 2026-03-02 Spans window start [all day]",
				"2026-03-05 -> 2026-03-06 One day [all day]",
				"2026-03-07 -> 2026-03-08 Weekly all day [all day]",
				"2026-03-10 -> 2026-03-13 Three days [all day]",
				"2026-03-14 -> 2026-03-15 Weekly all day [all day]",
				"2026-03-20 -> 2026-03-21 No end [all day]",
				"2026-03-31 -> 2026-04-02 Spans window end [all day]",
			},
		},
		{
			name: "a recurring Event started long before the Window",
			file: "long-running.ics",
			win:  newWindow(time.Date(2026, 3, 1, 0, 0, 0, 0, paris), time.Date(2026, 3, 10, 0, 0, 0, 0, paris), paris),
			want: []string{
				"2026-03-02T18:00:00+01:00 -> 2026-03-02T19:00:00+01:00 Started last year",
				"2026-03-09T18:00:00+01:00 -> 2026-03-09T19:00:00+01:00 Started last year",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := occurrences(readTestdata(t, tt.file), "perso", tt.win, paris)
			if err != nil {
				t.Fatalf("occurrences() error = %v", err)
			}
			if lines := show(got); !slices.Equal(lines, tt.want) {
				t.Errorf("occurrences() =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// inlineFeed wraps VEVENT properties in a minimal valid Feed.
func inlineFeed(props ...string) []byte {
	lines := append([]string{"BEGIN:VCALENDAR", "BEGIN:VEVENT", "UID:x", "DTSTAMP:20260101T000000Z"}, props...)
	return []byte(strings.Join(append(lines, "END:VEVENT", "END:VCALENDAR", ""), "\r\n"))
}

func TestOccurrences_Fields(t *testing.T) {
	t.Parallel()

	got, err := occurrences(readTestdata(t, "events.ics"), "perso", march2026, paris)
	if err != nil {
		t.Fatalf("occurrences() error = %v", err)
	}
	var simple Occurrence
	for _, o := range got {
		if o.UID == "simple" {
			simple = o
		}
	}

	want := Occurrence{
		UID:         "simple",
		Calendar:    "perso",
		Title:       "Standup",
		Start:       "2026-03-10T14:00:00+01:00",
		End:         "2026-03-10T15:00:00+01:00",
		Location:    "Salle A",
		Description: "Point quotidien\nApporter le rapport",
		Organizer:   "Alice",
	}
	simple.start = time.Time{}
	if simple != want {
		t.Errorf("Occurrence = %+v, want %+v", simple, want)
	}
}

func TestOccurrences_TextFromAThirdPartyIsCleaned(t *testing.T) {
	t.Parallel()

	feed := inlineFeed(
		"DTSTART:20260310T090000Z", "DTEND:20260310T100000Z",
		"SUMMARY:Ev"+string(rune(7))+"il"+string(rune(0x200b))+" "+strings.Repeat("t", 300),
		"LOCATION:"+strings.Repeat("l", 300),
		"DESCRIPTION:"+strings.Repeat("d", 1200),
	)

	got, err := occurrences(feed, "travail", march2026, paris)
	if err != nil || len(got) != 1 {
		t.Fatalf("occurrences() = %v, %v; want one Occurrence", got, err)
	}
	if want := "Evil " + strings.Repeat("t", 195) + "…"; got[0].Title != want {
		t.Errorf("Title = %q, want %q", got[0].Title, want)
	}
	if want := strings.Repeat("l", 200) + "…"; got[0].Location != want {
		t.Errorf("Location = %q, want %q", got[0].Location, want)
	}
	if want := strings.Repeat("d", 1000) + "…"; got[0].Description != want {
		t.Errorf("Description = %q, want %q", got[0].Description, want)
	}
}

func TestOccurrences_InvalidFeed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		feed []byte
	}{
		{"empty", nil},
		{"not iCal", []byte("<html><body>Please sign in to continue</body></html>")},
		{"event without DTSTAMP", []byte("BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:x\r\nSUMMARY:secret text\r\nDTSTART:20260310T090000Z\r\nDTEND:20260310T100000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := occurrences(tt.feed, "perso", march2026, paris)
			if !errors.Is(err, errInvalidFeed) {
				t.Fatalf("occurrences() = %v, %v; want %v", got, err, errInvalidFeed)
			}
			if err.Error() != "invalid iCal data" {
				t.Errorf("error = %q, want a generic message that does not quote the Feed", err)
			}
		})
	}
}
