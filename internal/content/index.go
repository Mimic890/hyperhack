package content

import (
	"log/slog"
	"slices"
	"strings"
)

// Index is an immutable set of published posts.
type Index struct {
	posts  []*Post // newest first
	bySlug map[string]*Post
	Logo   bool // assets/logo.svg is in the mirror
}

// NewIndex builds an index. Drafts are left out.
// On a slug conflict the first post by object key wins and the other is logged as ERROR.
func NewIndex(posts []*Post, log *slog.Logger) *Index {
	posts = slices.Clone(posts)
	slices.SortFunc(posts, func(a, b *Post) int { return strings.Compare(a.Key, b.Key) })
	ix := &Index{bySlug: map[string]*Post{}}
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
	return ix
}

// Get returns the post with this slug, or nil.
func (ix *Index) Get(slug string) *Post { return ix.bySlug[slug] }

// Posts returns published posts, newest first. Callers must not change the slice.
func (ix *Index) Posts() []*Post { return ix.posts }
