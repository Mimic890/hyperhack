package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

// reindexWriteTimeout replaces the server WriteTimeout: a sync can take longer than 30 s.
const reindexWriteTimeout = 10 * time.Minute

// reindex handles POST /reindex: private network only, RATE_LIMIT_REINDEX, Bearer token, waits for the sync.
func reindex(log *slog.Logger, token string, allow, trusted []netip.Prefix, limited func(http.ResponseWriter, *http.Request) bool, sync func(context.Context) error) http.HandlerFunc {
	want := sha256.Sum256([]byte(token))
	return func(w http.ResponseWriter, r *http.Request) {
		if !contains(allow, clientIP(r, trusted)) {
			log.Warn("reindex denied", "reason", "address not in REINDEX_ALLOW")
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if limited(w, r) { // before the token check: limits guessing
			return
		}
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		// Hash both sides so the compare takes the same time for any token length.
		gotSum := sha256.Sum256([]byte(got))
		if !ok || subtle.ConstantTimeCompare(gotSum[:], want[:]) != 1 {
			log.Warn("reindex denied", "reason", "bad token")
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		log.Info("reindex")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(reindexWriteTimeout))
		if err := sync(r.Context()); err != nil {
			http.Error(w, "sync failed", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// clientIP is the TCP peer, or the last X-Forwarded-For hop before the trusted proxies.
// It returns the zero Addr (matched by no prefix) if a trusted proxy sent a bad value.
func clientIP(r *http.Request, trusted []netip.Prefix) netip.Addr {
	ap, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}
	}
	ip := ap.Addr().Unmap()
	if !contains(trusted, ip) {
		return ip
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		h := strings.TrimSpace(hops[i])
		if h == "" && i == 0 && len(hops) == 1 {
			return ip // no header: the trusted proxy itself
		}
		a, err := netip.ParseAddr(h)
		if err != nil {
			return netip.Addr{}
		}
		if ip = a.Unmap(); !contains(trusted, ip) {
			return ip
		}
	}
	return ip
}

func contains(prefixes []netip.Prefix, ip netip.Addr) bool {
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}
