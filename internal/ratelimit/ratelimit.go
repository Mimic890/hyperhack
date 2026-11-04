// Package ratelimit is an in-memory token bucket per client address.
package ratelimit

import (
	"container/list"
	"math"
	"net/netip"
	"sync"
	"time"
)

// MaxClients bounds memory: past it, the least recently seen client is evicted.
const MaxClients = 10000

// Limiter allows N requests per Per for each client, with bursts up to N.
type Limiter struct {
	n    float64
	per  time.Duration
	max  int
	now  func() time.Time
	mu   sync.Mutex
	byIP map[netip.Prefix]*list.Element
	lru  *list.List // front: most recently seen
}

type bucket struct {
	key    netip.Prefix
	tokens float64
	last   time.Time
}

// New returns a limiter for n requests per per.
func New(n int, per time.Duration) *Limiter {
	return &Limiter{n: float64(n), per: per, max: MaxClients, now: time.Now, byIP: map[netip.Prefix]*list.Element{}, lru: list.New()}
}

// Key groups IPv6 clients by /64 (one subscriber); IPv4 is one address.
// The zero Addr (unknown client) shares one bucket.
func Key(ip netip.Addr) netip.Prefix {
	ip = ip.Unmap()
	bits := 32
	if ip.Is6() {
		bits = 64
	}
	p, _ := ip.Prefix(bits) // zero Addr gives the zero Prefix
	return p
}

// Allow takes a token for ip. If none is left it returns false and when to retry.
func (l *Limiter) Allow(ip netip.Addr) (bool, time.Duration) {
	key := Key(ip)
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	var b *bucket
	if e, ok := l.byIP[key]; ok {
		b = e.Value.(*bucket)
		l.lru.MoveToFront(e)
		b.tokens = min(l.n, b.tokens+now.Sub(b.last).Seconds()*l.n/l.per.Seconds())
		b.last = now
	} else {
		if l.lru.Len() >= l.max {
			old := l.lru.Back()
			l.lru.Remove(old)
			delete(l.byIP, old.Value.(*bucket).key)
		}
		b = &bucket{key: key, tokens: l.n, last: now}
		l.byIP[key] = l.lru.PushFront(b)
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	wait := time.Duration((1 - b.tokens) * float64(l.per) / l.n)
	return false, wait
}

// RetryAfter is the Retry-After header value: whole seconds, at least 1.
func RetryAfter(d time.Duration) int { return max(1, int(math.Ceil(d.Seconds()))) }
