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
)

// New returns the root handler. ready gates /healthz; sync runs a bucket sync for /reindex;
// index returns the current posts (nil before the first load).
func New(log *slog.Logger, cfg *config.Config, ready *atomic.Bool, sync func(context.Context) error, index func() *content.Index) http.Handler {
	p := newPages(log, cfg, index)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", p.home)
	mux.HandleFunc("GET /posts/{slug}", p.post)
	mux.HandleFunc("POST /prefs", p.prefs)
	mux.HandleFunc("GET /static/style.css", p.style)
	mux.HandleFunc("GET /static/fonts/{file}", p.font)
	mux.HandleFunc("GET /", p.notFound)
	mux.Handle("POST /reindex", reindex(log, cfg.ReindexToken, cfg.ReindexAllow, cfg.TrustedProxies, sync))
	// The "GET /" catch-all would answer GET on POST-only paths with 404; SPEC wants 405.
	for _, path := range []string{"/reindex", "/prefs"} {
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
	return middleware(log, mux)
}

// middleware sets security headers, recovers panics and logs each request on DEBUG.
func middleware(log *slog.Logger, next http.Handler) http.Handler {
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
