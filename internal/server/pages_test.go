package server

import (
	"context"
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
		AltchaExpires: 5 * time.Minute, AltchaMaxNumber: 1000, PopularDays: 30,
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
	// Only Russian has a "few" plural form.
	keys["ru"] = slices.DeleteFunc(keys["ru"], func(k string) bool { return strings.HasSuffix(k, "_few") })
	if !slices.Equal(keys["en"], keys["ru"]) {
		t.Errorf("en and ru keys differ:\nen %v\nru %v", keys["en"], keys["ru"])
	}
}

func TestHome(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(4)) // 5 posts: one featured, the rest 2 per page
	rec := s.get("/")
	body := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("home = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	mustContain(t, body, `<html lang="en">`, "Test &lt;Blog&gt;", `<h1 class="sr">Test &lt;Blog&gt;</h1>`,
		`<article class="featured">`, `<h2><a href="/posts/post-d">Post D</a></h2>`, `<time datetime="2026-11-04">November 4, 2026</time>`,
		`<h3><a href="/posts/post-c">Post C</a></h3>`, `<time datetime="2026-11-03">November 3</time>`, `<h2 class="year">2026 <span class="n">4 posts</span></h2>`,
		"3 min read", "Page 1 of 2", `href="/?page=2"`,
		`<svg class="logo"`, `name="back" value="/"`, `popovertarget="lang-menu"`, `aria-current="true">English`)
	for _, no := range []string{"Post A", "github", `id="search"`, `<h3><a href="/posts/post-d"`} {
		if strings.Contains(body, no) {
			t.Errorf("page 1 has %q", no)
		}
	}

	body = s.get("/?page=2").Body.String()
	mustContain(t, body, "&lt;script&gt;", "&#34;&gt;&lt;img src=x", `rel="prev" href="/"`, "Page 2 of 2", `<h3><a href="/posts/post-a">`, `<h2 class="year">2026 <span class="n">4 posts</span></h2>`)
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<img src=x") {
		t.Error("frontmatter not escaped")
	}
	for _, no := range []string{`class="featured"`, `class="top-tags"`, "post-d"} {
		if strings.Contains(body, no) {
			t.Errorf("page 2 has %q", no)
		}
	}
	for _, bad := range []string{"/?page=1", "/?page=3", "/?page=0", "/?page=abc", "/?page=-1"} {
		if got := s.get(bad).Code; got != 404 {
			t.Errorf("%s = %d, want 404", bad, got)
		}
	}
}

// homeSite is a site whose main page has these blocks, parsed from home.md.
func homeSite(t *testing.T, blocks string, ix *content.Index) *testSite {
	t.Helper()
	parsed, problems := content.ParseHome([]byte("---\nblocks:\n" + blocks + "---\n"))
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	ix.Home = parsed
	return newSite(t, testConfig(), ix)
}

func TestHomeBlocks(t *testing.T) {
	tagged := func() *content.Index {
		var posts []*content.Post
		for i, y := range []int{2026, 2026, 2025} {
			posts = append(posts, &content.Post{
				Key: string(rune('a'+i)) + ".md", Slug: "p" + string(rune('a'+i)), Title: "P" + string(rune('A'+i)), Lang: "en", Words: 10,
				Date: time.Date(y, 3, 1+i, 0, 0, 0, 0, time.UTC), Tags: []string{"go", "t" + string(rune('a'+i))}, Cover: "assets/c.png", CoverW: 800, CoverH: 600,
			})
		}
		return content.NewIndex(posts, slog.New(slog.DiscardHandler))
	}
	inOrder := func(t *testing.T, body string, want ...string) {
		t.Helper()
		at := 0
		for _, w := range want {
			i := strings.Index(body[at:], w)
			if i < 0 {
				t.Fatalf("missing %q after byte %d", w, at)
			}
			at += i
		}
	}

	t.Run("order and intro", func(t *testing.T) {
		s := homeSite(t, "  - {type: tags, limit: 2}\n  - {type: intro, title: {ru: Привет, en: <b>Hi</b>}, text: Sub}\n  - {type: intro, title: Two}\n  - {type: featured}\n", tagged())
		body := s.get("/").Body.String()
		inOrder(t, body, `<nav class="top-tags"`, "<h1>&lt;b&gt;Hi&lt;/b&gt;</h1>", "<p>Sub</p>", "<h2>Two</h2>", `<article class="featured">`)
		if strings.Contains(body, `<h1 class="sr"`) {
			t.Error("a second h1 for the site name")
		}
		// No posts block: no list, no page 2.
		if strings.Contains(body, `class="post-list"`) || s.get("/?page=2").Code != 404 {
			t.Error("posts without a posts block")
		}
		mustContain(t, s.get("/", &http.Cookie{Name: "lang", Value: "ru"}).Body.String(), "<h1>Привет</h1>", "Новый пост")
	})

	t.Run("tags", func(t *testing.T) {
		body := homeSite(t, "  - {type: tags, limit: 2}\n", tagged()).get("/").Body.String()
		inOrder(t, body, `href="/" aria-current="page">All posts<span class="n">3</span>`, `href="/tags/go"><span>#</span>go<span class="n">3</span>`, `href="/tags/ta"><span>#</span>ta<span class="n">1</span>`)
		if strings.Contains(body, "/tags/tb") {
			t.Error("limit ignored")
		}
		if b := newSite(t, testConfig(), testIndex(1)).get("/").Body.String(); strings.Contains(b, `class="top-tags"`) {
			t.Error("tags block without tags")
		}
	})

	t.Run("featured cover", func(t *testing.T) {
		body := homeSite(t, "  - {type: featured}\n", tagged()).get("/").Body.String()
		mustContain(t, body, `<img src="/assets/c.png" alt="" width="800" height="600">`, "New post")
	})

	t.Run("posts by year and limit", func(t *testing.T) {
		s := homeSite(t, "  - {type: posts, group: year, limit: 1}\n", tagged())
		inOrder(t, s.get("/").Body.String(), `<h2 class="year">2026 <span class="n">2 posts</span></h2>`, `href="/posts/pb"`, "Page 1 of 3")
		inOrder(t, s.get("/?page=3").Body.String(), `<h2 class="year">2025 <span class="n">1 post</span></h2>`, `href="/posts/pc"`)
		// Without featured the newest post stays in the list. Without grouping there is a full date.
		s = homeSite(t, "  - {type: posts}\n", tagged())
		body := s.get("/").Body.String()
		mustContain(t, body, `<h3><a href="/posts/pb"`, "March 2, 2026", `<h2 class="sr">Newest</h2>`)
		if strings.Contains(body, `class="year"`) {
			t.Error("year heading without group: year")
		}
	})

	t.Run("side column", func(t *testing.T) {
		s := homeSite(t, "  - {type: featured, area: side}\n  - {type: posts}\n", tagged())
		body := s.get("/").Body.String()
		inOrder(t, body, `class="home has-side"`, `class="post-list"`, `<aside class="home-side">`, `class="featured"`)
		if b := s.get("/?page=2"); b.Code == 200 && strings.Contains(b.Body.String(), "<aside") {
			t.Error("side column on page 2")
		}
		if b := homeSite(t, "  - {type: posts}\n", tagged()).get("/").Body.String(); strings.Contains(b, "has-side") || strings.Contains(b, "<aside") {
			t.Error("side column without side blocks")
		}
	})

	t.Run("popular limit and views", func(t *testing.T) {
		s := homeSite(t, "  - {type: popular, limit: 1}\n", tagged())
		if strings.Contains(s.get("/").Body.String(), `class="popular"`) {
			t.Error("popular without views")
		}
		day := store.Day(time.Now())
		_ = s.d.Store.Add(context.Background(), day, store.Inc{Slug: "pa", Metric: store.View, N: 7}, store.Inc{Slug: "pb", Metric: store.View, N: 2}, store.Inc{Slug: "gone", Metric: store.View, N: 99})
		for i, v := range []int{1, 1, -1} {
			_, _ = s.d.Store.SetVote(context.Background(), []byte{byte(i)}, "pa", v)
		}
		body := s.get("/").Body.String()
		inOrder(t, body, `<section class="popular"`, `<li><a href="/posts/pa">PA</a><p class="meta">`,
			`<span class="sr">Views</span>7</span>`, `<span class="sr">Like</span>2</span>`, `<span class="sr">Dislike</span>1</span>`)
		if strings.Contains(body[strings.Index(body, `<section class="popular"`):], `href="/rss.xml"`) {
			t.Error("popular block repeats the RSS link of the top bar")
		}
		if strings.Contains(body, `href="/posts/pb"`) || strings.Contains(body, "gone") {
			t.Error("popular limit ignored or a removed post shown")
		}
	})
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
	ix.Logo = content.Logo{}.Or("assets/logo.svg")
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

func TestTopBarSearch(t *testing.T) {
	cfg := testConfig()
	cfg.GitHubURL = "https://github.com/example"
	s := newSite(t, cfg, testIndex(3))
	order := func(body string, want ...string) {
		t.Helper()
		at := 0
		for _, w := range want {
			i := strings.Index(body[at:], w)
			if i < 0 {
				t.Fatalf("missing %q after byte %d", w, at)
			}
			at += i
		}
	}
	home := s.get("/").Body.String()
	order(home, `<a class="search-field" href="/search"`, "Search posts", "Ctrl K", `name="theme"`, `href="/rss.xml"`, `href="https://github.com/example"`, `popovertarget="lang-menu"`)
	if page2 := s.get("/?page=2").Body.String(); !strings.Contains(page2, `class="search-field"`) {
		t.Error("page 2 of the main page has no search field")
	}
	// Other pages get the icon only, and the link works without JS.
	for _, path := range []string{"/posts/post-a", "/search", "/nope"} {
		body := s.get(path).Body.String()
		if strings.Contains(body, "search-field") || strings.Contains(body, "Ctrl K") {
			t.Errorf("%s has the search field", path)
		}
		order(body, `<a class="icon" href="/search"`, `name="theme"`)
	}
}

func TestCookies(t *testing.T) {
	s := newSite(t, testConfig(), testIndex(1))
	body := s.get("/", &http.Cookie{Name: "lang", Value: "ru"}, &http.Cookie{Name: "theme", Value: "dark"}).Body.String()
	mustContain(t, body, `<html lang="ru" data-theme="dark">`, `content="dark"`, "Новый пост", "1 ноября 2026", "3 мин чтения", `aria-label="Язык: ru"`, `aria-current="true">Русский`)

	body = s.get("/", &http.Cookie{Name: "lang", Value: "de"}, &http.Cookie{Name: "theme", Value: `"><script>`}).Body.String()
	mustContain(t, body, `<html lang="en">`, `content="light dark"`, "New post")

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
	if rec := s.get("/static/fonts/onest-latin-wght-normal.woff2"); rec.Code != 200 || rec.Body.Len() < 10000 {
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
	mustContain(t, s.get("/").Body.String(), `<link rel="icon" href="/favicon.ico">`)
	for _, idx := range []*content.Index{ix, nil} { // nil: still starting, no index yet
		s.idx.Store(idx)
		rec := s.get("/favicon.ico")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(rec.Body.String(), "<svg") {
			t.Errorf("built-in favicon = %d %q", rec.Code, rec.Header().Get("Content-Type"))
		}
	}

	ix.Logo = content.Logo{}.Or("assets/logo.svg")
	s.idx.Store(ix)
	mustContain(t, s.get("/").Body.String(), `<link rel="icon" href="/assets/logo.svg">`)
	if rec := s.get("/favicon.ico"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/assets/logo.svg" {
		t.Errorf("favicon with logo = %d %q", rec.Code, rec.Header().Get("Location"))
	}

	ix.Logo = content.Logo{Light: "assets/brand/day.svg", Dark: "assets/brand/night logo.png", Favicon: "assets/brand/icon.png"}
	body := s.get("/").Body.String()
	mustContain(t, body, `<link rel="icon" href="/assets/brand/icon.png">`,
		`<img class="logo logo-light" src="/assets/brand/day.svg"`, `<img class="logo logo-dark" src="/assets/brand/night%20logo.png"`)
	if strings.Contains(body, `<svg class="logo"`) {
		t.Error("built-in mark next to bucket logos")
	}
	if rec := s.get("/favicon.ico"); rec.Header().Get("Location") != "/assets/brand/icon.png" {
		t.Errorf("favicon = %q", rec.Header().Get("Location"))
	}
}

func TestPluralForm(t *testing.T) {
	ru := map[string]string{"posts_few": ""}
	for n, want := range map[int]string{0: "many", 1: "one", 2: "few", 4: "few", 5: "many", 11: "many", 12: "many", 14: "many", 21: "one", 22: "few", 25: "many", 101: "one", 111: "many", 112: "many", 122: "few"} {
		if got := pluralForm(ru, "posts", n); got != want {
			t.Errorf("ru %d: %s, want %s", n, got, want)
		}
	}
	for n, want := range map[int]string{0: "many", 1: "one", 2: "many", 21: "many"} {
		if got := pluralForm(map[string]string{}, "posts", n); got != want {
			t.Errorf("en %d: %s, want %s", n, got, want)
		}
	}
}
