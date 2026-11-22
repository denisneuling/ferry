// Command ferry shows SMB shares in a browser: `ferry serve` runs the
// interface and the file API, logging people in through an SMB session setup
// against the configured server.
//
// It is configured through FERRY_* variables; docs/configuration.md lists
// every one.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/metapatrol/ferry/internal/api"
	"github.com/metapatrol/ferry/internal/config"
	"github.com/metapatrol/ferry/internal/metrics"
	"github.com/metapatrol/ferry/internal/server"
	"github.com/metapatrol/ferry/internal/session"
	"github.com/metapatrol/ferry/internal/smb"
	"github.com/metapatrol/ferry/internal/tracing"
)

// Set at build time through -ldflags (Makefile, Dockerfile, release.yml).
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

const usage = `ferry — SMB shares in the browser

Usage:
  ferry serve     run the server (configured through FERRY_* variables)
  ferry version   print the build
  ferry help      this text
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "serve":
		return serve(args[1:], stderr)
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "ferry %s (commit %s, built %s)\n", version, commit, builtAt)
		return 0
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "ferry: unknown command %q\n\n%s", args[0], usage)
	return 2
}

// serve runs ferry until SIGINT or SIGTERM.
func serve(args []string, stderr io.Writer) int {
	if len(args) > 0 {
		_, _ = fmt.Fprintf(stderr, "ferry serve takes no arguments; it is configured through FERRY_* variables (docs/configuration.md)\n")
		return 2
	}
	cfg, err := config.FromEnv(os.Getenv)
	log := newLogger(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		log.Error("ferry cannot start", "err", err)
		return 1
	}
	log.Info("starting ferry", "version", version, "commit", commit, "built_at", builtAt,
		"smb_server", cfg.SMBServer, "shares_allowed", len(cfg.Shares))

	sessions := session.New(session.Options{
		IdleTimeout: cfg.IdleTimeout,
		Lifetime:    cfg.SessionLifetime,
		MaxSessions: cfg.MaxSessions,
	})
	var a *api.API
	m := metrics.New(version, commit, metrics.Gauges{
		Sessions:        sessions.Len,
		ThumbnailMemory: func() (int64, int64) { return a.ThumbnailMemory() },
	})
	a = api.New(cfg, sessions, smb.LoginGroup, log, version, m)
	srv := server.New(cfg, sessions, a, m, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	stopTracing, err := tracing.Init(ctx, version)
	if err != nil {
		log.Error("ferry cannot start", "err", err)
		return 1
	}
	defer stopTracing()
	if err := srv.Run(ctx); err != nil {
		log.Error("ferry stopped", "err", err)
		return 1
	}
	log.Info("ferry stopped")
	return 0
}

func newLogger(level, format string) *slog.Logger {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil {
		l = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: l}
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
