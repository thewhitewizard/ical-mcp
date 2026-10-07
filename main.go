// Command ical-mcp is a read-only MCP server (stdio) that gives an AI assistant
// access to calendars published as iCal feeds over HTTPS.
//
// stdout carries the MCP protocol and nothing else: every diagnostic goes to
// stderr.
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"
)

// version is set at build time: go build -ldflags "-X main.version=v1.0.0".
var version = "dev"

const configEnv = "ICAL_MCP_CONFIG"

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stderr, func(s *server.MCPServer) error {
		return server.ServeStdio(s)
	}))
}

// run starts the server and returns the process exit code: 0 on success, 1 on
// a runtime or configuration error, 2 on a usage error. serve blocks until the
// server stops.
func run(args []string, getenv func(string) string, stderr io.Writer, serve func(*server.MCPServer) error) int {
	logger := log.New(stderr, "ical-mcp: ", 0)

	flags := flag.NewFlagSet("ical-mcp", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to the JSON configuration file (or set "+configEnv+")")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *configPath == "" {
		*configPath = getenv(configEnv)
	}
	if *configPath == "" {
		logger.Printf("no configuration file: pass --config <path> or set %s", configEnv)
		return 2
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		logger.Print(err)
		return 1
	}
	logger.Printf("starting with %d calendar(s), timezone %s", len(cfg.Calendars), cfg.Timezone)

	if err := serve(newServer(cfg, newFeedStore(cfg, nil, time.Now, logger), time.Now)); err != nil {
		logger.Printf("server stopped: %v", err)
		return 1
	}
	return 0
}
