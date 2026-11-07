package server

import (
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Mimic890/hyperhack/internal/content"
)

func postSite(t *testing.T) *testSite {
	t.Helper()
	r := content.NewRenderer("en")
	var posts []*content.Post
	for key, src := range map[string]string{
		"full.md":   "---\ntitle: Full <b>post</b>\ndate: 2026-11-03\nupdated: 2026-11-05\ntags: [go]\ncover: assets/c.png\n---\n## One & two\n\n### Sub\n\n#### Deep\n\n> [!tip] Hint\n> Body\n\n```go\nfunc f() {}\n```\n",
		"plain.md":  "---\ntitle: Plain\ndate: 2026-11-02\nlang: ru\n---\nNo headings.\n\n> [!note]\n> Текст\n",
		"oldest.md": "---\ntitle: Oldest <i>one</i>\ndate: 2026-11-01\n---\nText.\n",
		"draft.md":  "---\ntitle: Draft\ndate: 2026-10-01\ndraft: true\n---\nHidden.\n",
	} {
		p, err := r.Parse(key, []byte(src))
		if err != nil {
			t.Fatal(key, err)
		}
		posts = append(posts, p)
	}
	cfg := testConfig()
	cfg.RSSLimit = 10
	return newSite(t, cfg, content.NewIndex(posts, slog.New(slog.DiscardHandler)))
}

func TestPostLayout(t *testing.T) {
	s := postSite(t)
	inOrder := func(body string, want ...string) {
		t.Helper()
		at := 0
		for _, w := range want {
			i := strings.Index(body[at:], w)
			if i < 0 {
				t.Fatalf("missing %q after byte %d", w, at)
			}
			at += i + len(w)
		}
	}

	t.Run("full post", func(t *testing.T) {
		body := s.get("/posts/full").Body.String()
		inOrder(body,
			`class="brand"`, `<span class="bar-title" aria-hidden="true">Full &lt;b&gt;post&lt;/b&gt;</span>`, `<nav class="tools"`,
			`<div class="banner"><img class="cover" src="/assets/c.png" alt="">`, `<main id="main">`,
			`<header class="post-head">`, `class="tag"`, `<h1>Full &lt;b&gt;post&lt;/b&gt;</h1>`,
			`datetime="2026-11-03"`, "Updated November 5, 2026", "1 min read", "Views: 0", `</header>`,
			`<details class="toc toc-details" lang="en">`, `<summary>Contents</summary>`, `</details>`,
			`<div class="prose">`, `<h2 id="one-two">`,
			`<blockquote class="callout callout-tip"><p class="callout-title">Hint</p>`,
			`<div class="code-block"><div class="code-head" data-lang="go"></div>`,
			`Was this post helpful?`, `data-vote="like"`, `data-vote="dislike"`, `role="status"`,
			`<button type="button" class="vote share" aria-label="Share" title="Share" hidden><svg`, `</footer>`,
			`<a class="post-card" href="/posts/plain"><span class="label">Previous post</span><strong lang="ru">Plain</strong></a>`,
			`<a class="post-card" href="/"><strong>All posts</strong></a>`,
			`<nav class="toc toc-side" lang="en" aria-label="Contents">`,
		)
		const items = `<ul><li class="l2"><a href="#one-two">One &amp; two</a></li><li class="l3"><a href="#sub">Sub</a></li><li class="l4"><a href="#deep">Deep</a></li></ul>`
		if n := strings.Count(body, items); n != 2 {
			t.Errorf("contents appear %d times, want 2 (details and column)\n%s", n, body)
		}
		mustContain(t, body, `data-copy="Copy"`, `data-copied="Copied"`, `data-link-copied="Link copied"`)
		// The Copy buttons are added by the script.
		if strings.Contains(body, `class="copy"`) {
			t.Error("Copy button in server HTML")
		}
	})

	t.Run("no cover, no headings", func(t *testing.T) {
		body := s.get("/posts/plain").Body.String()
		for _, no := range []string{`class="banner"`, `class="toc`} {
			if strings.Contains(body, no) {
				t.Errorf("found %q", no)
			}
		}
		// The next older post, not the draft.
		mustContain(t, body, `href="/posts/oldest"`, `<p class="callout-title">Заметка</p>`, `<html lang="ru">`)
	})

	t.Run("oldest post", func(t *testing.T) {
		body := s.get("/posts/oldest").Body.String()
		if strings.Contains(body, "Previous post") {
			t.Error("previous post card on the oldest post")
		}
		mustContain(t, body, `<strong>All posts</strong>`)
	})

	t.Run("Russian UI", func(t *testing.T) {
		body := s.get("/posts/full", &http.Cookie{Name: "lang", Value: "ru"}).Body.String()
		mustContain(t, body, "Пост был полезен?", `aria-label="Поделиться"`, `data-link-copied="Ссылка скопирована"`,
			`data-copy="Копировать"`, `data-copied="Скопировано"`, "<summary>Содержание</summary>", "Предыдущий пост")
	})

	t.Run("no top bar title elsewhere", func(t *testing.T) {
		if strings.Contains(s.get("/").Body.String(), "bar-title") {
			t.Error("bar title on the main page")
		}
	})
}

// The feed carries the same post markup, and the Copy button stays out of it.
func TestPostMarkupInFeed(t *testing.T) {
	s := postSite(t)
	body := s.get("/rss.xml").Body.String()
	mustContain(t, body, "callout-title", "code-block")
	if strings.Contains(body, "copy") {
		t.Error("Copy button in the feed")
	}
}

func TestMotionMarkup(t *testing.T) {
	s := postSite(t)
	for _, path := range []string{"/", "/posts/full", "/search", "/tags/go", "/nope"} {
		body := s.get(path).Body.String()
		// Blocking and before the body: pagereveal fires before the first frame.
		script := regexp.MustCompile(`<script src="/static/transition\.js\?v=[^"]+"></script>`)
		if loc := script.FindStringIndex(body); loc == nil || loc[0] > strings.Index(body, "</head>") {
			t.Errorf("%s: no blocking transition script in <head>", path)
		}
		mustContain(t, body, `<link rel="expect" href="#main" blocking="render">`, `<main id="main">`)
		if got := strings.Contains(body, `class="progress"`); got != (path == "/posts/full") {
			t.Errorf("%s: reading progress = %v", path, got)
		}
	}
}

func TestDiagramScripts(t *testing.T) {
	r := content.NewRenderer("en")
	var posts []*content.Post
	for key, src := range map[string]string{
		"chart.md": "---\ntitle: Chart\ndate: 2026-11-03\n---\n```mermaid\nflowchart LR\n  A --> B\n```\n",
		"math.md":  "---\ntitle: Math\ndate: 2026-11-02\n---\nArea $\\pi r^2$.\n\n```go\nx := 1\n```\n",
	} {
		p, err := r.Parse(key, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		posts = append(posts, p)
	}
	s := newSite(t, testConfig(), content.NewIndex(posts, slog.New(slog.DiscardHandler)))
	for path, want := range map[string]bool{"/posts/chart": true, "/posts/math": false} {
		body := s.get(path).Body.String()
		for _, js := range []string{"/static/pele.js?v=", "/static/diagram.js?v="} {
			if strings.Contains(body, js) != want {
				t.Errorf("%s: %s loaded = %v, want %v", path, js, !want, want)
			}
		}
	}
	if !strings.Contains(s.get("/posts/math").Body.String(), "<math ") {
		t.Error("math post has no MathML")
	}
	if w := s.get("/static/pele.js"); w.Code != 200 || !strings.HasPrefix(w.Body.String(), "/*! Pele 1.13.0") {
		t.Errorf("pele.js: %d", w.Code)
	}
}
