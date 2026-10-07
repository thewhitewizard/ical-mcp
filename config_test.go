package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const secretFeed = "https://calendar.example.com/ical/secret-token-123/basic.ics" //nolint:gosec // a fake secret, to check that it never leaks

// writeConfig writes content to a temporary file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    Config
	}{
		{
			name: "every field set",
			content: `{
				"timezone": "Europe/Paris",
				"default_window_days": 7,
				"max_range_days": 90,
				"max_events": 20,
				"cache_ttl_seconds": 60,
				"calendars": {"perso": "` + secretFeed + `"}
			}`,
			want: Config{
				Timezone:          "Europe/Paris",
				DefaultWindowDays: 7,
				MaxRangeDays:      90,
				MaxEvents:         20,
				CacheTTLSeconds:   60,
				Calendars:         map[string]string{"perso": secretFeed},
			},
		},
		{
			name: "omitted fields take their defaults",
			content: `{
				"timezone": "Europe/Paris",
				"calendars": {"perso": "` + secretFeed + `"}
			}`,
			want: Config{
				Timezone:          "Europe/Paris",
				DefaultWindowDays: 30,
				MaxRangeDays:      366,
				MaxEvents:         100,
				CacheTTLSeconds:   300,
				Calendars:         map[string]string{"perso": secretFeed},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := loadConfig(writeConfig(t, tt.content))
			if err != nil {
				t.Fatalf("loadConfig() error = %v", err)
			}
			if got.Timezone != tt.want.Timezone ||
				got.DefaultWindowDays != tt.want.DefaultWindowDays ||
				got.MaxRangeDays != tt.want.MaxRangeDays ||
				got.MaxEvents != tt.want.MaxEvents ||
				got.CacheTTLSeconds != tt.want.CacheTTLSeconds ||
				got.Calendars["perso"] != tt.want.Calendars["perso"] ||
				len(got.Calendars) != len(tt.want.Calendars) {
				t.Errorf("loadConfig() = %+v, want %+v", got, tt.want)
			}
			if got.Location == nil || got.Location.String() != "Europe/Paris" {
				t.Errorf("Location = %v, want Europe/Paris", got.Location)
			}
		})
	}
}

func TestLoadConfig_Invalid(t *testing.T) {
	t.Parallel()

	const feed = `"perso": "` + secretFeed + `"`
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"invalid JSON", `{"timezone": `, "parse config"},
		{"trailing data", `{"timezone": "Europe/Paris", "calendars": {` + feed + `}} {}`, "parse config"},
		{"wrong type", `{"timezone": "Europe/Paris", "max_events": "ten", "calendars": {` + feed + `}}`, "parse config"},
		{"unknown field", `{"timezone": "Europe/Paris", "max_event": 5, "calendars": {` + feed + `}}`, "max_event"},
		{"missing timezone", `{"calendars": {` + feed + `}}`, "timezone"},
		{"unknown timezone", `{"timezone": "Mars/Olympus", "calendars": {` + feed + `}}`, "timezone"},
		{"local timezone", `{"timezone": "Local", "calendars": {` + feed + `}}`, "timezone"},
		{"no calendars", `{"timezone": "Europe/Paris"}`, "calendar"},
		{"empty calendars", `{"timezone": "Europe/Paris", "calendars": {}}`, "calendar"},
		{"empty alias", `{"timezone": "Europe/Paris", "calendars": {"": "` + secretFeed + `"}}`, "alias"},
		{"http feed", `{"timezone": "Europe/Paris", "calendars": {"perso": "http://calendar.example.com/secret-token-123.ics"}}`, `"perso"`},
		{"other scheme", `{"timezone": "Europe/Paris", "calendars": {"perso": "ftp://calendar.example.com/secret-token-123.ics"}}`, `"perso"`},
		{"no host", `{"timezone": "Europe/Paris", "calendars": {"perso": "https:///secret-token-123.ics"}}`, `"perso"`},
		{"unparsable feed", `{"timezone": "Europe/Paris", "calendars": {"perso": "https://%zz/secret-token-123.ics"}}`, `"perso"`},
		{"not a URL", `{"timezone": "Europe/Paris", "calendars": {"perso": "secret-token-123"}}`, `"perso"`},
		{"zero default window", `{"timezone": "Europe/Paris", "default_window_days": 0, "calendars": {` + feed + `}}`, "default_window_days"},
		{"negative max range", `{"timezone": "Europe/Paris", "max_range_days": -1, "calendars": {` + feed + `}}`, "max_range_days"},
		{"zero max events", `{"timezone": "Europe/Paris", "max_events": 0, "calendars": {` + feed + `}}`, "max_events"},
		{"negative cache ttl", `{"timezone": "Europe/Paris", "cache_ttl_seconds": -5, "calendars": {` + feed + `}}`, "cache_ttl_seconds"},
		{"window above max range", `{"timezone": "Europe/Paris", "default_window_days": 400, "calendars": {` + feed + `}}`, "default_window_days"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadConfig(writeConfig(t, tt.content))
			if err == nil {
				t.Fatal("loadConfig() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantErr)
			}
			if strings.Contains(err.Error(), "secret-token-123") {
				t.Errorf("error leaks the Feed address: %q", err)
			}
		})
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := loadConfig(filepath.Join(t.TempDir(), "absent.json"))
	if err == nil {
		t.Fatal("loadConfig() error = nil, want an error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error = %v, want it to wrap os.ErrNotExist", err)
	}
}
