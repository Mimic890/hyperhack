package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/altcha-org/altcha-lib-go/v2"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/store"
)

func (s *testSite) do(method, path, body string, hdr map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, r)
	return rec
}

var jsonSameOrigin = map[string]string{"Content-Type": "application/json", "Origin": "https://example.com", "Sec-Fetch-Site": "same-origin"}

func beaconBody(view string, first bool, time int) string {
	b := map[string]any{"slug": "zametka", "view": view, "first": first, "time": time}
	if first {
		b["source"], b["device"], b["lang"], b["theme"] = "telegram", "mobile", "ru", "dark"
	}
	out, _ := json.Marshal(b)
	return string(out)
}

func TestOriginCheck(t *testing.T) {
	s := seoSite(t)
	body := beaconBody(view1, true, 1)
	for name, hdr := range map[string]map[string]string{
		"no origin":    {"Content-Type": "application/json"},
		"other origin": {"Content-Type": "application/json", "Origin": "https://evil.example"},
		"http origin":  {"Content-Type": "application/json", "Origin": "http://example.com"},
		"cross-site":   {"Content-Type": "application/json", "Origin": "https://example.com", "Sec-Fetch-Site": "cross-site"},
		"same-site":    {"Content-Type": "application/json", "Origin": "https://example.com", "Sec-Fetch-Site": "same-site"},
	} {
		if rec := s.do("POST", "/api/beacon", body, hdr); rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, rec.Code)
		}
		if rec := s.do("POST", "/api/vote", `{}`, hdr); rec.Code != http.StatusForbidden {
			t.Errorf("vote %s: %d, want 403", name, rec.Code)
		}
	}
	if rec := s.do("GET", "/api/altcha", "", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site altcha = %d", rec.Code)
	}
	if rec := s.do("POST", "/api/beacon", body, jsonSameOrigin); rec.Code != http.StatusNoContent {
		t.Errorf("same origin = %d", rec.Code)
	}
	if rec := s.do("POST", "/api/beacon", body, map[string]string{"Content-Type": "text/plain", "Origin": "https://example.com"}); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain = %d", rec.Code)
	}
	if rec := s.do("GET", "/api/beacon", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET beacon = %d", rec.Code)
	}
}

func TestBeaconToMetrics(t *testing.T) {
	s := seoSite(t)
	send := func(body string, want int) {
		t.Helper()
		if rec := s.do("POST", "/api/beacon", body, jsonSameOrigin); rec.Code != want {
			t.Fatalf("beacon %s = %d, want %d", body, rec.Code, want)
		}
	}
	send(beaconBody(view1, true, 3*3600), 204)
	send(beaconBody(view1, false, 2*3600), 204) // view sum clamped at 4 h
	send(`{"slug":"zametka","view":"`+view1+`","depth":[25,100],"copies":2}`, 204)
	send(beaconBody(view2, true, 10), 204) // same visitor: no new view
	send(strings.Replace(beaconBody(view2, true, 1), "zametka", "nope", 1), 404)
	send(strings.Replace(beaconBody(view2, true, 1), "telegram", "facebook", 1), 400)

	mustContain(t, s.get("/posts/zametka").Body.String(), "Views: 1")
	m := s.get("/metrics").Body.String()
	mustContain(t, m,
		`hyperhack_post_views_total{slug="zametka"} 1`,
		`hyperhack_post_read_seconds_total{slug="zametka"} 14410`,
		`hyperhack_post_read_views_total{slug="zametka"} 2`,
		`hyperhack_post_read_depth_total{slug="zametka",depth="100"} 1`,
		`hyperhack_post_source_total{slug="zametka",source="telegram"} 1`,
		`hyperhack_post_code_copies_total{slug="zametka"} 2`,
		`hyperhack_sync_objects 3`,
		`hyperhack_http_requests_total{route="POST /api/beacon",code="204"} 4`,
		`hyperhack_http_request_duration_seconds_bucket{route="POST /api/beacon",le="+Inf"} `,
	)
}

func solve(t *testing.T, s *testSite) altcha.Payload {
	t.Helper()
	rec := s.do("GET", "/api/altcha", "", nil)
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("altcha = %d", rec.Code)
	}
	var ch altcha.Challenge
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil {
		t.Fatal(err)
	}
	sol, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: ch, DeriveKey: altcha.DeriveKeySHA()})
	if err != nil || sol == nil {
		t.Fatalf("solve: %v", err)
	}
	return altcha.Payload{Challenge: ch, Solution: *sol}
}

func voteBody(t *testing.T, vote string, p altcha.Payload) string {
	b, err := json.Marshal(voteRequest{Slug: "zametka", Vote: vote, Altcha: p})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestVote(t *testing.T) {
	s := seoSite(t)
	p := solve(t, s)
	rec := s.do("POST", "/api/vote", voteBody(t, "like", p), jsonSameOrigin)
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"likes":1,"dislikes":0,"vote":"like"}` {
		t.Fatalf("vote = %d %s", rec.Code, rec.Body)
	}
	var vid *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "vid" {
			vid = c
		}
	}
	if vid == nil || !vidRe.MatchString(vid.Value) || !vid.HttpOnly || !vid.Secure || vid.SameSite != http.SameSiteLaxMode {
		t.Fatalf("vid cookie = %+v", vid)
	}
	// Replay of the same solution.
	if rec := s.do("POST", "/api/vote", voteBody(t, "like", p), jsonSameOrigin, vid); rec.Code != http.StatusForbidden {
		t.Errorf("replay = %d", rec.Code)
	}
	page := s.get("/posts/zametka", vid).Body.String()
	mustContain(t, page, `data-vote="like" aria-pressed="true"`, `<span class="n">1</span>`, `src="/static/altcha.js?v=`, `src="/static/post.js?v=`)
	// Change to dislike with the same cookie, then remove.
	if rec := s.do("POST", "/api/vote", voteBody(t, "dislike", solve(t, s)), jsonSameOrigin, vid); !strings.Contains(rec.Body.String(), `{"likes":0,"dislikes":1,"vote":"dislike"}`) {
		t.Errorf("change = %d %s", rec.Code, rec.Body)
	}
	if rec := s.do("POST", "/api/vote", voteBody(t, "none", solve(t, s)), jsonSameOrigin, vid); !strings.Contains(rec.Body.String(), `{"likes":0,"dislikes":0,"vote":"none"}`) {
		t.Errorf("remove = %d %s", rec.Code, rec.Body)
	}
	// Bad inputs.
	if rec := s.do("POST", "/api/vote", voteBody(t, "love", solve(t, s)), jsonSameOrigin); rec.Code != 400 {
		t.Errorf("bad vote = %d", rec.Code)
	}
	bad := solve(t, s)
	bad.Solution.DerivedKey = strings.Repeat("ab", 32)
	if rec := s.do("POST", "/api/vote", voteBody(t, "like", bad), jsonSameOrigin); rec.Code != http.StatusForbidden {
		t.Errorf("wrong solution = %d", rec.Code)
	}
	if rec := s.do("POST", "/api/vote", `{"slug":"zametka","vote":"like"}`, jsonSameOrigin); rec.Code != http.StatusForbidden {
		t.Errorf("no altcha = %d", rec.Code)
	}
	mustContain(t, s.get("/metrics").Body.String(), `hyperhack_post_votes_total{slug="zametka",vote="like"} 1`, `hyperhack_post_votes_total{slug="zametka",vote="dislike"} 1`, `hyperhack_post_likes{slug="zametka"} 0`)
}

func TestRateLimits(t *testing.T) {
	cfg := testConfig()
	one := config.Rate{N: 1, Per: time.Hour}
	cfg.RateBeacon, cfg.RateSearch, cfg.RateVote = one, one, one
	s := newSite(t, cfg, seoSite(t).idx.Load())
	if rec := s.do("POST", "/api/beacon", beaconBody(view1, true, 1), jsonSameOrigin); rec.Code != 204 {
		t.Fatalf("first beacon = %d", rec.Code)
	}
	rec := s.do("POST", "/api/beacon", beaconBody(view1, false, 1), jsonSameOrigin)
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "3600" {
		t.Errorf("second beacon = %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	s.get("/search?q=go")
	rec = s.get("/search?q=go")
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "Too many requests") || rec.Header().Get("Retry-After") == "" {
		t.Errorf("search = %d", rec.Code)
	}
	// /api/altcha and /api/vote share RATE_LIMIT_VOTE.
	s.do("GET", "/api/altcha", "", nil)
	if rec := s.do("POST", "/api/vote", `{}`, jsonSameOrigin); rec.Code != http.StatusTooManyRequests {
		t.Errorf("vote after altcha = %d", rec.Code)
	}
	// Another client is not affected.
	r := httptest.NewRequest("GET", "/search?q=go", nil)
	r.RemoteAddr = "198.51.100.7:1000"
	rr := httptest.NewRecorder()
	s.h.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Errorf("other client = %d", rr.Code)
	}
}

func TestMetricsAccess(t *testing.T) {
	s := seoSite(t)
	for remote, want := range map[string]int{"203.0.113.9:1": 403, "10.1.2.3:1": 200, "[2001:db8::1]:1": 403, "127.0.0.1:1": 200} {
		r := httptest.NewRequest("GET", "/metrics", nil)
		r.RemoteAddr = remote
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, r)
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", remote, rec.Code, want)
		}
		if want == 200 && rec.Header().Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" {
			t.Errorf("content type %q", rec.Header().Get("Content-Type"))
		}
	}
}

func TestSearchStats(t *testing.T) {
	s := seoSite(t)
	s.do("GET", "/search?q=bl", "", map[string]string{"X-Search": "live"})
	s.do("GET", "/search?q=Bleve!", "", map[string]string{"X-Search": "final"})
	s.get("/search?q=nothinghere")
	s.get("/search?q=go&page=2") // later pages are not counted
	top, _ := s.d.Store.TopSearches(context.Background(), 10, false)
	if len(top) != 2 || top[0].Query != "bleve" && top[1].Query != "bleve" {
		t.Errorf("stored = %v", top)
	}
	zero, _ := s.d.Store.TopSearches(context.Background(), 10, true)
	if len(zero) != 1 || zero[0].Query != "nothinghere" {
		t.Errorf("zero = %v", zero)
	}
	mustContain(t, s.get("/metrics").Body.String(), "hyperhack_search_queries_total 2", "hyperhack_search_zero_results_total 1",
		`hyperhack_search_top_zero_result_queries{query="nothinghere"} 1`, "hyperhack_search_duration_seconds_count 4")
}

func TestPopular(t *testing.T) {
	s := seoSite(t)
	ctx := context.Background()
	_ = s.d.Store.Add(ctx, store.Day(time.Now()), store.Inc{Slug: "zametka", Metric: store.View, N: 5}, store.Inc{Slug: "go-script-tips", Metric: store.View, N: 9})
	_ = s.d.Store.Add(ctx, store.Day(time.Now().AddDate(0, 0, -40)), store.Inc{Slug: "zametka", Metric: store.View, N: 100}) // outside POPULAR_DAYS
	body := s.get("/").Body.String()
	i, j := strings.Index(body, `<section class="popular"`), strings.LastIndex(body, `href="/posts/zametka"`)
	if i < 0 || !strings.Contains(body[i:], `<li><a href="/posts/go-script-tips">`) || j < i ||
		strings.Index(body[i:], "go-script-tips") > strings.Index(body[i:], "zametka") {
		t.Errorf("popular section wrong:\n%s", body[max(i, 0):])
	}
	if strings.Contains(s.get("/?page=2").Body.String(), `class="popular"`) {
		t.Error("popular on page 2")
	}
}

func TestStaticScripts(t *testing.T) {
	s := seoSite(t)
	for _, name := range []string{"search.js", "altcha.js", "post.js"} {
		if rec := s.get("/static/" + name); rec.Code != 200 || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
			t.Errorf("%s = %d", name, rec.Code)
		}
	}
	if rec := s.get("/static/other.js"); rec.Code != 404 {
		t.Errorf("other.js = %d", rec.Code)
	}
}
