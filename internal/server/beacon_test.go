package server

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/store"
)

const view1, view2 = "0123456789abcdef0123456789abcdef", "fedcba9876543210fedcba9876543210"

func sum(incs []store.Inc, metric, label string) int64 {
	var n int64
	for _, in := range incs {
		if in.Metric == metric && in.Label == label {
			n += in.N
		}
	}
	return n
}

func firstBeacon(view string) *beacon {
	return &beacon{Slug: "p", View: view, First: true, Source: "direct", Device: "desktop", Lang: "en", Theme: "dark"}
}

func TestVisitorsPerViewSum(t *testing.T) {
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	v := newVisitors("visitor-secret-visitor-secret-32b", nil)
	v.now = func() time.Time { return now }
	p := &content.Post{Slug: "p", Outbound: []string{"github.com"}}
	ip, ua := netip.MustParseAddr("192.0.2.7"), "UA"
	total := func(b *beacon, metric string) int64 {
		_, incs := v.apply(b, p, ip, ua)
		return sum(incs, metric, "")
	}

	b := firstBeacon(view1)
	b.Time = 3 * 3600
	if _, incs := v.apply(b, p, ip, ua); sum(incs, store.View, "") != 1 || sum(incs, store.ReadSecs, "") != 3*3600 {
		t.Fatalf("first delta = %+v", incs)
	}
	// Second delta of the same view: 2 h more, but the view's sum is clamped at 4 h.
	if got := total(&beacon{Slug: "p", View: view1, Time: 7200}, store.ReadSecs); got != 3600 {
		t.Errorf("clamped delta = %d, want 3600", got)
	}
	if got := total(&beacon{Slug: "p", View: view1, Time: 100}, store.ReadSecs); got != 0 {
		t.Errorf("past the clamp = %d, want 0", got)
	}
	// A new view of the same visitor: time counts, the view does not (daily dedup).
	_, incs := v.apply(func() *beacon { b := firstBeacon(view2); b.Time = 60; return b }(), p, ip, ua)
	if sum(incs, store.View, "") != 0 || sum(incs, store.ReadSecs, "") != 60 || sum(incs, store.ReadView, "") != 1 {
		t.Errorf("second view incs = %+v", incs)
	}
	// Unknown view id without first, repeated first, other post: ignored.
	for _, b := range []*beacon{{Slug: "p", View: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Time: 5}, firstBeacon(view1)} {
		if _, incs := v.apply(b, p, ip, ua); incs != nil {
			t.Errorf("ignored beacon gave %+v", incs)
		}
	}
	if _, incs := v.apply(&beacon{Slug: "q", View: view1, Time: 5}, &content.Post{Slug: "q"}, ip, ua); incs != nil {
		t.Errorf("view id reused for another post: %+v", incs)
	}
	// Depth once per visitor and post; outbound only to the post's own links.
	_, incs = v.apply(&beacon{Slug: "p", View: view2, Depth: []int{25, 50}, Outbound: []string{"github.com", "evil.example"}, Copies: 2}, p, ip, ua)
	if sum(incs, store.Depth, "25") != 1 || sum(incs, store.Outbound, "github.com") != 1 || sum(incs, store.Outbound, "evil.example") != 0 || sum(incs, store.Copy, "") != 2 {
		t.Errorf("incs = %+v", incs)
	}
	_, incs = v.apply(&beacon{Slug: "p", View: view1, Depth: []int{25, 100}}, p, ip, ua)
	if sum(incs, store.Depth, "25") != 0 || sum(incs, store.Depth, "100") != 1 {
		t.Errorf("depth dedup = %+v", incs)
	}
	// Next day: new daily key, the same visitor counts again.
	now = now.Add(24 * time.Hour)
	_, incs = v.apply(firstBeacon("11111111111111111111111111111111"), p, ip, ua)
	if sum(incs, store.View, "") != 1 {
		t.Errorf("next day view = %+v", incs)
	}
	// Another visitor (other /64) counts the same day.
	_, incs = v.apply(firstBeacon("22222222222222222222222222222222"), p, netip.MustParseAddr("2001:db8::1"), ua)
	if sum(incs, store.View, "") != 1 {
		t.Errorf("other visitor = %+v", incs)
	}
}

func TestVisitorsFirstCountsView(t *testing.T) {
	v := newVisitors("visitor-secret-visitor-secret-32b", nil)
	p := &content.Post{Slug: "p"}
	_, incs := v.apply(firstBeacon(view1), p, netip.MustParseAddr("192.0.2.1"), "UA")
	if sum(incs, store.View, "") != 1 || sum(incs, store.Source, "direct") != 1 || sum(incs, store.Theme, "dark") != 1 {
		t.Errorf("first = %+v", incs)
	}
}

func TestBeaconValidate(t *testing.T) {
	ok := []string{
		`{"slug":"p","view":"` + view1 + `","first":true,"time":5,"depth":[25],"source":"telegram","device":"mobile","lang":"ru","theme":"light"}`,
		`{"slug":"p","view":"` + view1 + `","time":14400,"outbound":["github.com"],"copies":100}`,
	}
	bad := []string{
		`{"slug":"p","view":"short"}`,
		`{"slug":"p","view":"` + view1 + `","time":-1}`,
		`{"slug":"p","view":"` + view1 + `","time":14401}`,
		`{"slug":"p","view":"` + view1 + `","depth":[30]}`,
		`{"slug":"p","view":"` + view1 + `","depth":[25,25]}`,
		`{"slug":"p","view":"` + view1 + `","source":"facebook"}`,
		`{"slug":"p","view":"` + view1 + `","first":true}`,
		`{"slug":"p","view":"` + view1 + `","copies":101}`,
		`{"slug":"p","view":"` + view1 + `","extra":1}`,
		`{"slug":"p","view":"` + view1 + `"}{}`,
		`[]`,
		`{"slug":"p","view":"` + view1 + `","outbound":["` + strings.Repeat("a", 254) + `"]}`,
	}
	for _, s := range ok {
		if status, err := decodeBeacon(s, "application/json"); status != 0 || err != nil {
			t.Errorf("rejected %s: %d %v", s, status, err)
		}
	}
	for _, s := range bad {
		if status, err := decodeBeacon(s, "application/json"); err == nil {
			t.Errorf("accepted %s (%d)", s, status)
		}
	}
	if status, _ := decodeBeacon(ok[0], "text/plain"); status != 415 {
		t.Errorf("text/plain = %d", status)
	}
	if status, _ := decodeBeacon(`{"slug":"`+strings.Repeat("a", 5000)+`"}`, "application/json; charset=utf-8"); status != 413 {
		t.Errorf("big body = %d", status)
	}
}

func decodeBeacon(body, ctype string) (int, error) {
	r := httptest.NewRequest("POST", "/api/beacon", strings.NewReader(body))
	r.Header.Set("Content-Type", ctype)
	var b beacon
	if status, err := decodeJSON(httptest.NewRecorder(), r, &b); err != nil {
		return status, err
	}
	return 0, b.validate()
}

func FuzzBeacon(f *testing.F) {
	f.Add(`{"slug":"p","view":"` + view1 + `","first":true,"time":5,"depth":[25,100],"source":"direct","device":"tablet","lang":"en","theme":"dark","outbound":["github.com"],"copies":1}`)
	f.Add(`{"slug":"p","view":"` + view1 + `","time":99999999999999999999}`)
	f.Add(`{"depth":[25,50,75,100,25]}`)
	v := newVisitors("visitor-secret-visitor-secret-32b", nil)
	p := &content.Post{Slug: "p", Outbound: []string{"github.com"}}
	f.Fuzz(func(t *testing.T, body string) {
		r := httptest.NewRequest("POST", "/api/beacon", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		var b beacon
		if _, err := decodeJSON(httptest.NewRecorder(), r, &b); err != nil || b.validate() != nil {
			return
		}
		_, incs := v.apply(&b, p, netip.MustParseAddr("192.0.2.1"), "UA")
		for _, in := range incs {
			if in.N < 0 || (in.Metric == store.ReadSecs && in.N > maxViewSeconds) {
				t.Fatalf("bad increment %+v from %s", in, body)
			}
		}
		for _, vs := range v.views {
			if vs.seconds > maxViewSeconds {
				t.Fatalf("view sum %d over the clamp", vs.seconds)
			}
		}
	})
}
