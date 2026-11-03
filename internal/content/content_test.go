package content

import (
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func TestParse(t *testing.T) {
	r := NewRenderer("en")
	src := "\ufeff---\r\ntitle: Leaving Ghost\r\ndescription: Why\r\ndate: 2026-11-03\r\nupdated: 2026-11-05T20:59\r\ntags:\r\n  - go\r\n  - ' selfhost '\r\ncover: assets/covers/ghost.webp\r\nslug: leaving-ghost\r\nlang: ru\r\nextra: ignored\r\n---\r\n\r\nHello **world**\r\n"
	p, err := r.Parse("blog/leaving.md", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := Post{
		Key: "blog/leaving.md", Slug: "leaving-ghost", Title: "Leaving Ghost", Description: "Why",
		Date:    time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		Updated: time.Date(2026, 11, 5, 20, 59, 0, 0, time.UTC),
		Cover:   "assets/covers/ghost.webp", Lang: "ru",
	}
	if p.Key != want.Key || p.Slug != want.Slug || p.Title != want.Title || p.Description != want.Description ||
		!p.Date.Equal(want.Date) || !p.Updated.Equal(want.Updated) || p.Cover != want.Cover || p.Lang != want.Lang || p.Draft {
		t.Errorf("got %+v", p)
	}
	if strings.Join(p.Tags, ",") != "go,selfhost" {
		t.Errorf("tags = %q", p.Tags)
	}
	if !strings.Contains(string(p.HTML), "<strong>world</strong>") {
		t.Errorf("html = %s", p.HTML)
	}
}

func TestParseDefaults(t *testing.T) {
	p, err := NewRenderer("ru").Parse("Мой Первый Пост!.md", []byte("---\ntitle: T\ndate: 2026-11-03\ntags: go\ndraft: true\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "moy-pervyy-post" || p.Lang != "ru" || !p.Draft || len(p.Tags) != 1 || p.Tags[0] != "go" {
		t.Errorf("got %+v", p)
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"no frontmatter": "# Title\n",
		"not closed":     "---\ntitle: T\ndate: 2026-11-03\n",
		"invalid yaml":   "---\ntitle: [\n---\n",
		"no title":       "---\ndate: 2026-11-03\n---\n",
		"no date":        "---\ntitle: T\n---\n",
		"bad date":       "---\ntitle: T\ndate: 03.11.2026\n---\n",
		"bad updated":    "---\ntitle: T\ndate: 2026-11-03\nupdated: soon\n---\n",
		"bad slug":       "---\ntitle: T\ndate: 2026-11-03\nslug: Bad Slug\n---\n",
		"bad lang":       "---\ntitle: T\ndate: 2026-11-03\nlang: english\n---\n",
		"cover outside":  "---\ntitle: T\ndate: 2026-11-03\ncover: ../secret.png\n---\n",
		"cover escape":   "---\ntitle: T\ndate: 2026-11-03\ncover: assets/../x.png\n---\n",
		"cover wikilink": "---\ntitle: T\ndate: 2026-11-03\ncover: \"[[x.png]]\"\n---\n",
		"cover ext":      "---\ntitle: T\ndate: 2026-11-03\ncover: assets/x.html\n---\n",
		"bad draft":      "---\ntitle: T\ndate: 2026-11-03\ndraft: maybe\n---\n",
		"huge":           "---\ntitle: T\n" + strings.Repeat("x: y\n", maxFrontmatter/5+1) + "---\n",
	}
	r := NewRenderer("en")
	for name, src := range tests {
		if _, err := r.Parse("post.md", []byte(src)); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := r.Parse("!!!.md", []byte("---\ntitle: T\ndate: 2026-11-03\n---\n")); err == nil {
		t.Error("empty slug: want error")
	}
}

func TestSlugify(t *testing.T) {
	tests := map[string]string{
		"Hello, World!":   "hello-world",
		"  --Go 1.27--  ": "go-1-27",
		"Съешь же ещё этих булок": "sesh-zhe-eshchyo-etikh-bulok",
		"Подъезд":                 "podezd",
		"日本語":                     "",
		strings.Repeat("ab-", 60): strings.TrimRight(strings.Repeat("ab-", 34)[:100], "-"),
	}
	for in, want := range tests {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRender(t *testing.T) {
	r := NewRenderer("en")
	src := "# Привет мир\n\n## Привет мир\n\n```go\nfunc main() {}\n```\n\n" +
		"[ext](https://example.com) [int](/posts/x) [file](assets/doc.png) <https://example.org>\n\n" +
		"![alt](assets/a%20b.png)\n"
	got, err := r.render([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h1 id="privet-mir">`,
		`<h2 id="privet-mir-1">`,
		`<pre class="chroma"><code><span class="line">`,
		`<span class="kd">func</span>`,
		`<a href="https://example.com" rel="noopener noreferrer">ext</a>`,
		`<a href="/posts/x">int</a>`,
		`<a href="/assets/doc.png">file</a>`,
		`<a href="https://example.org" rel="noopener noreferrer">`,
		`<img src="/assets/a%20b.png" alt="alt">`,
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
}

var xssCorpus = []string{
	`<script>alert(1)</script>`,
	`<img src=x onerror=alert(1)>`,
	`<svg onload=alert(1)>`,
	`<iframe src="https://example.com"></iframe>`,
	`<a href="javascript:alert(1)">x</a>`,
	`[x](javascript:alert(1))`,
	`[x](JaVaScRiPt:alert(1))`,
	`[x](java&#x09;script:alert(1))`,
	`[x](vbscript:msgbox(1))`,
	`[x](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
	`![x](javascript:alert(1))`,
	`![x](data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=)`,
	`![x"onerror="alert(1)](assets/a.png)`,
	`[x](https://example.com "a\" onmouseover=\"alert(1)")`,
	`<div style="background:url(javascript:alert(1))">x</div>`,
	`<style>body{display:none}</style>`,
	`<object data="x"></object><embed src="x">`,
	`<form action="https://example.com"><input name=a></form>`,
	`<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
	`<base href="https://example.com/">`,
	`<meta http-equiv="refresh" content="0;url=https://example.com">`,
	"```\n</code></pre><script>alert(1)</script>\n```",
	"```html\n<script>alert(1)</script>\n```",
	"`<script>alert(1)</script>`",
	`<!--><script>alert(1)</script>-->`,
	`<a href="https://example.com" target="_blank" onclick="alert(1)">x</a>`,
	`<details open ontoggle=alert(1)>`,
}

func TestRenderXSS(t *testing.T) {
	r := NewRenderer("en")
	for _, payload := range xssCorpus {
		got, err := r.render([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if bad := unsafeHTML(string(got)); bad != "" {
			t.Errorf("payload %q: %s\n%s", payload, bad, got)
		}
	}
}

// unsafeHTML returns a reason if s has a tag or attribute that can run code or load content.
func unsafeHTML(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if z.Err() == io.EOF {
				return ""
			}
			return z.Err().Error()
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			switch tok.Data {
			case "script", "style", "iframe", "object", "embed", "form", "input", "svg", "math", "base", "meta", "link":
				return "tag " + tok.Data
			}
			for _, a := range tok.Attr {
				v := strings.ToLower(strings.TrimSpace(a.Val))
				switch {
				case strings.HasPrefix(a.Key, "on"), a.Key == "style", a.Key == "srcdoc", a.Key == "target":
					return "attribute " + a.Key
				case a.Key == "href" || a.Key == "src":
					u, err := url.Parse(v)
					if err != nil || (u.Scheme != "" && u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "mailto") {
						return "url " + a.Val
					}
				}
			}
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte("---\ntitle: T\ndate: 2026-11-03\n---\n# Hi\n"))
	f.Add([]byte("---\ntags: &a [*a]\n---\n"))
	for _, p := range xssCorpus {
		f.Add([]byte("---\ntitle: T\ndate: 2026-11-03\n---\n" + p))
	}
	r := NewRenderer("en")
	f.Fuzz(func(t *testing.T, src []byte) {
		p, err := r.Parse("post.md", src)
		if err != nil {
			return
		}
		if !slugRe.MatchString(p.Slug) || p.Title == "" {
			t.Fatalf("bad post: %+v", p)
		}
		if bad := unsafeHTML(string(p.HTML)); bad != "" {
			t.Fatalf("%s\n%s", bad, p.HTML)
		}
	})
}

func TestIndex(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	posts := []*Post{
		{Key: "z.md", Slug: "dup", Date: d(5)},
		{Key: "a.md", Slug: "dup", Date: d(1)},
		{Key: "b.md", Slug: "new", Date: d(9)},
		{Key: "c.md", Slug: "draft", Date: d(10), Draft: true},
	}
	ix := NewIndex(posts, slog.New(slog.DiscardHandler))
	if got := ix.Get("dup"); got == nil || got.Key != "a.md" {
		t.Errorf("dup = %+v, want a.md", got)
	}
	if ix.Get("draft") != nil {
		t.Error("draft in index")
	}
	if ps := ix.Posts(); len(ps) != 2 || ps[0].Slug != "new" || ps[1].Slug != "dup" {
		t.Errorf("order = %v", ps)
	}
}
