// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/wenisch-tech/s3webui/downstream/internal/app"
)

// Set by scripts/package-release.ps1 for published binaries.
var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func main() {
	showVersion := flag.Bool("version", false, "Print version, source commit, build date and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("s3webui %s (commit %s, built %s)\n", version, commit, buildDate)
		return
	}
	if e := run(); e != nil {
		slog.Error("server stopped", "error", e)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	c := app.EnvConfig()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	s, e := app.New(startup, c)
	cancel()
	if e != nil {
		return e
	}
	defer s.Close()
	srv := &http.Server{Addr: c.Address, Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { slog.Info("listening", "address", c.Address); done <- srv.ListenAndServe() }()
	select {
	case e = <-done:
		if !errors.Is(e, http.ErrServerClosed) {
			return e
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
	return nil
}
