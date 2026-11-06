package server

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/store"
	"github.com/Mimic890/hyperhack/internal/syncer"
)

func testConfig() *config.Config {
	u, _ := url.Parse("https://example.com")
	private, _ := config.ParsePrefixes("127.0.0.0/8,10.0.0.0/8,192.0.2.0/24")
	lots := config.Rate{N: 1000, Per: time.Minute}
	return &config.Config{
		SiteName: "Test <Blog>", SiteURL: u, DefaultLang: "en", ReadingWPM: 200, PageSize: 2,
		AltchaHMACKey: "0123456789abcdef0123456789abcdef", VisitorSecret: "visitor-secret-visitor-secret-32b",
		AltchaExpires: 5 * time.Minute, AltchaMaxNumber: 1000, PopularDays: 30, PopularLimit: 5,
		RateVote: lots, RateBeacon: lots, RateSearch: lots, RateReindex: lots, MetricsAllow: private,
	}
}

// testDeps returns working dependencies with a fresh SQLite store.
func testDeps(t *testing.T, index func() *content.Index) Deps {
	t.Helper()
	st, err := store.Open(t.TempDir(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ready := new(atomic.Bool)
	ready.Store(true)
	return Deps{Ready: ready, Index: index, Store: st, SyncStats: func() syncer.Stats { return syncer.Stats{Objects: 3} }}
}

func testIndex(n int) *content.Index {
	var posts []*content.Post
	for i := range n {
		posts = append(posts, &content.Post{
			Key: string(rune('a'+i)) + ".md", Slug: "post-" + string(rune('a'+i)), Title: "Post " + string(rune('A'+i)),
			Date: time.Date(2026, 11, 1+i, 0, 0, 0, 0, time.UTC), Lang: "en", Words: 450, HTML: "<p>body</p>",
		})
	}
	posts = append(posts, &content.Post{
		Key: "x.md", Slug: "xss", Title: `<script>alert(1)</script>`, Description: `"><img src=x onerror=alert(1)>`,
		Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), Updated: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		Lang: "ru", Cover: "assets/covers/a b#c.webp", Words: 10, HTML: template.HTML("<p>Привет</p>"),
	})
	return content.NewIndex(posts, slog.New(slog.DiscardHandler))
}

type testSite struct {
	h   http.Handler
	idx atomic.Pointer[content.Index]
	d   Deps
}

func newSite(t *testing.T, cfg *config.Config, ix *content.Index) *testSite {
	t.Helper()
	s := &testSite{}
	s.idx.Store(ix)
	s.d = testDeps(t, s.idx.Load)
	s.h = New(slog.New(slog.DiscardHandler), cfg, s.d)
	return s
}

func (s *testSite) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.h.ServeHTTP(rec, r)
	return rec
}

func mustContain(t *testing.T, body string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("missing %q", w)
		}
	}
}

func TestI18nKeysMatch(t *testing.T) {
	keys := map[string][]string{}
	for _, l := range langs {
		b, err := webFS.ReadFile("web/i18n/" + l + ".json")
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		keys[l] = slices.Sorted(maps.Keys(m))
	}
	if !slices.Equal(keys["en"], keys["ru"]) {
		t.Errorf("en and ru keys differ:\nen %v\nru %v", keys["en"], keys["ru"])
	}
}

func TestHome(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(4)) // 5 posts, 2 per page
	rec := s.get("/")
	body := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("home = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	mustContain(t, body, `<html lang="en">`, "Test &lt;Blog&gt;", `href="/posts/post-d"`, "Post D", "Post C",
		`<time datetime="2026-11-04">November 4, 2026</time>`, "3 min read", "Page 1 of 3", `href="/?page=2"`,
		`<svg class="logo"`, `name="back" value="/"`, `popovertarget="lang-menu"`, `aria-current="true">English`)
	if strings.Contains(body, "Post B") || strings.Contains(body, "github") {
		t.Error("page 1 shows too much")
	}

	body = s.get("/?page=3").Body.String()
	mustContain(t, body, "&lt;script&gt;", "&#34;&gt;&lt;img src=x", `rel="prev" href="/?page=2"`, "Page 3 of 3")
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
		t.Error("frontmatter not escaped")
	}
	mustContain(t, s.get("/?page=2").Body.String(), `rel="prev" href="/"`)
	for _, bad := range []string{"/?page=1", "/?page=4", "/?page=0", "/?page=abc", "/?page=-1"} {
		if got := s.get(bad).Code; got != 404 {
			t.Errorf("%s = %d, want 404", bad, got)
		}
	}
}

func TestHomeEmptyAndNotReady(t *testing.T) {
	mustContain(t, newSite(t, testConfig(), content.NewIndex(nil, slog.New(slog.DiscardHandler))).get("/").Body.String(), "No posts yet.")
	s := newSite(t, testConfig(), nil)
	for _, p := range []string{"/", "/posts/x"} {
		rec := s.get(p)
		if rec.Code != 503 || rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s not ready = %d", p, rec.Code)
		}
		mustContain(t, rec.Body.String(), "Starting up")
	}
}

func TestPost(t *testing.T) {
	cfg := testConfig()
	cfg.GitHubURL = "https://github.com/example"
	ix := testIndex(1)
	ix.Logo = true
	s := newSite(t, cfg, ix)
	rec := s.get("/posts/xss")
	if rec.Code != 200 {
		t.Fatalf("post = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, `<html lang="ru">`, `<header class="topbar" lang="en">`, "<p>Привет</p>",
		`src="/assets/covers/a%20b%23c.webp"`, "Updated November 5, 2026", "1 min read",
		`<img class="logo" src="/assets/logo.svg"`, `href="https://github.com/example"`, `name="back" value="/posts/xss"`)
	if strings.Contains(body, "<script>alert") {
		t.Error("title not escaped")
	}
	if rec := s.get("/posts/nope"); rec.Code != 404 || !strings.Contains(rec.Body.String(), "Page not found") {
		t.Errorf("missing post = %d", rec.Code)
	}
}

func TestCookies(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(1))
	body := s.get("/", &http.Cookie{Name: "lang", Value: "ru"}, &http.Cookie{Name: "theme", Value: "dark"}).Body.String()
	mustContain(t, body, `<html lang="ru" data-theme="dark">`, `content="dark"`, "Новые", "1 ноября 2026", "3 мин чтения", `aria-label="Язык: ru"`, `aria-current="true">Русский`)

	body = s.get("/", &http.Cookie{Name: "lang", Value: "de"}, &http.Cookie{Name: "theme", Value: `"><script>`}).Body.String()
	mustContain(t, body, `<html lang="en">`, `content="light dark"`, "Newest")

	cfg := testConfig()
	cfg.DefaultLang = "ru"
	mustContain(t, newSite(t, cfg, testIndex(1)).get("/nope").Body.String(), "Страница не найдена")
}

func TestPrefs(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(1))
	post := func(form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/prefs", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		s.h.ServeHTTP(rec, r)
		return rec
	}
	rec := post(url.Values{"theme": {"dark"}, "back": {"/posts/xss?x=1"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/posts/xss?x=1" {
		t.Fatalf("prefs = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	c := rec.Header().Get("Set-Cookie")
	for _, want := range []string{"theme=dark", "Path=/", "Max-Age=31536000", "HttpOnly", "Secure", "SameSite=Lax"} {
		if !strings.Contains(c, want) {
			t.Errorf("cookie %q missing %s", c, want)
		}
	}
	if rec := post(url.Values{"lang": {"ru"}}); rec.Header().Get("Location") != "/" || !strings.Contains(rec.Header().Get("Set-Cookie"), "lang=ru") {
		t.Errorf("lang prefs = %v", rec.Header())
	}
	for _, bad := range []url.Values{{"theme": {"blue"}}, {"lang": {"de"}}, {"lang": {"ru; Path=/x"}}} {
		if rec := post(bad); rec.Code != 400 || rec.Header().Get("Set-Cookie") != "" {
			t.Errorf("%v = %d", bad, rec.Code)
		}
	}
	if rec := post(url.Values{"back": {strings.Repeat("a", 5000)}}); rec.Code != 400 {
		t.Errorf("oversized body = %d", rec.Code)
	}
	cfg := testConfig()
	cfg.SiteURL, _ = url.Parse("http://localhost:8080")
	s = newSite(t, cfg, testIndex(1))
	if c := post(url.Values{"theme": {"light"}}).Header().Get("Set-Cookie"); strings.Contains(c, "Secure") {
		t.Errorf("Secure cookie on an http site: %s", c)
	}
}

func TestLocalPath(t *testing.T) {
	tests := map[string]string{
		"/":                    "/",
		"/posts/a?page=2":      "/posts/a?page=2",
		"":                     "/",
		"posts/a":              "/",
		"//evil.example":       "/",
		`/\evil.example`:       "/",
		"https://evil.example": "/",
		"/a\r\nSet-Cookie: x":  "/",
		"/%0d%0a":              "/%0d%0a",
		"javascript:alert(1)":  "/",
	}
	for in, want := range tests {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStatic(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(1))
	home := s.get("/").Body.String()
	i := strings.Index(home, "/static/style.css?v=")
	if i < 0 {
		t.Fatal("no stylesheet link")
	}
	rec := s.get("/static/style.css")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("css = %d %v", rec.Code, rec.Header())
	}
	css := rec.Body.String()
	mustContain(t, css, `:root[data-theme="dark"] .chroma .k {`, `:root:not([data-theme="light"]) .chroma .k {`,
		`:root[data-theme="light"] .chroma .k {`, "@font-face")
	if strings.Contains(css, ".bg {") {
		t.Error("chroma .bg rule not dropped")
	}
	if rec := s.get("/static/fonts/inter-latin-wght-normal.woff2"); rec.Code != 200 || rec.Body.Len() < 10000 {
		t.Errorf("font = %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if got := s.get("/static/fonts/../../pages.go").Code; got == 200 {
		t.Error("path traversal served a file")
	}
	for _, p := range []string{"/static/fonts/", "/static/fonts/nope.woff2", "/static/fonts/..%2fstyle.css", "/static/"} {
		if got := s.get(p).Code; got != 404 {
			t.Errorf("%s = %d, want 404", p, got)
		}
	}
}

func TestFavicon(t *testing.T) {
	ix := testIndex(1)
	s := newSite(t, testConfig(), ix)
	mustContain(t, s.get("/").Body.String(), `<link rel="icon" type="image/svg+xml" href="/favicon.ico">`)
	for _, idx := range []*content.Index{ix, nil} { // nil: still starting, no index yet
		s.idx.Store(idx)
		rec := s.get("/favicon.ico")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(rec.Body.String(), "<svg") {
			t.Errorf("built-in favicon = %d %q", rec.Code, rec.Header().Get("Content-Type"))
		}
	}

	ix.Logo = true
	s.idx.Store(ix)
	mustContain(t, s.get("/").Body.String(), `<link rel="icon" type="image/svg+xml" href="/assets/logo.svg">`)
	if rec := s.get("/favicon.ico"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/assets/logo.svg" {
		t.Errorf("favicon with logo = %d %q", rec.Code, rec.Header().Get("Location"))
	}
}
