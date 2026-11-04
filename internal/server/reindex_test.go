package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/content"
)

const token = "0123456789abcdef0123456789abcdef"

func TestReindex(t *testing.T) {
	allow, _ := config.ParsePrefixes("10.0.0.0/8")
	trusted, _ := config.ParsePrefixes("10.0.0.5")
	var calls atomic.Int32
	var fail atomic.Bool
	sync := func(context.Context) error {
		calls.Add(1)
		if fail.Load() {
			return errors.New("s3 down")
		}
		return nil
	}
	cfg := testConfig()
	cfg.ReindexToken, cfg.ReindexAllow, cfg.TrustedProxies = token, allow, trusted
	h := New(slog.New(slog.DiscardHandler), cfg, new(atomic.Bool), sync, func() *content.Index { return nil })

	req := func(remote, xff, auth string) int {
		r := httptest.NewRequest("POST", "/reindex", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		if auth != "" {
			r.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	tests := []struct {
		name, remote, xff, auth string
		want                    int
	}{
		{"ok", "10.1.2.3:5000", "", "Bearer " + token, http.StatusNoContent},
		{"public peer", "203.0.113.9:5000", "", "Bearer " + token, http.StatusForbidden},
		{"spoofed xff from untrusted peer", "203.0.113.9:5000", "10.1.2.3", "Bearer " + token, http.StatusForbidden},
		{"public client via proxy", "10.0.0.5:5000", "203.0.113.9", "Bearer " + token, http.StatusForbidden},
		{"spoof left of real client", "10.0.0.5:5000", "10.9.9.9, 203.0.113.9", "Bearer " + token, http.StatusForbidden},
		{"garbage xff via proxy", "10.0.0.5:5000", "nonsense", "Bearer " + token, http.StatusForbidden},
		{"private client via proxy", "10.0.0.5:5000", "10.1.2.3", "Bearer " + token, http.StatusNoContent},
		{"no token", "10.1.2.3:5000", "", "", http.StatusUnauthorized},
		{"wrong token", "10.1.2.3:5000", "", "Bearer " + strings.Repeat("x", 32), http.StatusUnauthorized},
		{"token prefix", "10.1.2.3:5000", "", "Bearer " + token[:10], http.StatusUnauthorized},
		{"basic scheme", "10.1.2.3:5000", "", "Basic " + token, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		if got := req(tt.remote, tt.xff, tt.auth); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
	if calls.Load() != 2 {
		t.Errorf("sync calls = %d, want 2", calls.Load())
	}
	fail.Store(true)
	if got := req("10.1.2.3:5000", "", "Bearer "+token); got != http.StatusServiceUnavailable {
		t.Errorf("failed sync: got %d, want 503", got)
	}
	r := httptest.NewRequest("GET", "/reindex", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /reindex = %d, want 405", rec.Code)
	}
}

func TestClientIP(t *testing.T) {
	trusted, _ := config.ParsePrefixes("10.0.0.0/8,::1")
	tests := []struct {
		remote string
		xff    []string
		want   string
	}{
		{"203.0.113.1:1", nil, "203.0.113.1"},
		{"203.0.113.1:1", []string{"10.0.0.1"}, "203.0.113.1"},
		{"10.0.0.1:1", nil, "10.0.0.1"},
		{"10.0.0.1:1", []string{"198.51.100.7"}, "198.51.100.7"},
		{"10.0.0.1:1", []string{"1.1.1.1, 198.51.100.7, 10.0.0.2"}, "198.51.100.7"},
		{"10.0.0.1:1", []string{"1.1.1.1", "198.51.100.7"}, "198.51.100.7"},
		{"10.0.0.1:1", []string{"10.0.0.3, 10.0.0.2"}, "10.0.0.3"},
		{"[::1]:1", []string{"2001:db8::1"}, "2001:db8::1"},
		{"[::ffff:10.0.0.1]:1", []string{"::ffff:198.51.100.7"}, "198.51.100.7"},
		{"10.0.0.1:1", []string{"bad"}, "invalid IP"},
		{"10.0.0.1:1", []string{"198.51.100.7,"}, "invalid IP"},
		{"not-an-addr", nil, "invalid IP"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		for _, v := range tt.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientIP(r, trusted).String(); got != tt.want {
			t.Errorf("clientIP(%s, %q) = %s, want %s", tt.remote, tt.xff, got, tt.want)
		}
	}
}
