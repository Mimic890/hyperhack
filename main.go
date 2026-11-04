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
	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/mirror"
	"github.com/Mimic890/hyperhack/internal/server"
	"github.com/Mimic890/hyperhack/internal/store"
	"github.com/Mimic890/hyperhack/internal/syncer"
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

	mir, err := mirror.Open(cfg.VaultDir)
	if err != nil {
		mainLog.Error("open vault dir", "err", err)
		return 1
	}
	st, err := store.Open(cfg.DataDir, log)
	if err != nil {
		mainLog.Error("open SQLite", "err", err)
		return 1
	}
	defer func() { _ = st.Close() }()
	bucket, err := syncer.NewS3(cfg.S3Endpoint, cfg.S3Region, cfg.S3Bucket, cfg.S3AccessKey, cfg.S3SecretKey)
	if err != nil {
		mainLog.Error("S3 client", "err", err)
		return 1
	}
	render := content.NewRenderer(cfg.DefaultLang)
	render.ImageSize = mir.ImageSize
	syn := syncer.New(bucket, mir, render, syncer.Options{
		PollInterval:  cfg.PollInterval,
		MaxPostBytes:  cfg.MaxPostBytes,
		MaxAssetBytes: cfg.MaxAssetBytes,
		MaxObjects:    cfg.MaxObjects,
		SiteURL:       cfg.SiteURL,
		IndexNowKey:   cfg.IndexNowKey,
	}, log)

	var ready atomic.Bool
	httpLog := log.With("component", "http")
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(log, cfg, server.Deps{Ready: &ready, Sync: syn.Sync, Index: syn.Index, Store: st, SyncStats: syn.Stats}),
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
	syncCtx, stopSync := context.WithCancel(ctx)
	defer stopSync()
	syncErr := make(chan error, 1)
	go func() { syncErr <- syn.Run(syncCtx, &ready) }()
	go prune(syncCtx, st, cfg.PopularDays)

	code := 0
	select {
	case err := <-errc:
		mainLog.Error("server failed", "err", err)
		return 1
	case err := <-syncErr:
		if err != nil {
			mainLog.Error("no local mirror and S3 unavailable", "err", err)
			code = 1
		}
	case <-ctx.Done():
	}
	mainLog.Info("shutting down")
	stopSync()
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
	return code
}

// Search query TTL and cap (SPEC → Data: bounded growth).
const (
	searchTTLDays = 90
	maxSearches   = 10000
)

// prune keeps the SQLite tables bounded: hourly, and once at start.
func prune(ctx context.Context, st *store.Store, popularDays int) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		now := time.Now()
		_ = st.Prune(ctx, store.Day(now.AddDate(0, 0, -searchTTLDays)), store.Day(now.AddDate(0, 0, -popularDays)), maxSearches) // logged by store
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
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
