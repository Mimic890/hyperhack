package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	var ready atomic.Bool
	h := New(slog.New(slog.DiscardHandler), &ready)

	if got := do(h, "GET", "/healthz").Code; got != http.StatusServiceUnavailable {
		t.Errorf("healthz before ready = %d, want 503", got)
	}
	ready.Store(true)
	rec := do(h, "GET", "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body)
	}
	if got := do(h, "POST", "/healthz").Code; got != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz = %d, want 405", got)
	}
	rec = do(h, "GET", "/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", rec.Code)
	}
	for _, k := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy", "Cross-Origin-Opener-Policy", "X-Frame-Options", "Permissions-Policy"} {
		if rec.Header().Get(k) == "" {
			t.Errorf("missing header %s on 404", k)
		}
	}
}

func TestPanicRecovery(t *testing.T) {
	h := middleware(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret internal detail")
	}))
	rec := do(h, "GET", "/")
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusInternalServerError || string(body) != "Internal Server Error\n" {
		t.Errorf("got %d %q", rec.Code, body)
	}
}
