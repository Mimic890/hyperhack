package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sync/atomic"
	"testing"

	"github.com/Mimic890/hyperhack/internal/content"
)

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestRoutes(t *testing.T) {
	var ready atomic.Bool
	h := New(slog.New(slog.DiscardHandler), testConfig(), &ready, nil, func() *content.Index { return nil })

	if got := do(h, "GET", "/healthz").Code; got != http.StatusServiceUnavailable {
		t.Errorf("healthz before ready = %d, want 503", got)
	}
	ready.Store(true)
	rec := do(h, "GET", "/healthz")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
		t.Errorf("healthz = %d %q", rec.Code, rec.Body)
	}
	for _, r := range []struct{ method, path string }{{"POST", "/healthz"}, {"GET", "/prefs"}, {"PUT", "/reindex"}, {"POST", "/posts/x"}, {"DELETE", "/"}} {
		if got := do(h, r.method, r.path).Code; got != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", r.method, r.path, got)
		}
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

// TestCSPMatchesSpec keeps the CSP header identical to SPEC → Security headers.
func TestCSPMatchesSpec(t *testing.T) {
	re := regexp.MustCompile("`Content-Security-Policy: (default-src 'self'[^`]*)`")
	h := New(slog.New(slog.DiscardHandler), testConfig(), new(atomic.Bool), nil, func() *content.Index { return nil })
	got := do(h, "GET", "/healthz").Header().Get("Content-Security-Policy")
	for _, f := range []string{"../../SPEC.md", "../../SPEC.ru.md"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := re.FindAllSubmatch(b, -1)
		if len(m) != 1 {
			t.Fatalf("%s: want one service CSP, found %d", f, len(m))
		}
		if want := string(m[0][1]); got != want {
			t.Errorf("CSP differs from %s:\ngot  %s\nwant %s", f, got, want)
		}
	}
}
