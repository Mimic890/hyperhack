package content

import (
	"bytes"
	"html/template"
	"net/url"
	"regexp"
	"strconv"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Renderer turns post Markdown into sanitized HTML. It is safe for concurrent use.
type Renderer struct {
	md          goldmark.Markdown
	policy      *bluemonday.Policy
	defaultLang string
	// ImageSize returns the pixel size of an assets/ key, or 0, 0. Nil means unknown.
	ImageSize func(key string) (w, h int)
}

// NewRenderer builds the Markdown pipeline. defaultLang is used for posts without lang.
func NewRenderer(defaultLang string) *Renderer {
	r := &Renderer{defaultLang: defaultLang}
	r.md = goldmark.New(
		// No goldmark.WithUnsafe: raw HTML and dangerous URLs are dropped.
		goldmark.WithExtensions(
			extension.GFM,
			highlighting.NewHighlighting(highlighting.WithFormatOptions(chromahtml.WithClasses(true))),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(linkTransformer{r}, 100)),
		),
	)
	p := bluemonday.UGCPolicy()
	p.RequireNoFollowOnLinks(false)
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^noopener noreferrer$`)).OnElements("a")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-z0-9 -]+$`)).OnElements("pre", "code", "span")
	p.AllowAttrs("width", "height").Matching(regexp.MustCompile(`^[0-9]{1,6}$`)).OnElements("img")
	p.AllowAttrs("loading").Matching(regexp.MustCompile(`^lazy$`)).OnElements("img")
	p.AllowAttrs("decoding").Matching(regexp.MustCompile(`^async$`)).OnElements("img")
	r.policy = p
	return r
}

func (r *Renderer) render(src []byte) (template.HTML, error) {
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(&headingIDs{}))
	if err := r.md.Convert(src, &buf, parser.WithContext(ctx)); err != nil {
		return "", err
	}
	return template.HTML(r.policy.SanitizeBytes(buf.Bytes())), nil // safe: sanitized above
}

// linkTransformer maps "assets/…" to "/assets/…", marks external links and sizes images.
type linkTransformer struct{ r *Renderer }

func (t linkTransformer) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			if t.r.ImageSize != nil && bytes.HasPrefix(n.Destination, []byte("assets/")) {
				if key, err := url.PathUnescape(string(n.Destination)); err == nil {
					if w, h := t.r.ImageSize(key); w > 0 {
						n.SetAttributeString("width", []byte(strconv.Itoa(w)))
						n.SetAttributeString("height", []byte(strconv.Itoa(h)))
					}
				}
			}
			n.SetAttributeString("loading", []byte("lazy"))
			n.SetAttributeString("decoding", []byte("async"))
			n.Destination = assetPath(n.Destination)
		case *ast.Link:
			n.Destination = assetPath(n.Destination)
			if isExternal(n.Destination) {
				n.SetAttributeString("rel", []byte("noopener noreferrer"))
			}
		case *ast.AutoLink:
			n.SetAttributeString("rel", []byte("noopener noreferrer"))
		}
		return ast.WalkContinue, nil
	})
}

func assetPath(dest []byte) []byte {
	if bytes.HasPrefix(dest, []byte("assets/")) {
		return append([]byte("/"), dest...)
	}
	return dest
}

func isExternal(dest []byte) bool {
	u, err := url.Parse(string(dest))
	return err == nil && u.Host != ""
}

// headingIDs makes heading ids with Slugify, so Cyrillic headings get readable ids.
type headingIDs struct{ used map[string]bool }

func (h *headingIDs) Generate(value []byte, _ ast.NodeKind) []byte {
	base := Slugify(string(value))
	if base == "" {
		base = "section"
	}
	id := base
	for i := 1; h.used[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	h.Put([]byte(id))
	return []byte(id)
}

func (h *headingIDs) Put(value []byte) {
	if h.used == nil {
		h.used = map[string]bool{}
	}
	h.used[string(value)] = true
}
