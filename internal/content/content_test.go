package content

import (
	"io"
	"log/slog"
	"net/url"
	"slices"
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
	if p.Words != 2 {
		t.Errorf("words = %d, want 2", p.Words)
	}
	if !strings.Contains(string(p.HTML), "<strong>world</strong>") {
		t.Errorf("html = %s", p.HTML)
	}
}

func TestParseDefaults(t *testing.T) {
	p, err := NewRenderer("ru").Parse("Мой Первый Пост!.md", []byte("---\ntitle: T\ndate: 2026-11-03\ntags: [Go, go, Заметки, dev/go-1_2]\ndraft: true\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "moy-pervyy-post" || p.Lang != "ru" || !p.Draft || strings.Join(p.Tags, ",") != "go,заметки,dev/go-1_2" || len(p.Source) == 0 {
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
		"tag space":      "---\ntitle: T\ndate: 2026-11-03\ntags: [a b]\n---\n",
		"tag html":       "---\ntitle: T\ndate: 2026-11-03\ntags: ['<x>']\n---\n",
		"tag long":       "---\ntitle: T\ndate: 2026-11-03\ntags: [" + strings.Repeat("x", 51) + "]\n---\n",
		"too many tags":  "---\ntitle: T\ndate: 2026-11-03\ntags: [a,b,c,d,e,f,g,h,i,j,k,l,m,n,o,p,q,r,s,t,u]\n---\n",
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
	r.ImageSize = func(key string) (int, int) {
		if key == "assets/a b.png" {
			return 640, 360
		}
		return 0, 0
	}
	src := "# Привет мир\n\n## Привет мир\n\n```go\nfunc main() {}\n```\n\n" +
		"[ext](https://example.com) [int](/posts/x) [file](assets/doc.png) <https://example.org>\n\n" +
		"![alt](assets/a%20b.png)\n"
	got, err := r.render([]byte(src), "en")
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
		`<img src="/assets/a%20b.png" alt="alt" width="640" height="360" loading="lazy" decoding="async">`,
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
	"```\"><script>alert(1)</script>\ncode\n```",
	"```go\" onclick=\"alert(1)\ncode\n```",
	"```<img/src=x/onerror=alert(1)>\ncode\n```",
	"> [!note] <script>alert(1)</script>\n> <img src=x onerror=alert(1)>",
	"> [!tip] [x](javascript:alert(1))",
	"> [!warning] ![x](javascript:alert(1))\n> body",
	"> [!note\" onclick=\"alert(1)] x",
	"## <img src=x onerror=alert(1)>",
	`<!--><script>alert(1)</script>-->`,
	`<a href="https://example.com" target="_blank" onclick="alert(1)">x</a>`,
	`<details open ontoggle=alert(1)>`,
}

func TestRenderXSS(t *testing.T) {
	r := NewRenderer("en")
	for _, payload := range xssCorpus {
		got, err := r.render([]byte(payload), "en")
		if err != nil {
			t.Fatal(err)
		}
		if bad := unsafeHTML(string(got)); bad != "" {
			t.Errorf("payload %q: %s\n%s", payload, bad, got)
		}
	}
}

func TestCallouts(t *testing.T) {
	r := NewRenderer("en")
	for _, c := range []struct{ name, lang, src, want string }{
		{"default title", "en", "> [!note]\n> Body", `<blockquote class="callout callout-note"><p class="callout-title">Note</p><p>Body</p></blockquote>`},
		{"russian title", "ru", "> [!tip]\n> Текст", `<blockquote class="callout callout-tip"><p class="callout-title">Совет</p><p>Текст</p></blockquote>`},
		{"russian warning", "ru", "> [!warning]", `<blockquote class="callout callout-warning"><p class="callout-title">Внимание</p></blockquote>`},
		{"other language", "de", "> [!note]", `<p class="callout-title">Note</p>`},
		{"case and title", "en", "> [!WaRnInG] Look out\n> Body", `<blockquote class="callout callout-warning"><p class="callout-title">Look out</p><p>Body</p></blockquote>`},
		{"inline title", "en", "> [!note] A **b** `c`\n> Body", `<p class="callout-title">A <strong>b</strong> <code>c</code></p><p>Body</p>`},
		{"markup over two lines loses nothing", "en", "> [!note] **a\n> b** c\n> d", `<p class="callout-title"><strong>a`},
		{"hard break", "en", "> [!note] Title  \n> Body", `<p class="callout-title">Title</p><p>Body</p>`},
		{"own paragraph", "en", "> [!tip]\n>\n> Body\n>\n> More", `<p class="callout-title">Tip</p><p>Body</p><p>More</p>`},
		{"nested", "en", "> [!note]\n> > [!tip]\n> > x", `<blockquote class="callout callout-tip"><p class="callout-title">Tip</p><p>x</p></blockquote></blockquote>`},
		{"in a list", "en", "- item\n\n  > [!note]\n  > x", `<blockquote class="callout callout-note">`},
	} {
		got, err := r.render([]byte(c.src), c.lang)
		if err != nil {
			t.Fatal(err)
		}
		if s := strings.ReplaceAll(string(got), "\n", ""); !strings.Contains(s, c.want) {
			t.Errorf("%s: want %s in\n%s", c.name, c.want, got)
		}
	}
	for name, src := range map[string]string{
		"other type":    "> [!danger] x",
		"no space":      "> [!note]x",
		"not first":     "> a\n> [!note] x",
		"plain":         "> quote",
		"reference":     "> [!note]\n\n[!note]: /x",
		"link":          "> [!note](/x)",
		"not a quote":   "[!note] x",
		"foldable mark": "> [!note]- x",
	} {
		got, err := r.render([]byte(src), "en")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(got), "callout") {
			t.Errorf("%s: %s", name, got)
		}
	}
	// Nothing is lost when the markup crosses the line.
	got, _ := r.render([]byte("> [!note] **a\n> b** c\n> d"), "en")
	if text := plainText(string(got)); text != "a b c d" {
		t.Errorf("text = %q", text)
	}
}

func TestCodeBlockHeader(t *testing.T) {
	r := NewRenderer("en")
	for _, c := range []struct{ name, src, want string }{
		{"highlighted", "```go\nfunc f() {}\n```", `<div class="code-block"><div class="code-head" data-lang="go"></div><pre class="chroma"><code><span class="line">`},
		{"unknown language", "```nope\n<x>\n```", `<div class="code-block"><div class="code-head" data-lang="nope"></div><pre><code>&lt;x&gt;` + "\n" + `</code></pre></div>`},
		{"no language", "```\nx\n```", `<div class="code-block"><div class="code-head"></div><pre><code>x` + "\n" + `</code></pre></div>`},
		{"indented", "    x\n", `<div class="code-block"><div class="code-head"></div><pre><code>x` + "\n" + `</code></pre></div>`},
		{"odd language dropped", "```a\"b\nx\n```", `<div class="code-head"></div>`},
		{"c++", "```c++\nint x;\n```", `data-lang="c++"`},
	} {
		got, err := r.render([]byte(c.src), "en")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), c.want) {
			t.Errorf("%s: want %s in\n%s", c.name, c.want, got)
		}
	}
	// The language label is not post text.
	got, _ := r.render([]byte("```python\nx = 1\n```"), "en")
	if text := plainText(string(got)); strings.Contains(text, "python") {
		t.Errorf("text = %q", text)
	}
}

func TestTableOfContents(t *testing.T) {
	r := NewRenderer("en")
	got, err := r.render([]byte("# Top\n\n## Первый & *второй*\n\ntext\n\n### Sub\n\n#### Deep\n\n##### Too deep\n\n## ![only image](assets/a.png)\n\n> ## In a quote\n"), "en")
	if err != nil {
		t.Fatal(err)
	}
	want := []TOCItem{
		{2, "pervyy-vtoroy", "Первый & второй"},
		{3, "sub", "Sub"},
		{4, "deep", "Deep"},
		{2, "in-a-quote", "In a quote"},
	}
	if toc := tableOfContents(string(got)); !slices.Equal(toc, want) {
		t.Errorf("toc = %+v\nwant %+v\n%s", toc, want, got)
	}
	if toc := tableOfContents("<p>no headings</p>"); toc != nil {
		t.Errorf("toc = %+v", toc)
	}
}

func TestOlder(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	a, b, draft := &Post{Key: "a.md", Slug: "a", Date: d(1)}, &Post{Key: "b.md", Slug: "b", Date: d(2)}, &Post{Key: "c.md", Slug: "c", Date: d(3), Draft: true}
	ix := NewIndex([]*Post{a, b, draft}, slog.New(slog.DiscardHandler))
	if got := ix.Older(b); got != a {
		t.Errorf("Older(b) = %+v", got)
	}
	if got := ix.Older(a); got != nil {
		t.Errorf("Older(a) = %+v", got)
	}
	if got := ix.Older(draft); got != nil {
		t.Errorf("Older(draft) = %+v", got)
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
	f.Add([]byte("---\ntitle: T\ndate: 2026-11-03\n---\n> [!tip] **a\n> b** c  \n> d\n\n[!note]: /x\n\n> [!note]\n\n    code\n\n## h & i\n"))
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
		for _, item := range p.TOC {
			if item.Level < 2 || item.Level > 4 || item.ID == "" || item.Title == "" {
				t.Fatalf("bad contents item: %+v", item)
			}
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
