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
	tests := []struct{ q, tag, want string }{
		{"ghost", "", "ghost"},
		{"GHOST binary", "", "ghost"},
		{"ghist", "", "ghost"},      // one-letter typo
		{"replcaed", "", "ghost"},   // two edits in a long word
		{"bin", "", "ghost"},        // prefix while typing
		{"пост", "", "ru"},          // Russian stemming: "постах", "постов"
		{"постов", "", "ru"},        // and the other way round
		{"синхронизация", "", "ru"}, // stem of "синхронизацию"
		{"kubernetes", "", "xss"},
		{"go", "", "go,ghost"},      // tags outrank body; both have the tag
		{"go", "selfhost", "ghost"}, // tag filter
		{"ghost", "заметки", ""},
		{"nothingmatches", "", ""},
		{"", "", ""},
		{"!!! ???", "", ""},
	}
	for _, tt := range tests {
		hits, _, err := ix.Search(tt.q, tt.tag, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		got := slugsOf(hits)
		if tt.q == "go" && tt.tag == "" {
			if !strings.Contains(got, "go") || !strings.Contains(got, "ghost") {
				t.Errorf("Search(%q) = %q, want both go posts", tt.q, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("Search(%q, %q) = %q, want %q", tt.q, tt.tag, got, tt.want)
		}
	}
}

func TestSearchSnippetEscaped(t *testing.T) {
	ix := searchIndex(t)
	hits, _, _ := ix.Search("kubernetes", "", 1, 10)
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
	if hits, _, _ := ix.Search("ghost", "", MaxSearchPage+1, 10); hits != nil {
		t.Error("page past the limit returned hits")
	}
	start := time.Now()
	_, _, _ = ix.Search(strings.Repeat("abcdefghij ", 20), "", 1, 10)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("worst-case query took %s", d)
	}
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
		f.Add(s, "go")
	}
	f.Fuzz(func(t *testing.T, q, tag string) {
		terms := Terms(q)
		if len(terms) > maxTerms {
			t.Fatalf("%d terms", len(terms))
		}
		if _, _, err := ix.Search(q, tag, 1, 10); err != nil {
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
