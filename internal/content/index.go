package content

import (
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2"
)

// Index is an immutable set of published posts.
type Index struct {
	posts  []*Post // newest first
	bySlug map[string]*Post
	byTag  map[string][]*Post // newest first
	tags   []string           // sorted
	counts []TagCount         // most posts first, ties by name
	search bleve.Index
	Logo   bool    // assets/logo.svg is in the mirror
	Home   []Block // main page blocks

	themes    map[string][]byte // theme stylesheet ID → CSS
	siteTheme string            // ID, empty when home.md sets no colors
	postTheme map[*Post]string  // posts with colors of their own
}

// TagCount is a tag with its number of published posts.
type TagCount struct {
	Name  string
	Posts int
}

// NewIndex builds an index. Drafts are left out.
// On a slug conflict the first post by object key wins and the other is logged as ERROR.
func NewIndex(posts []*Post, log *slog.Logger) *Index {
	posts = slices.Clone(posts)
	slices.SortFunc(posts, func(a, b *Post) int { return strings.Compare(a.Key, b.Key) })
	ix := &Index{bySlug: map[string]*Post{}, Home: DefaultHome()}
	for _, p := range posts {
		if p.Draft {
			continue
		}
		if first, ok := ix.bySlug[p.Slug]; ok {
			log.Error("slug conflict, post skipped", "slug", p.Slug, "key", p.Key, "kept", first.Key)
			continue
		}
		ix.bySlug[p.Slug] = p
		ix.posts = append(ix.posts, p)
	}
	slices.SortStableFunc(ix.posts, func(a, b *Post) int { return b.Date.Compare(a.Date) })
	ix.byTag = map[string][]*Post{}
	for _, p := range ix.posts {
		for _, t := range p.Tags {
			ix.byTag[t] = append(ix.byTag[t], p)
		}
	}
	ix.tags = slices.Sorted(maps.Keys(ix.byTag))
	for _, t := range ix.tags {
		ix.counts = append(ix.counts, TagCount{t, len(ix.byTag[t])})
	}
	slices.SortStableFunc(ix.counts, func(a, b TagCount) int { return b.Posts - a.Posts })
	var err error
	if ix.search, err = newSearchIndex(ix.posts); err != nil {
		panic(err) // the mapping is static, so this is a bug, not bad input
	}
	return ix
}

// SetTheme builds the stylesheets for the site theme and for each post that sets
// its own colors. Call it once, before the index is shared.
func (ix *Index) SetTheme(site Theme) {
	ix.themes, ix.postTheme, ix.siteTheme = map[string][]byte{}, map[*Post]string{}, ""
	add := func(t Theme) string {
		css := t.CSS()
		if css == nil {
			return ""
		}
		id := themeID(css)
		ix.themes[id] = css
		return id
	}
	ix.siteTheme = add(site)
	for _, p := range ix.posts {
		if p.Theme.Light != nil || p.Theme.Dark != nil {
			ix.postTheme[p] = add(p.Theme.Over(site))
		}
	}
}

// ThemeID returns the theme stylesheet ID for a post page, or for other pages
// when p is nil. Empty means the built-in colors.
func (ix *Index) ThemeID(p *Post) string {
	if id, ok := ix.postTheme[p]; ok {
		return id
	}
	return ix.siteTheme
}

// ThemeCSS returns the theme stylesheet with this ID.
func (ix *Index) ThemeCSS(id string) ([]byte, bool) {
	css, ok := ix.themes[id]
	return css, ok
}

// Tag returns published posts with this tag, newest first.
func (ix *Index) Tag(tag string) []*Post { return ix.byTag[tag] }

// Tags returns all tags, sorted.
func (ix *Index) Tags() []string { return ix.tags }

// TopTags returns the n most used tags. Callers must not change the slice.
func (ix *Index) TopTags(n int) []TagCount { return ix.counts[:min(n, len(ix.counts))] }

// Get returns the post with this slug, or nil.
func (ix *Index) Get(slug string) *Post { return ix.bySlug[slug] }

// Older returns the next older published post, or nil.
func (ix *Index) Older(p *Post) *Post {
	if i := slices.Index(ix.posts, p); i >= 0 && i+1 < len(ix.posts) {
		return ix.posts[i+1]
	}
	return nil
}

// Posts returns published posts, newest first. Callers must not change the slice.
func (ix *Index) Posts() []*Post { return ix.posts }
