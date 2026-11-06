package content

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func searchIndex(t testing.TB) *Index {
	t.Helper()
	r := NewRenderer("en")
	src := map[string]string{
		"ghost.md": "---\ntitle: Leaving Ghost\ndescription: A lighter blog engine\ndate: 2026-11-03\ntags: [go, selfhost]\n---\nI replaced Ghost with a small Go binary that syncs from a bucket.\n",
		"ru.md":    "---\ntitle: Заметка о постах\ndate: 2026-11-02\nlang: ru\ntags: [заметки]\n---\nЗдесь много постов про синхронизацию и <b>разметку</b>.\n",
		"xss.md":   "---\ntitle: Escaping\ndate: 2026-11-01\n---\nThe word kubernetes next to `<script>alert(1)</script>` in code.\n",
		"go.md":    "---\ntitle: Tags only\ndate: 2026-10-01\ntags: [go]\n---\nNothing about the topic in the body.\n",
	}
	var posts []*Post
	for k, s := range src {
		p, err := r.Parse(k, []byte(s))
		if err != nil {
			t.Fatal(k, err)
		}
		posts = append(posts, p)
	}
	return NewIndex(posts, slog.New(slog.DiscardHandler))
}

func slugsOf(hits []Hit) string {
	var s []string
	for _, h := range hits {
		s = append(s, h.Post.Slug)
	}
	return strings.Join(s, ",")
}

func TestSearch(t *testing.T) {
	ix := searchIndex(t)
	tests := []struct{ q, want string }{
		{"ghost", "ghost"},
		{"GHOST binary", "ghost"},
		{"ghist", "ghost"},      // one-letter typo
		{"replcaed", "ghost"},   // two edits in a long word
		{"bin", "ghost"},        // prefix while typing
		{"пост", "ru"},          // Russian stemming: "постах", "постов"
		{"постов", "ru"},        // and the other way round
		{"синхронизация", "ru"}, // stem of "синхронизацию"
		{"kubernetes", "xss"},
		{"go", "go,ghost"},        // tags outrank body; both have the tag
		{"go #selfhost", "ghost"}, // tag filter
		{"#selfhost go", "ghost"}, // in any place
		{"ghost #заметки", ""},
		{"#GO #SelfHost", "ghost"}, // several tags mean AND, any case
		{"#go", "ghost,go"},        // tags only: by date, newest first
		{"#selfhost #заметки", ""},
		{"#nosuchtag", ""},
		{"ghost #nosuchtag", ""},
		{"ghost #bad!tag", "ghost"}, // invalid syntax is ignored
		{"#", ""},
		{"# ##", ""},
		{"nothingmatches", ""},
		{"", ""},
		{"!!! ???", ""},
	}
	for _, tt := range tests {
		hits, _, err := ix.Search(tt.q, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		got := slugsOf(hits)
		if tt.q == "go" {
			if !strings.Contains(got, "go") || !strings.Contains(got, "ghost") {
				t.Errorf("Search(%q) = %q, want both go posts", tt.q, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("Search(%q) = %q, want %q", tt.q, got, tt.want)
		}
	}
}

func TestSearchSnippetEscaped(t *testing.T) {
	ix := searchIndex(t)
	hits, _, _ := ix.Search("kubernetes", 1, 10)
	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	s := string(hits[0].Snippet)
	if !strings.Contains(s, "<mark>kubernetes</mark>") || strings.Contains(s, "<script>") {
		t.Errorf("snippet = %s", s)
	}
}

func TestSearchLimits(t *testing.T) {
	if got := Terms(strings.Repeat("a ", 50)); len(got) != maxTerms {
		t.Errorf("terms = %d, want %d", len(got), maxTerms)
	}
	if got := Terms(strings.Repeat("я", 500)); len([]rune(got[0])) != MaxQueryRunes {
		t.Errorf("term runes = %d", len([]rune(got[0])))
	}
	// Bleve query syntax is plain text here, not operators.
	if got := Terms(`title:x* +foo -bar /re.*/ "phrase"`); strings.Join(got, " ") != "title x foo bar re phrase" {
		t.Errorf("terms = %q", got)
	}
	ix := searchIndex(t)
	if hits, _, _ := ix.Search("ghost", MaxSearchPage+1, 10); hits != nil {
		t.Error("page past the limit returned hits")
	}
	start := time.Now()
	_, _, _ = ix.Search(strings.Repeat("abcdefghij ", 20), 1, 10)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("worst-case query took %s", d)
	}
}

func TestParseQuery(t *testing.T) {
	for _, tt := range []struct{ q, terms, tags string }{
		{"#self-host garage", "garage", "self-host"},
		{"garage #Self-Host #dev/go_1 x", "garage x", "self-host,dev/go_1"},
		{"#заметки пост", "пост", "заметки"},
		{"#go #go", "", "go"},
		{"#go, a#b #a#b # ## #!", "a b", ""},
		{"#" + strings.Repeat("a", 51) + " x", "x", ""},
		{"#" + strings.Repeat("a", 50), "", strings.Repeat("a", 50)},
		{"#1 #2 #3 #4 #5 a b c d e f g", "a b c d e", "1,2,3,4,5"},
		{"a b c d e f g h i #1 #2 j", "a b c d e f g h i", "1"},
		{"a-b-c-d-e-f-g-h-i-j-k-l", "a b c d e f g h i j", ""},
		{"a b c d e f g h i j #1", "a b c d e f g h i j", ""},
	} {
		terms, tags := ParseQuery(tt.q)
		if got := strings.Join(terms, " "); got != tt.terms {
			t.Errorf("ParseQuery(%q) terms = %q, want %q", tt.q, got, tt.terms)
		}
		if got := strings.Join(tags, ","); got != tt.tags {
			t.Errorf("ParseQuery(%q) tags = %q, want %q", tt.q, got, tt.tags)
		}
	}
	// The rune limit applies before the split.
	if _, tags := ParseQuery(strings.Repeat(" ", MaxQueryRunes) + "#go"); tags != nil {
		t.Errorf("tag past the limit: %v", tags)
	}
}

func TestSearchTitleMarks(t *testing.T) {
	r := NewRenderer("en")
	var posts []*Post
	for k, s := range map[string]string{
		"a.md": "---\ntitle: Go & <b>Garage</b> notes\ndate: 2026-11-03\ntags: [x]\n---\nText.\n",
		"b.md": "---\ntitle: \"" + strings.Repeat("long words ", 60) + "garage\"\ndate: 2026-11-02\n---\nText.\n",
		"c.md": "---\ntitle: Заметка о постах\ndate: 2026-11-01\nlang: ru\n---\nТекст.\n",
		"d.md": "---\ntitle: Other\ndate: 2026-10-01\n---\nAbout garage in the body.\n",
	} {
		p, err := r.Parse(k, []byte(s))
		if err != nil {
			t.Fatal(k, err)
		}
		posts = append(posts, p)
	}
	ix := NewIndex(posts, slog.New(slog.DiscardHandler))
	titles := func(q string) map[string]string {
		hits, _, err := ix.Search(q, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, h := range hits {
			m[h.Post.Slug] = string(h.Title)
		}
		return m
	}
	got := titles("garage")
	if want := "Go &amp; &lt;b&gt;<mark>Garage</mark>&lt;/b&gt; notes"; got["a"] != want {
		t.Errorf("title = %q, want %q", got["a"], want)
	}
	if got["b"] != "" {
		t.Errorf("a cut title must stay plain: %q", got["b"])
	}
	if got["d"] != "Other" {
		t.Errorf("no match in the title: %q", got["d"])
	}
	if want := "Заметка о <mark>постах</mark>"; titles("пост")["c"] != want {
		t.Errorf("stem = %q", titles("пост")["c"])
	}
	if want := "Go &amp; &lt;b&gt;<mark>Garage</mark>&lt;/b&gt; notes"; titles("garaje")["a"] != want { // a typo
		t.Errorf("fuzzy = %q", titles("garaje")["a"])
	}
	if got := titles("#x")["a"]; got != "" {
		t.Errorf("tags only: %q", got)
	}
}

func TestSearchTagPages(t *testing.T) {
	var posts []*Post
	for i := range 5 {
		slug := string(rune('a' + i))
		posts = append(posts, &Post{Key: slug + ".md", Slug: slug, Title: "T", Tags: []string{"x"}, Lang: "en", Date: time.Date(2026, 11, 1+i, 0, 0, 0, 0, time.UTC)})
	}
	ix := NewIndex(posts, slog.New(slog.DiscardHandler))
	for _, tt := range []struct {
		page int
		want string
	}{{1, "e,d"}, {2, "c,b"}, {3, "a"}, {4, ""}} {
		hits, total, err := ix.Search("#x", tt.page, 2)
		if err != nil || total != 5 || slugsOf(hits) != tt.want {
			t.Errorf("page %d: %q, total %d, err %v", tt.page, slugsOf(hits), total, err)
		}
	}
}

func FuzzParseQuery(f *testing.F) {
	for _, s := range []string{"#self-host garage", "# ## #!", "a#b", "#заметки", "#" + strings.Repeat("a", 60), strings.Repeat("#a ", 30)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, q string) {
		terms, tags := ParseQuery(q)
		if len(terms)+len(tags) > maxTerms {
			t.Fatalf("%d terms, %d tags", len(terms), len(tags))
		}
		for _, tag := range tags {
			if !tagRe.MatchString(tag) {
				t.Fatalf("bad tag %q", tag)
			}
		}
	})
}

func TestIndexTags(t *testing.T) {
	ix := searchIndex(t)
	if got := strings.Join(ix.Tags(), ","); got != "go,selfhost,заметки" {
		t.Errorf("tags = %q", got)
	}
	if ps := ix.Tag("go"); len(ps) != 2 || ps[0].Slug != "ghost" {
		t.Errorf("Tag(go) = %v", ps)
	}
}

func FuzzSearch(f *testing.F) {
	ix := searchIndex(f)
	for _, s := range []string{"ghost", "title:x*", "/.*/", `"a`, "пост~2", strings.Repeat("ab ", 30)} {
		f.Add(s + " #go")
	}
	f.Fuzz(func(t *testing.T, q string) {
		terms := Terms(q)
		if len(terms) > maxTerms {
			t.Fatalf("%d terms", len(terms))
		}
		if _, _, err := ix.Search(q, 1, 10); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPlainText(t *testing.T) {
	got := plainText("<h2>Title</h2>\n<p>Some <strong>bold</strong>, <a href=\"/\">link</a>.</p><ul><li>a</li><li>b</li></ul><p>x<br>y</p>")
	if want := "Title Some bold, link. a b x y"; got != want {
		t.Errorf("plainText = %q, want %q", got, want)
	}
}

func TestOutboundHosts(t *testing.T) {
	got := outboundHosts(`<a href="https://GitHub.com/x">a</a> <a href="/local">b</a> <a href="#h">c</a> <a href="http://github.com/y">d</a> <a href="mailto:a@b.c">e</a> <a href="https://t.me:443/z">f</a>`)
	if strings.Join(got, ",") != "github.com,t.me" {
		t.Errorf("outboundHosts = %v", got)
	}
}
