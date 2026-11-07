package content

import (
	"log/slog"
	"slices"
	"strconv"
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
	for _, bad := range []string{"{type: nope}", "{title: no type}", "text only"} {
		blocks, problems := ParseHome(home("blocks:\n  - type: featured\n  - " + bad + "\n"))
		if len(blocks) != 1 || blocks[0].Type != BlockFeatured || len(problems) != 1 {
			t.Errorf("%.60s: blocks = %+v, problems = %v", bad, blocks, problems)
		}
	}
}

// A bad or unknown field is dropped with a warning; the block stays with its defaults.
func TestParseHomeIgnoresBadFields(t *testing.T) {
	long := strings.Repeat("я", maxBlockTitle+1)
	for _, c := range []struct{ field, want string }{
		{"limit: 0", "tags/6"},
		{"limit: 21", "tags/6"},
		{"limit: six", "tags/6"},
		{"limit: 1.5", "tags/6"},
		{"area: left", "tags/6"},
		{"title: not for tags", "tags/6"},
		{"limt: 1", "tags/6"},
	} {
		blocks, problems := ParseHome(home("blocks:\n  - {type: tags, " + c.field + "}\n"))
		got := ""
		if len(blocks) == 1 {
			got = blocks[0].Type + "/" + strconv.Itoa(blocks[0].Limit)
		}
		if got != c.want || blocks[0].Side || len(problems) != 1 {
			t.Errorf("%s: %+v, problems = %v", c.field, blocks, problems)
		}
	}
	for _, field := range []string{
		"text: [a]",
		"text: {russian: a}",
		"text: {ru: [a]}",
		"text: " + strings.Repeat("a", maxBlockText+1),
		"description: x",
	} {
		blocks, problems := ParseHome(home("blocks:\n  - {type: intro, title: Hi, " + field + "}\n"))
		if len(blocks) != 1 || blocks[0].Title.In("en") != "Hi" || blocks[0].Text != nil || len(problems) != 1 {
			t.Errorf("%.60s: %+v, problems = %v", field, blocks, problems)
		}
	}
	for _, field := range []string{"title: " + long, "title: {ru: " + long + "}", "title: [a]"} {
		blocks, problems := ParseHome(home("blocks:\n  - {type: intro, text: Body, " + field + "}\n"))
		if len(blocks) != 1 || blocks[0].Title != nil || blocks[0].Text.In("en") != "Body" || len(problems) != 1 {
			t.Errorf("%.60s: %+v, problems = %v", field, blocks, problems)
		}
	}
	for _, c := range []struct{ field, want string }{
		{"posts, limit: 101", "posts/0"},
		{"posts, group: month", "posts/0"},
		{"posts, group: [year]", "posts/0"},
		{"popular, limit: 11", "popular/4"},
		{"featured, limit: 1", "featured/0"},
	} {
		blocks, problems := ParseHome(home("blocks:\n  - {type: " + c.field + "}\n"))
		if len(blocks) != 1 || blocks[0].Type+"/"+strconv.Itoa(blocks[0].Limit) != c.want || blocks[0].GroupYear || len(problems) != 1 {
			t.Errorf("%s: %+v, problems = %v", c.field, blocks, problems)
		}
	}
}

// Clearing a text in Obsidian leaves an empty value: the title must survive it.
func TestParseHomeEmptyText(t *testing.T) {
	for _, field := range []string{"text:", `text: ""`, "text: {en: , ru: }", "text: {en: '', ru: ' '}"} {
		blocks, problems := ParseHome(home("blocks:\n  - type: intro\n    title: Hi\n    " + field + "\n"))
		if len(blocks) != 1 || blocks[0].Title.In("ru") != "Hi" || blocks[0].Text.In("ru") != "" || len(problems) != 0 {
			t.Errorf("%s: %+v, problems = %v", field, blocks, problems)
		}
	}
	blocks, _ := ParseHome(home("blocks:\n  - type: intro\n    title: {en: Hi, ru: Привет}\n    text:\n      en:\n      ru: Текст\n"))
	if b := blocks[0]; b.Title.In("ru") != "Привет" || b.Text.In("ru") != "Текст" || b.Text.In("en") != "Текст" {
		t.Errorf("one empty language: %+v", b)
	}
	blocks, _ = ParseHome(home("blocks:\n  - {type: intro, title: 2026}\n"))
	if blocks[0].Title.In("en") != "2026" {
		t.Errorf("number title: %+v", blocks[0])
	}
}

func TestParseHomeAtLimits(t *testing.T) {
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
