package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/ratelimit"
	"github.com/Mimic890/hyperhack/internal/store"
)

const (
	maxAPIBody      = 4 << 10
	maxViewSeconds  = 4 * 60 * 60 // time-on-page clamp, applied to the sum of a view's deltas
	maxOutboundSend = 20
	maxCopiesSend   = 100
	maxViews        = 100000 // open page views tracked in memory
	maxSeen         = 200000 // visitor × post entries per day
	viewTTL         = 24 * time.Hour
)

var (
	viewIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	sources  = []string{"telegram", "google", "yandex", "bing", "github", "direct", "internal", "other"}
	devices  = []string{"mobile", "tablet", "desktop"}
	depths   = []int{25, 50, 75, 100}
)

// beacon is one delta from the post script: what happened since its last send.
type beacon struct {
	Slug     string   `json:"slug"`
	View     string   `json:"view"`  // random id of this page view, the same in every delta
	First    bool     `json:"first"` // first delta of the view: counts the view
	Time     int      `json:"time"`  // visible seconds since the last delta
	Depth    []int    `json:"depth"` // read-depth markers reached since the last delta
	Source   string   `json:"source"`
	Device   string   `json:"device"`
	Lang     string   `json:"lang"`
	Theme    string   `json:"theme"`
	Outbound []string `json:"outbound"` // hosts of external links clicked
	Copies   int      `json:"copies"`   // copy events inside code blocks
}

var errBadInput = errors.New("bad input")

// decodeJSON reads exactly one JSON object with known fields from a small body.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) (int, error) {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return http.StatusUnsupportedMediaType, errBadInput
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxAPIBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return http.StatusRequestEntityTooLarge, err
		}
		return http.StatusBadRequest, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return http.StatusBadRequest, errors.New("more than one JSON value")
	}
	return 0, nil
}

// validate checks types and ranges. Whether the slug exists is checked by the caller.
func (b *beacon) validate() error {
	ok := viewIDRe.MatchString(b.View) &&
		b.Time >= 0 && b.Time <= maxViewSeconds &&
		len(b.Depth) <= len(depths) && len(b.Outbound) <= maxOutboundSend &&
		b.Copies >= 0 && b.Copies <= maxCopiesSend &&
		enumOK(b.Source, sources) && enumOK(b.Device, devices) && enumOK(b.Lang, langs) && enumOK(b.Theme, themes)
	if b.First {
		ok = ok && b.Source != "" && b.Device != "" && b.Lang != "" && b.Theme != ""
	}
	for i, d := range b.Depth {
		ok = ok && slices.Contains(depths, d) && !slices.Contains(b.Depth[:i], d)
	}
	for _, h := range b.Outbound {
		ok = ok && h != "" && len(h) <= 253
	}
	if !ok {
		return errBadInput
	}
	return nil
}

func enumOK(v string, allowed []string) bool { return v == "" || slices.Contains(allowed, v) }

// viewState is the running sum of one page view.
type viewState struct {
	slug    string
	seconds int
	created time.Time
}

// visitors turns beacons into counter increments: views and depth once per visitor
// per post per day (daily HMAC hash), time summed and clamped per page view.
// Nothing here is written to disk; the daily key is dropped at midnight UTC.
type visitors struct {
	secret []byte
	now    func() time.Time

	mu        sync.Mutex
	day       string
	key       []byte                // HMAC(VISITOR_SECRET, day)
	seen      map[[16]byte]uint8    // hash(visitor, slug) → bit 0 viewed, bits 1–4 depth markers
	views     map[string]*viewState // page view id → running sum
	lastPurge time.Time
	fullWarn  func(what string) // called once per day when a map is full
	warned    map[string]bool
}

func newVisitors(secret string, warn func(string)) *visitors {
	return &visitors{secret: []byte(secret), now: time.Now, views: map[string]*viewState{}, fullWarn: warn}
}

// visitorKey is HMAC(daily_key, ip_prefix + user_agent) bound to the post. Caller holds mu.
func (v *visitors) visitorKey(ip netip.Addr, ua, slug string) [16]byte {
	m := hmac.New(sha256.New, v.key)
	m.Write([]byte(ratelimit.Key(ip).String()))
	m.Write([]byte{0})
	m.Write([]byte(ua))
	visitor := m.Sum(nil)
	h := sha256.New()
	h.Write(visitor)
	h.Write([]byte(slug))
	var k [16]byte
	copy(k[:], h.Sum(nil))
	return k
}

func (v *visitors) rollDay(now time.Time) {
	day := store.Day(now)
	if day == v.day {
		return
	}
	m := hmac.New(sha256.New, v.secret)
	m.Write([]byte(day))
	v.day, v.key, v.seen, v.warned = day, m.Sum(nil), map[[16]byte]uint8{}, map[string]bool{}
}

func (v *visitors) warnFull(what string) {
	if !v.warned[what] {
		v.warned[what] = true
		if v.fullWarn != nil {
			v.fullWarn(what)
		}
	}
}

// apply returns the counter increments for one validated beacon on post p.
func (v *visitors) apply(b *beacon, p *content.Post, ip netip.Addr, ua string) (day string, incs []store.Inc) {
	now := v.now()
	v.mu.Lock()
	defer v.mu.Unlock()
	v.rollDay(now)
	if now.Sub(v.lastPurge) > time.Minute {
		for id, vs := range v.views {
			if now.Sub(vs.created) > viewTTL {
				delete(v.views, id)
			}
		}
		v.lastPurge = now
	}

	vs := v.views[b.View]
	switch {
	case vs == nil && !b.First:
		return v.day, nil // unknown view (expired, restart): a view starts only with its first delta
	case vs == nil && len(v.views) >= maxViews:
		v.warnFull("page views")
		return v.day, nil
	case vs == nil:
		vs = &viewState{slug: p.Slug, created: now}
		v.views[b.View] = vs
	case vs.slug != p.Slug || b.First:
		return v.day, nil // id reused for another post, or a repeated first delta
	}
	inc := func(metric, label string, n int) {
		incs = append(incs, store.Inc{Slug: p.Slug, Metric: metric, Label: label, N: int64(n)})
	}

	k := v.visitorKey(ip, ua, p.Slug)
	mask, known := v.seen[k]
	if !known && len(v.seen) >= maxSeen {
		v.warnFull("visitors")
	} else {
		if b.First && mask&1 == 0 {
			mask |= 1
			inc(store.View, "", 1)
			inc(store.Source, b.Source, 1)
			inc(store.Device, b.Device, 1)
			inc(store.Lang, b.Lang, 1)
			inc(store.Theme, b.Theme, 1)
		}
		for _, d := range b.Depth {
			if bit := uint8(2) << slices.Index(depths, d); mask&bit == 0 {
				mask |= bit
				inc(store.Depth, strconv.Itoa(d), 1)
			}
		}
		v.seen[k] = mask
	}

	if add := min(b.Time, maxViewSeconds-vs.seconds); add > 0 {
		if vs.seconds == 0 {
			inc(store.ReadView, "", 1) // views with reading time: the divisor of the average
		}
		vs.seconds += add
		inc(store.ReadSecs, "", add)
	}
	for _, h := range b.Outbound {
		if slices.Contains(p.Outbound, h) { // labels stay bounded by the post's own links
			inc(store.Outbound, h, 1)
		}
	}
	if b.Copies > 0 {
		inc(store.Copy, "", b.Copies)
	}
	return v.day, incs
}
