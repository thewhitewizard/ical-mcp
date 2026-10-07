package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"time"
)

// Config is the server configuration, read from a JSON file.
type Config struct {
	Timezone          string            `json:"timezone"`
	DefaultWindowDays int               `json:"default_window_days"`
	MaxRangeDays      int               `json:"max_range_days"`
	MaxEvents         int               `json:"max_events"`
	CacheTTLSeconds   int               `json:"cache_ttl_seconds"`
	Calendars         map[string]string `json:"calendars"` // alias -> Feed

	// Location is Timezone, loaded.
	Location *time.Location `json:"-"`
}

// loadConfig reads and validates the configuration file at path. Errors never
// contain a Feed address: they name the Calendar alias only.
func loadConfig(path string) (Config, error) {
	cfg := Config{
		DefaultWindowDays: 30,
		MaxRangeDays:      366,
		MaxEvents:         100,
		CacheTTLSeconds:   300,
	}

	data, err := os.ReadFile(path) //nolint:gosec // the configuration path is chosen by the user, by design
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	if dec.More() {
		return Config{}, errors.New("parse config: unexpected data after the JSON object")
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate checks the values and loads the timezone.
func (c *Config) validate() error {
	if c.Timezone == "" {
		return errors.New("config: timezone is required")
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("config: timezone: %w", err)
	}
	c.Location = loc

	for name, v := range map[string]int{
		"default_window_days": c.DefaultWindowDays,
		"max_range_days":      c.MaxRangeDays,
		"max_events":          c.MaxEvents,
		"cache_ttl_seconds":   c.CacheTTLSeconds,
	} {
		if v <= 0 {
			return fmt.Errorf("config: %s must be greater than 0", name)
		}
	}
	if c.DefaultWindowDays > c.MaxRangeDays {
		return errors.New("config: default_window_days must not exceed max_range_days")
	}

	if len(c.Calendars) == 0 {
		return errors.New("config: at least one calendar is required")
	}
	for _, alias := range slices.Sorted(maps.Keys(c.Calendars)) {
		if alias == "" {
			return errors.New("config: a calendar alias must not be empty")
		}
		// The url.Parse error would quote the Feed, so it is dropped.
		if u, err := url.Parse(c.Calendars[alias]); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("config: calendar %q: the Feed must be an https address", alias)
		}
	}
	return nil
}
