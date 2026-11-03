// Command hyperhack is a lightweight blog server for one author.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/server"
)

// addr is fixed: the container port is mapped in docker-compose.yml.
const addr = ":8080"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("invalid config", "component", "main", "err", err)
		return 1
	}
	log := newLogger(cfg)
	mainLog := log.With("component", "main")
	mainLog.Info("starting", cfg.Summary()...)

	var ready atomic.Bool
	// shortcut: no content index yet, so ready at once; set it after the first sync once sync exists.
	ready.Store(true)

	httpLog := log.With("component", "http")
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(httpLog, &ready),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(httpLog.Handler(), slog.LevelWarn),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	mainLog.Info("listening", "addr", addr)

	select {
	case err := <-errc:
		mainLog.Error("server failed", "err", err)
		return 1
	case <-ctx.Done():
	}
	mainLog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		mainLog.Error("shutdown", "err", err)
		return 1
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		mainLog.Error("server failed", "err", err)
		return 1
	}
	return 0
}

func newLogger(cfg *config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

// healthcheck is for the Compose healthcheck: the image has no curl.
func healthcheck() int {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://127.0.0.1" + addr + "/healthz")
	if err != nil {
		return 1
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
