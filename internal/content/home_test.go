package content

import (
	"log/slog"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func home(yaml string) []byte { return []byte("---\n" + yaml + "---\nBody is ignored.\n") }

func TestParseHome(t *testing.T) {
	blocks, problems := ParseHome(home(`blocks:
  - type: intro
    title: {ru: "Привет", en: "Hello"}
    text: Plain text
  - type: tags
  - type: tags
    limit: 20
  - type: featured
    area: main
  - type: posts
    group: year
    limit: 100
  - type: popular
    limit: 10
    area: side
obsidian_field: ignored
`))
	if len(problems) != 0 {
		t.Errorf("problems = %v", problems)
	}
	want := []Block{
		{Type: BlockIntro, Title: Text{{"ru", "Привет"}, {"en", "Hello"}}, Text: Text{{"", "Plain text"}}},
		{Type: BlockTags, Limit: 6},
		{Type: BlockTags, Limit: 20},
		{Type: BlockFeatured},
		{Type: BlockPosts, GroupYear: true, Limit: 100},
		{Type: BlockPopular, Limit: 10, Side: true},
	}
	if len(blocks) != len(want) {
		t.Fatalf("blocks = %+v", blocks)
	}
	for i, b := range blocks {
		w := want[i]
		if b.Type != w.Type || b.Limit != w.Limit || b.GroupYear != w.GroupYear || b.Side != w.Side ||
			!slices.Equal(b.Title, w.Title) || !slices.Equal(b.Text, w.Text) {
			t.Errorf("block %d = %+v, want %+v", i, b, w)
		}
	}
	if got := blocks[0].Title.In("en"); got != "Hello" {
		t.Errorf(`In("en") = %q`, got)
	}
	if got := blocks[0].Title.In("de"); got != "Привет" {
		t.Errorf(`In("de") = %q, want the first`, got)
	}
	if got := blocks[0].Text.In("ru"); got != "Plain text" {
		t.Errorf(`plain In("ru") = %q`, got)
	}
}

func TestParseHomeDefaults(t *testing.T) {
	for name, src := range map[string][]byte{
		"no frontmatter": []byte("just text"),
		"not closed":     []byte("---\nblocks: []\n"),
		"invalid YAML":   home("blocks: [\n"),
		"no blocks":      home("title: x\n"),
		"empty blocks":   home("blocks: []\n"),
		"scalar blocks":  home("blocks: nope\n"),
		"none valid":     home("blocks:\n  - type: nope\n  - 5\n"),
		"too large":      []byte("---\n" + strings.Repeat("x: 1\n", maxFrontmatter) + "---\n"),
	} {
		blocks, problems := ParseHome(src)
		if len(problems) == 0 {
			t.Errorf("%s: no problem reported", name)
		}
		if !slices.EqualFunc(blocks, DefaultHome(), func(a, b Block) bool { return a.Type == b.Type && a.Limit == b.Limit && a.Side == b.Side }) {
			t.Errorf("%s: blocks = %+v", name, blocks)
		}
	}
}

func TestParseHomeSkipsBadBlocks(t *testing.T) {
	long := strings.Repeat("я", maxBlockTitle+1)
	for _, bad := range []string{
		"{type: nope}",
		"{title: no type}",
		"text only",
		"{type: tags, limit: 0}",
		"{type: tags, limit: 21}",
		"{type: tags, limit: six}",
		"{type: tags, limit: 1.5}",
		"{type: popular, limit: 11}",
		"{type: posts, limit: 101}",
		"{type: posts, group: month}",
		"{type: posts, group: [year]}",
		"{type: tags, area: left}",
		"{type: tags, title: not for tags}",
		"{type: featured, limit: 1}",
		"{type: intro, limt: 1}",
		"{type: intro, title: 2026}",
		"{type: intro, title: [a]}",
		"{type: intro, title: {russian: a}}",
		"{type: intro, title: {ru: [a]}}",
		"{type: intro, title: " + long + "}",
		"{type: intro, title: {ru: " + long + "}}",
		"{type: intro, text: " + strings.Repeat("a", maxBlockText+1) + "}",
	} {
		blocks, problems := ParseHome(home("blocks:\n  - type: featured\n  - " + bad + "\n"))
		if len(blocks) != 1 || blocks[0].Type != BlockFeatured || len(problems) != 1 {
			t.Errorf("%.60s: blocks = %+v, problems = %v", bad, blocks, problems)
		}
	}
	// At the limits, and an empty Obsidian property.
	blocks, problems := ParseHome(home("blocks:\n  - {type: intro, title: " + strings.Repeat("я", maxBlockTitle) + ", text: }\n"))
	if len(blocks) != 1 || len(problems) != 0 || blocks[0].Text != nil {
		t.Errorf("at the limit: %+v, %v", blocks, problems)
	}
}

func TestParseHomeOnePostsBlockAndMaxBlocks(t *testing.T) {
	blocks, problems := ParseHome(home("blocks:\n  - type: posts\n  - type: posts\n"))
	if len(blocks) != 1 || len(problems) != 1 {
		t.Errorf("two posts blocks: %+v, %v", blocks, problems)
	}
	blocks, problems = ParseHome(home("blocks:\n" + strings.Repeat("  - type: featured\n", maxHomeBlocks+5)))
	if len(blocks) != maxHomeBlocks || len(problems) != 1 {
		t.Errorf("too many blocks: %d, %v", len(blocks), problems)
	}
}

func TestTopTags(t *testing.T) {
	var posts []*Post
	for i, tags := range [][]string{{"b", "c"}, {"a", "c"}, {"a", "c"}, {"b"}} {
		posts = append(posts, &Post{Key: string(rune('a'+i)) + ".md", Slug: string(rune('a' + i)), Title: "T", Tags: tags, Lang: "en"})
	}
	posts = append(posts, &Post{Key: "d.md", Slug: "draft", Title: "T", Tags: []string{"z", "z2"}, Lang: "en", Draft: true})
	ix := NewIndex(posts, slog.New(slog.DiscardHandler))
	if got, want := ix.TopTags(2), []TagCount{{"c", 3}, {"a", 2}}; !slices.Equal(got, want) {
		t.Errorf("TopTags(2) = %v, want %v", got, want)
	}
	if got, want := ix.TopTags(20), []TagCount{{"c", 3}, {"a", 2}, {"b", 2}}; !slices.Equal(got, want) {
		t.Errorf("TopTags(20) = %v, want %v", got, want)
	}
}

func FuzzParseHome(f *testing.F) {
	f.Add(home("blocks:\n  - {type: intro, title: {ru: a, en: b}, text: c}\n  - {type: posts, group: year, limit: 3, area: side}\n"))
	f.Add(home("blocks: &a [*a]\n"))
	f.Add(home("blocks:\n  - &x {type: tags}\n  - *x\n  - {<<: *x, limit: 2}\n"))
	f.Add([]byte("no frontmatter"))
	f.Fuzz(func(t *testing.T, src []byte) {
		blocks, _ := ParseHome(src)
		if len(blocks) == 0 || len(blocks) > maxHomeBlocks {
			t.Fatalf("%d blocks", len(blocks))
		}
		posts := 0
		for _, b := range blocks {
			spec, ok := blockSpecs[b.Type]
			if !ok {
				t.Fatalf("type %q", b.Type)
			}
			if b.Limit < 0 || b.Limit > spec.limitMax {
				t.Fatalf("limit %d for %q", b.Limit, b.Type)
			}
			for _, e := range b.Title {
				if utf8.RuneCountInString(e.Value) > maxBlockTitle {
					t.Fatalf("title too long")
				}
			}
			for _, e := range b.Text {
				if utf8.RuneCountInString(e.Value) > maxBlockText {
					t.Fatalf("text too long")
				}
			}
			if b.Type == BlockPosts {
				posts++
			}
		}
		if posts > 1 {
			t.Fatalf("%d posts blocks", posts)
		}
	})
}
