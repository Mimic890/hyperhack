// Package content parses posts (frontmatter + Markdown) and holds the post index.
package content

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"path"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Mimic890/hyperhack/internal/mirror"
)

// Post is one published or draft post.
type Post struct {
	Key         string // bucket key of the .md file
	Slug        string
	Title       string
	Description string
	Date        time.Time
	Updated     time.Time // zero if not set
	Tags        []string
	Cover       string // key inside assets/, empty if not set
	Lang        string
	Draft       bool
	Words       int           // body word count, for reading time
	HTML        template.HTML // sanitized
}

const maxFrontmatter = 64 << 10

var (
	slugRe = regexp.MustCompile(`^[a-z0-9-]{1,100}$`)
	langRe = regexp.MustCompile(`^[a-z]{2}$`)
)

type frontmatter struct {
	Title       string     `yaml:"title"`
	Description string     `yaml:"description"`
	Date        string     `yaml:"date"`
	Updated     string     `yaml:"updated"`
	Tags        stringList `yaml:"tags"`
	Cover       string     `yaml:"cover"`
	Slug        string     `yaml:"slug"`
	Lang        string     `yaml:"lang"`
	Draft       bool       `yaml:"draft"`
}

// stringList accepts a YAML list or a single string.
type stringList []string

func (l *stringList) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		*l = []string{n.Value}
		return nil
	}
	var s []string
	if err := n.Decode(&s); err != nil {
		return err
	}
	*l = s
	return nil
}

// Parse reads frontmatter and renders the body. The error says why a post is skipped.
func (r *Renderer) Parse(key string, src []byte) (*Post, error) {
	fm, body, err := splitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	var f frontmatter
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	p := &Post{
		Key:         key,
		Title:       strings.TrimSpace(f.Title),
		Description: strings.TrimSpace(f.Description),
		Lang:        r.defaultLang,
		Draft:       f.Draft,
	}
	if p.Title == "" {
		return nil, errors.New("missing title")
	}
	if f.Date == "" {
		return nil, errors.New("missing date")
	}
	if p.Date, err = parseDate(f.Date); err != nil {
		return nil, fmt.Errorf("date: %w", err)
	}
	if f.Updated != "" {
		if p.Updated, err = parseDate(f.Updated); err != nil {
			return nil, fmt.Errorf("updated: %w", err)
		}
	}
	for _, t := range f.Tags {
		if t = strings.TrimSpace(t); t != "" {
			p.Tags = append(p.Tags, t)
		}
	}
	if f.Cover != "" {
		c := strings.TrimPrefix(f.Cover, "/")
		if k, err := mirror.Classify(c); err != nil || k != mirror.Asset {
			return nil, errors.New("cover must be a file inside assets/")
		}
		p.Cover = c
	}
	if f.Lang != "" {
		if !langRe.MatchString(f.Lang) {
			return nil, errors.New("lang must be a two-letter ISO 639-1 code")
		}
		p.Lang = f.Lang
	}
	switch {
	case f.Slug != "":
		if !slugRe.MatchString(f.Slug) {
			return nil, errors.New("slug must match ^[a-z0-9-]{1,100}$")
		}
		p.Slug = f.Slug
	default:
		base := path.Base(key)
		if p.Slug = Slugify(strings.TrimSuffix(base, path.Ext(base))); p.Slug == "" {
			return nil, errors.New("no slug: set slug in frontmatter")
		}
	}
	p.Words = len(bytes.Fields(body))
	if p.HTML, err = r.render(body); err != nil {
		return nil, fmt.Errorf("render: %w", err)
	}
	return p, nil
}

// splitFrontmatter cuts the YAML block between the leading "---" lines.
func splitFrontmatter(src []byte) (fm, body []byte, err error) {
	src = bytes.TrimPrefix(src, []byte("\ufeff"))
	first, rest, _ := bytes.Cut(src, []byte("\n"))
	if !isFence(first) {
		return nil, nil, errors.New("no frontmatter")
	}
	for off := 0; off <= maxFrontmatter; {
		line, next, found := bytes.Cut(rest[off:], []byte("\n"))
		if isFence(line) {
			return rest[:off], next, nil
		}
		if !found {
			return nil, nil, errors.New("frontmatter not closed")
		}
		off += len(line) + 1
	}
	return nil, nil, fmt.Errorf("frontmatter larger than %d bytes", maxFrontmatter)
}

func isFence(line []byte) bool { return string(bytes.TrimRight(line, " \t\r")) == "---" }

// parseDate accepts Obsidian "date" and "date & time" values. No zone means UTC.
func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006-01-02T15:04", "2006-01-02T15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("want YYYY-MM-DD or YYYY-MM-DDTHH:MM")
}

// translit maps Russian letters to Latin. An empty value drops the letter.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "yo", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "kh", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya",
}

// Slugify lowercases and transliterates s; other characters become single dashes.
// The result matches ^[a-z0-9-]{0,100}$ and may be empty.
func Slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		var t string
		if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			t = string(r)
		} else if tr, ok := translit[r]; ok {
			t = tr
		} else {
			dash = b.Len() > 0
			continue
		}
		if t == "" {
			continue
		}
		if dash {
			b.WriteByte('-')
			dash = false
		}
		b.WriteString(t)
	}
	out := b.String()
	if len(out) > 100 {
		out = strings.TrimRight(out[:100], "-")
	}
	return out
}
