package server

import (
	"encoding/json"
	"encoding/xml"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Mimic890/hyperhack/internal/content"
)

func seoSite(t *testing.T) *testSite {
	t.Helper()
	r := content.NewRenderer("en")
	r.ImageSize = func(string) (int, int) { return 1200, 630 }
	var posts []*content.Post
	for key, src := range map[string]string{
		"go-script-tips.md": "---\ntitle: Go & </script> tips\ndescription: Short \"tips\" </script><b>x</b>\ndate: 2026-11-01\nupdated: 2026-11-03\ntags: [go, dev/tools]\ncover: assets/c.png\n---\nSee [home](/) and ![i](assets/i.png). Bleve search works.\n",
		"zametka.md":        "---\ntitle: Заметка\ndate: 2026-11-02\nlang: ru\ntags: [заметки, go]\n---\nМного постов.\n",
		"d.md":              "---\ntitle: Draft\ndate: 2026-11-04\ndraft: true\n---\nSecret.\n",
	} {
		p, err := r.Parse(key, []byte(src))
		if err != nil {
			t.Fatal(key, err)
		}
		posts = append(posts, p)
	}
	cfg := testConfig()
	cfg.AuthorName, cfg.GitHubURL, cfg.RSSLimit, cfg.PageSize = "Ann", "https://github.com/example", 10, 1
	cfg.DefaultOGImage, cfg.IndexNowKey = "assets/og.png", "key12345"
	return newSite(t, cfg, content.NewIndex(posts, slog.New(slog.DiscardHandler)))
}

func TestPostHead(t *testing.T) {
	s := seoSite(t)
	rec := s.get("/posts/go-script-tips")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body,
		`<link rel="canonical" href="https://example.com/posts/go-script-tips">`,
		`<meta property="og:type" content="article">`,
		`<meta property="og:image" content="https://example.com/assets/c.png">`,
		`<meta name="twitter:card" content="summary_large_image">`,
		`<link rel="alternate" type="application/rss+xml"`,
		`<img class="cover" src="/assets/c.png" alt="" width="1200" height="630">`,
		`href="/tags/dev/tools"><span>#</span>dev/tools</a>`,
	)
	m := regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no JSON-LD")
	}
	if strings.Contains(m[1], "<") {
		t.Errorf("JSON-LD not escaped: %s", m[1])
	}
	var ld map[string]any
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Fatalf("JSON-LD: %v", err)
	}
	if ld["@type"] != "BlogPosting" || ld["headline"] != "Go & </script> tips" || ld["dateModified"] != "2026-11-03" ||
		ld["keywords"] != "go, dev/tools" || ld["author"].(map[string]any)["name"] != "Ann" || ld["inLanguage"] != "en" {
		t.Errorf("JSON-LD = %v", ld)
	}

	// The cover is a banner between the top bar and main, outside the text column.
	if c, h, m := strings.Index(body, `class="cover"`), strings.Index(body, "</header>"), strings.Index(body, `<main id="main">`); !(h < c && c < m) {
		t.Errorf("cover at %d, want between top bar (%d) and main (%d)", c, h, m)
	}

	// No cover: DEFAULT_OG_IMAGE, no banner.
	plain := s.get("/posts/zametka").Body.String()
	mustContain(t, plain, `<meta property="og:image" content="https://example.com/assets/og.png">`, `<html lang="ru"`)
	if strings.Contains(plain, `class="cover"`) {
		t.Error("banner on a post without cover")
	}
}

func TestMarkdownSource(t *testing.T) {
	s := seoSite(t)
	rec := s.get("/posts/zametka.md")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/markdown; charset=utf-8" ||
		rec.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(rec.Body.String(), "---\ntitle: Заметка") {
		t.Errorf("md = %d %v %q", rec.Code, rec.Header(), rec.Body.String())
	}
	for _, path := range []string{"/posts/draft.md", "/posts/nope.md", "/posts/zametka.md.md", "/posts/draft"} {
		if rec := s.get(path); rec.Code != 404 {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
}

func TestTagPage(t *testing.T) {
	s := seoSite(t)
	rec := s.get("/tags/go")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "#go</h1>", `href="/posts/zametka"`, `href="/tags/go?page=2"`, `<link rel="canonical" href="https://example.com/tags/go">`)
	mustContain(t, s.get("/tags/go?page=2").Body.String(), `href="/posts/go-script-tips"`)
	mustContain(t, s.get("/tags/%D0%B7%D0%B0%D0%BC%D0%B5%D1%82%D0%BA%D0%B8").Body.String(), "#заметки</h1>")
	mustContain(t, s.get("/tags/dev/tools").Body.String(), "#dev/tools</h1>")
	for _, path := range []string{"/tags/nope", "/tags/", "/tags/go?page=3", "/tags/go?page=1", "/tags/go/"} {
		if rec := s.get(path); rec.Code != 404 {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
}

func TestSearchPage(t *testing.T) {
	s := seoSite(t)
	rec := s.get("/search?q=bleve")
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body, `<meta name="robots" content="noindex">`, `<mark>Bleve</mark>`, `href="/posts/go-script-tips"`,
		`<script src="/static/search.js?v=`, `value="bleve"`, `Found: 1`)
	mustContain(t, s.get("/search?q=постов").Body.String(), `href="/posts/zametka"`)
	if b := s.get("/search?q=go&tag=заметки").Body.String(); strings.Contains(b, "go-script-tips") || !strings.Contains(b, "zametka") {
		t.Error("tag filter ignored")
	}
	mustContain(t, s.get("/search?q=secret").Body.String(), "Nothing found") // drafts are not indexed
	mustContain(t, s.get("/search?q=go").Body.String(), `href="/search?page=2&amp;q=go"`)
	xss := s.get(`/search?q="><script>alert(1)</script>`).Body.String()
	if strings.Contains(xss, "<script>alert") {
		t.Error("query not escaped")
	}
	for _, path := range []string{"/search?q=go&page=0", "/search?q=go&page=51", "/search?q=go&page=x", "/search?q=bleve&page=2"} {
		if rec := s.get(path); rec.Code != 404 {
			t.Errorf("%s = %d, want 404", path, rec.Code)
		}
	}
	if rec := s.get("/search?q=" + strings.Repeat("a", 5000)); rec.Code != 200 {
		t.Errorf("long query = %d", rec.Code)
	}
	if rec := s.get("/static/search.js"); rec.Code != 200 || rec.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Errorf("search.js = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestRSS(t *testing.T) {
	s := seoSite(t)
	rec := s.get("/rss.xml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/rss+xml; charset=utf-8" {
		t.Fatalf("rss = %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	var doc struct {
		Channel struct {
			Title         string `xml:"title"`
			LastBuildDate string `xml:"lastBuildDate"`
			Items         []struct {
				Title    string   `xml:"title"`
				Link     string   `xml:"link"`
				GUID     string   `xml:"guid"`
				PubDate  string   `xml:"pubDate"`
				Category []string `xml:"category"`
				Creator  string   `xml:"http://purl.org/dc/elements/1.1/ creator"`
				Content  string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
				Updated  string   `xml:"http://www.w3.org/2005/Atom updated"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("rss xml: %v\n%s", err, rec.Body.String())
	}
	c := doc.Channel
	if c.Title != "Test <Blog>" || c.LastBuildDate != "Tue, 03 Nov 2026 00:00:00 +0000" || len(c.Items) != 2 {
		t.Fatalf("channel = %+v", c)
	}
	mustContain(t, rec.Body.String(), "<link>https://example.com/</link>",
		`<atom:link href="https://example.com/rss.xml" rel="self" type="application/rss+xml"></atom:link>`)
	it := c.Items[1]
	if it.Link != "https://example.com/posts/go-script-tips" || it.GUID != it.Link || it.Creator != "Ann" ||
		it.Updated != "2026-11-03T00:00:00Z" || strings.Join(it.Category, ",") != "go,dev/tools" || it.PubDate != "Sun, 01 Nov 2026 00:00:00 +0000" {
		t.Errorf("item = %+v", it)
	}
	if !strings.Contains(it.Content, `href="https://example.com/"`) || !strings.Contains(it.Content, `src="https://example.com/assets/i.png"`) {
		t.Errorf("content URLs not absolute: %s", it.Content)
	}
}

func TestSitemapRobotsLLMs(t *testing.T) {
	s := seoSite(t)
	var doc struct {
		URLs []struct {
			Loc     string `xml:"loc"`
			LastMod string `xml:"lastmod"`
		} `xml:"url"`
	}
	if err := xml.Unmarshal(s.get("/sitemap.xml").Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, u := range doc.URLs {
		got = append(got, u.Loc+" "+u.LastMod)
	}
	want := "https://example.com/ 2026-11-03|https://example.com/posts/zametka 2026-11-02|https://example.com/posts/go-script-tips 2026-11-03|" +
		"https://example.com/tags/dev/tools 2026-11-03|https://example.com/tags/go 2026-11-03|https://example.com/tags/%D0%B7%D0%B0%D0%BC%D0%B5%D1%82%D0%BA%D0%B8 2026-11-02"
	if strings.Join(got, "|") != want {
		t.Errorf("sitemap =\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
	if b := s.get("/robots.txt").Body.String(); b != "User-agent: *\nAllow: /\n\nSitemap: https://example.com/sitemap.xml\n" {
		t.Errorf("robots = %q", b)
	}
	llms := s.get("/llms.txt").Body.String()
	mustContain(t, llms, "# Test <Blog>\n", "- [Заметка](https://example.com/posts/zametka.md)\n",
		`- [Go & </script> tips](https://example.com/posts/go-script-tips.md): Short "tips" </script><b>x</b>`)
	if rec := s.get("/key12345.txt"); rec.Code != 200 || rec.Body.String() != "key12345" {
		t.Errorf("indexnow key = %d %q", rec.Code, rec.Body.String())
	}
	if rec := s.get("/other.txt"); rec.Code != 404 {
		t.Errorf("other.txt = %d", rec.Code)
	}
}

func TestSearchTags(t *testing.T) {
	s := seoSite(t)
	t.Run("tag in the query", func(t *testing.T) {
		body := s.get("/search?q=%23go+%D0%BF%D0%BE%D1%81%D1%82%D0%BE%D0%B2").Body.String()
		mustContain(t, body, `href="/posts/zametka"`, `value="#go постов"`, "Found: 1")
		if strings.Contains(body, `href="/posts/go-script-tips"`) {
			t.Error("post without the word")
		}
	})
	t.Run("tags only list by date", func(t *testing.T) {
		body := s.get("/search?q=%23go").Body.String()
		mustContain(t, body, "Found: 2", `href="/search?page=2&amp;q=%23go"`, `href="/posts/zametka"`)
		mustContain(t, s.get("/search?q=%23go&page=2").Body.String(), `href="/posts/go-script-tips"`)
	})
	t.Run("several tags mean AND", func(t *testing.T) {
		mustContain(t, s.get("/search?q=%23go+%23dev/tools").Body.String(), "Found: 1", `href="/posts/go-script-tips"`)
		mustContain(t, s.get("/search?q=%23dev/tools+%23заметки").Body.String(), "Found: 0")
	})
	t.Run("old tag links", func(t *testing.T) {
		body := s.get("/search?tag=go").Body.String()
		mustContain(t, body, `value="#go"`, "Found: 2", `href="/search?page=2&amp;q=%23go"`)
		mustContain(t, s.get("/search?q=много&tag=заметки").Body.String(), `value="#заметки много"`)
		// An unknown tag stays out of the query.
		mustContain(t, s.get(`/search?q=go&tag="><b>`).Body.String(), `value="go"`)
	})
	t.Run("no results", func(t *testing.T) {
		body := s.get(`/search?q=zzzz+"><script>alert(1)</script>`).Body.String()
		mustContain(t, body, "Nothing found for “zzzz &#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;”. Check the word or remove #tag from the query.")
		if strings.Contains(body, "<script>alert") {
			t.Error("query not escaped")
		}
		mustContain(t, s.get("/search?q=zzzz", &http.Cookie{Name: "lang", Value: "ru"}).Body.String(), "Ничего не нашлось по «zzzz». Проверь слово или убери #тег из запроса.")
	})
	t.Run("title match is marked", func(t *testing.T) {
		mustContain(t, s.get("/search?q=tips").Body.String(), `<h2 class="hit-title"><a href="/posts/go-script-tips">Go &amp; &lt;/script&gt; <mark>tips</mark></a></h2>`)
	})
	t.Run("no tag dropdown", func(t *testing.T) {
		if body := s.get("/search?q=go").Body.String(); strings.Contains(body, "<select") || strings.Contains(body, `name="tag"`) {
			t.Error("tag dropdown on the search page")
		}
	})
}

func TestPaletteOnEveryPage(t *testing.T) {
	s := seoSite(t)
	for _, path := range []string{"/", "/posts/zametka", "/search", "/tags/go", "/nope"} {
		body := s.get(path).Body.String()
		mustContain(t, body, `<dialog class="palette" id="palette"`, `<script src="/static/search.js?v=`,
			`role="combobox"`, `aria-controls="palette-list"`, `id="palette-list" role="listbox"`,
			"filters by tag", "<kbd>↑ ↓</kbd>", "<kbd>Enter</kbd>", "<kbd>Esc</kbd>",
			`<script type="application/json" id="tag-data">[{"Name":"go","Posts":2},{"Name":"dev/tools","Posts":1},{"Name":"заметки","Posts":1}]</script>`)
		if strings.Contains(body, "<dialog class=\"palette\" id=\"palette\" lang=\"en\" open") {
			t.Errorf("%s: palette open", path)
		}
	}
	// The script is not listed twice on the post page, and the post scripts stay.
	post := s.get("/posts/zametka").Body.String()
	if strings.Count(post, `src="/static/search.js`) != 1 {
		t.Error("search.js listed twice")
	}
	mustContain(t, post, `src="/static/altcha.js`, `src="/static/post.js`)
	mustContain(t, s.get("/", &http.Cookie{Name: "lang", Value: "ru"}).Body.String(), "<kbd>#тег</kbd> фильтр по тегу", "выбрать", "открыть", "закрыть")
}

func TestTagDataEscaped(t *testing.T) {
	// Tags cannot hold these characters; the JSON stays safe in a script element anyway.
	b, err := json.Marshal([]content.TagCount{{Name: "</script><b>&", Posts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(b), "<>&") {
		t.Errorf("not escaped: %s", b)
	}
}
