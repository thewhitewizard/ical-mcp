package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

const validConfig = `{"timezone": "Europe/Paris", "calendars": {"perso": "` + secretFeed + `"}}`

// runResult is what run did: its exit code, what it wrote on stderr, and the
// server it handed to serve (nil if serve was not called).
type runResult struct {
	code   int
	stderr string
	served *server.MCPServer
}

func runWith(t *testing.T, args []string, env map[string]string, serveErr error) runResult {
	t.Helper()

	var res runResult
	var stderr bytes.Buffer
	res.code = run(args, func(k string) string { return env[k] }, &stderr,
		func(s *server.MCPServer) error {
			res.served = s
			return serveErr
		})
	res.stderr = stderr.String()
	return res
}

func TestRun(t *testing.T) {
	t.Parallel()

	valid := writeConfig(t, validConfig)
	invalid := writeConfig(t, `{"timezone": "Europe/Paris", "calendars": {"perso": "http://calendar.example.com/secret-token-123.ics"}}`)
	absent := filepath.Join(t.TempDir(), "absent.json")

	tests := []struct {
		name       string
		args       []string
		env        map[string]string
		serveErr   error
		wantCode   int
		wantServed bool
		wantStderr string
	}{
		{name: "config from the flag", args: []string{"--config", valid}, wantServed: true},
		{name: "config from the environment", env: map[string]string{"ICAL_MCP_CONFIG": valid}, wantServed: true},
		{
			name:       "the flag wins over the environment",
			args:       []string{"--config", valid},
			env:        map[string]string{"ICAL_MCP_CONFIG": absent},
			wantServed: true,
		},
		{name: "no config path", wantCode: 2, wantStderr: "ICAL_MCP_CONFIG"},
		{name: "unknown flag", args: []string{"--nope"}, wantCode: 2, wantStderr: "nope"},
		{name: "help", args: []string{"-h"}, wantStderr: "-config"},
		{name: "absent config file", args: []string{"--config", absent}, wantCode: 1, wantStderr: "read config"},
		{name: "invalid config", args: []string{"--config", invalid}, wantCode: 1, wantStderr: `"perso"`},
		{
			name:       "server failure",
			args:       []string{"--config", valid},
			serveErr:   errors.New("boom"),
			wantCode:   1,
			wantServed: true,
			wantStderr: "boom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := runWith(t, tt.args, tt.env, tt.serveErr)
			if res.code != tt.wantCode {
				t.Errorf("exit code = %d, want %d (stderr: %q)", res.code, tt.wantCode, res.stderr)
			}
			if (res.served != nil) != tt.wantServed {
				t.Errorf("serve called = %v, want %v", res.served != nil, tt.wantServed)
			}
			if !strings.Contains(res.stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", res.stderr, tt.wantStderr)
			}
			if strings.Contains(res.stderr, "secret-token-123") {
				t.Errorf("stderr leaks the Feed address: %q", res.stderr)
			}
		})
	}
}

func TestRun_ServesTheTools(t *testing.T) {
	t.Parallel()

	res := runWith(t, []string{"--config", writeConfig(t, validConfig)}, nil, nil)
	if res.served == nil {
		t.Fatalf("no server was served (stderr: %q)", res.stderr)
	}
	if tools := string(rpc(t, res.served, "tools/list", nil)); !strings.Contains(tools, `"list_events"`) {
		t.Errorf("tools/list = %s, want list_events", tools)
	}
}
