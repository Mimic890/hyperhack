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

// Tag returns published posts with this tag, newest first.
func (ix *Index) Tag(tag string) []*Post { return ix.byTag[tag] }

// Tags returns all tags, sorted.
func (ix *Index) Tags() []string { return ix.tags }

// TopTags returns the n most used tags. Callers must not change the slice.
func (ix *Index) TopTags(n int) []TagCount { return ix.counts[:min(n, len(ix.counts))] }

// Get returns the post with this slug, or nil.
func (ix *Index) Get(slug string) *Post { return ix.bySlug[slug] }

// Posts returns published posts, newest first. Callers must not change the slice.
func (ix *Index) Posts() []*Post { return ix.posts }
