// Package server holds the HTTP routes and middleware.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/store"
	"github.com/Mimic890/hyperhack/internal/syncer"
)

// Deps is what the handlers need from the rest of the service.
type Deps struct {
	Ready     *atomic.Bool                // gates /healthz
	Sync      func(context.Context) error // bucket sync for /reindex
	Index     func() *content.Index       // current posts, nil before the first load
	Store     *store.Store
	SyncStats func() syncer.Stats
}

// New returns the root handler. log is the base logger; components are added here.
func New(log *slog.Logger, cfg *config.Config, d Deps) http.Handler {
	p := newPages(log, cfg, d)
	ready := d.Ready
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", p.home)
	mux.HandleFunc("GET /posts/{slug}", p.post) // also /posts/<slug>.md
	mux.HandleFunc("GET /tags/{tag...}", p.tag)
	mux.HandleFunc("GET /search", p.search)
	mux.HandleFunc("GET /rss.xml", p.rss)
	mux.HandleFunc("GET /sitemap.xml", p.sitemap)
	mux.HandleFunc("GET /robots.txt", p.robots)
	mux.HandleFunc("GET /llms.txt", p.llms)
	if key := cfg.IndexNowKey; key != "" { // the config allows only [a-zA-Z0-9-]
		mux.HandleFunc("GET /"+key+".txt", func(w http.ResponseWriter, _ *http.Request) {
			writeText(w, "text/plain; charset=utf-8", key)
		})
	}
	mux.HandleFunc("POST /prefs", p.prefs)
	mux.HandleFunc("GET /static/style.css", p.style)
	mux.HandleFunc("GET /static/{script}", p.script)
	mux.HandleFunc("GET /api/altcha", p.apiAltcha)
	mux.HandleFunc("POST /api/vote", p.apiVote)
	mux.HandleFunc("POST /api/beacon", p.apiBeacon)
	mux.HandleFunc("GET /metrics", p.metricsPage)
	mux.HandleFunc("GET /static/fonts/{file}", p.font)
	mux.HandleFunc("GET /", p.notFound)
	mux.Handle("POST /reindex", reindex(p.log, cfg.ReindexToken, cfg.ReindexAllow, cfg.TrustedProxies, p.limit(p.lim.reindex, "reindex"), d.Sync))
	// The "GET /" catch-all would answer GET on POST-only paths with 404; SPEC wants 405.
	for _, path := range []string{"/reindex", "/prefs", "/api/vote", "/api/beacon"} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("not ready\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return middleware(p.log, p.metrics, mux)
}

// middleware sets security headers, recovers panics, counts and logs each request on DEBUG.
func middleware(log *slog.Logger, m *registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestID()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic in handler", "request_id", id, "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				if sw.status == 0 {
					http.Error(sw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}
			m.request(r.Pattern, sw.code(), time.Since(start)) // the mux sets Pattern on r
			log.Debug("request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", sw.code(), "duration", time.Since(start))
		}()
		securityHeaders(w.Header())
		next.ServeHTTP(sw, r)
	})
}

func securityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; worker-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; upgrade-insecure-requests")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), browsing-topics=()")
}

func requestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return hex.EncodeToString(b)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
