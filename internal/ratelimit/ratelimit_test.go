package ratelimit

import (
	"net/netip"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	l := New(3, time.Minute)
	l.now = func() time.Time { return now }
	a := netip.MustParseAddr("192.0.2.1")
	for i := range 3 {
		if ok, _ := l.Allow(a); !ok {
			t.Fatalf("request %d denied", i)
		}
	}
	ok, wait := l.Allow(a)
	if ok || wait != 20*time.Second || RetryAfter(wait) != 20 {
		t.Fatalf("4th = %v %v", ok, wait)
	}
	if ok, _ := l.Allow(netip.MustParseAddr("192.0.2.2")); !ok {
		t.Error("other client limited")
	}
	now = now.Add(20 * time.Second) // one token back
	if ok, _ := l.Allow(a); !ok {
		t.Error("no refill")
	}
	if ok, _ := l.Allow(a); ok {
		t.Error("refilled too much")
	}
	now = now.Add(time.Hour) // refill caps at the burst
	for range 3 {
		l.Allow(a)
	}
	if ok, _ := l.Allow(a); ok {
		t.Error("burst above N")
	}
}

func TestKeyIPv6Slash64(t *testing.T) {
	l := New(1, time.Minute)
	l.Allow(netip.MustParseAddr("2001:db8:1:2::1"))
	if ok, _ := l.Allow(netip.MustParseAddr("2001:db8:1:2:ffff::9")); ok {
		t.Error("same /64 not grouped")
	}
	if ok, _ := l.Allow(netip.MustParseAddr("2001:db8:1:3::1")); !ok {
		t.Error("other /64 limited")
	}
	if Key(netip.MustParseAddr("::ffff:192.0.2.1")) != netip.MustParsePrefix("192.0.2.1/32") {
		t.Error("mapped IPv4 not unmapped")
	}
	l.Allow(netip.Addr{})
	if ok, _ := l.Allow(netip.Addr{}); ok {
		t.Error("unknown clients do not share a bucket")
	}
}

func TestMaxClients(t *testing.T) {
	l := New(1, time.Hour)
	l.max = 3
	ips := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4"}
	for _, s := range ips {
		l.Allow(netip.MustParseAddr(s))
	}
	if len(l.byIP) != 3 || l.lru.Len() != 3 {
		t.Fatalf("tracked %d", len(l.byIP))
	}
	// The oldest (.1) was evicted, so it gets a fresh bucket; .4 is still limited.
	if ok, _ := l.Allow(netip.MustParseAddr("192.0.2.1")); !ok {
		t.Error("evicted client still limited")
	}
	if ok, _ := l.Allow(netip.MustParseAddr("192.0.2.4")); ok {
		t.Error("recent client forgotten")
	}
}
