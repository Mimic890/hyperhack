package content

import (
	"bytes"
	stdhtml "html"
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
			mathExtension{},
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
				highlighting.WithWrapperRenderer(codeBlockWrapper),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(linkTransformer{r}, 100), util.Prioritized(blockTransformer{}, 200)),
		),
	)
	p := bluemonday.UGCPolicy()
	p.RequireNoFollowOnLinks(false)
	p.AllowAttrs("rel").Matching(regexp.MustCompile(`^noopener noreferrer$`)).OnElements("a")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-z0-9 -]+$`)).OnElements("pre", "code", "span")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^code-(block|head)$`)).OnElements("div")
	p.AllowAttrs("data-lang").Matching(regexp.MustCompile(`^[A-Za-z0-9_+#.-]{1,40}$`)).OnElements("div")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^callout callout-(note|tip|warning)$`)).OnElements("blockquote")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^callout-title$`)).OnElements("p")
	p.AllowAttrs("width", "height").Matching(regexp.MustCompile(`^[0-9]{1,6}$`)).OnElements("img")
	p.AllowAttrs("loading").Matching(regexp.MustCompile(`^lazy$`)).OnElements("img")
	p.AllowAttrs("decoding").Matching(regexp.MustCompile(`^async$`)).OnElements("img")
	allowMathML(p)
	r.policy = p
	return r
}

// render converts a post body. lang is the post language, for default callout titles.
func (r *Renderer) render(src []byte, lang string) (template.HTML, error) {
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(&headingIDs{}))
	ctx.Set(postLangKey, lang)
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

var postLangKey = parser.NewContextKey()

// calloutRe matches the first line of a callout, with an optional title after the type.
var calloutRe = regexp.MustCompile(`(?i)^\[!(note|tip|warning)\](?:[ \t]|$)`)

var calloutTitles = map[string]struct{ en, ru string }{
	"note":    {"Note", "Заметка"},
	"tip":     {"Tip", "Совет"},
	"warning": {"Warning", "Внимание"},
}

// blockTransformer makes callouts from blockquotes and gives indented code the code block header.
type blockTransformer struct{}

func (blockTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	var quotes []*ast.Blockquote
	var code []*ast.CodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			switch n := n.(type) {
			case *ast.Blockquote:
				quotes = append(quotes, n)
			case *ast.CodeBlock:
				code = append(code, n)
			}
		}
		return ast.WalkContinue, nil
	})
	lang, _ := pc.Get(postLangKey).(string)
	for _, q := range quotes {
		makeCallout(q, reader.Source(), lang)
	}
	for _, c := range code {
		f := ast.NewFencedCodeBlock(nil)
		f.SetLines(c.Lines())
		c.Parent().ReplaceChild(c.Parent(), c, f)
	}
}

// makeCallout turns a blockquote into a callout if its first line is "[!type] title".
// Anything else stays a plain blockquote.
func makeCallout(q *ast.Blockquote, source []byte, lang string) {
	para, ok := q.FirstChild().(*ast.Paragraph)
	if !ok || para.Lines().Len() == 0 {
		return
	}
	line := para.Lines().At(0)
	m := calloutRe.FindSubmatchIndex(bytes.TrimRight(line.Value(source), "\r\n"))
	if m == nil {
		return
	}
	// A reference definition can make the marker a link: then it is not a callout.
	if t, ok := para.FirstChild().(*ast.Text); !ok || t.Segment.Start != line.Start {
		return
	}
	kind := string(bytes.ToLower(line.Value(source)[m[2]:m[3]]))
	markerEnd := line.Start + m[3] + 1

	// Everything up to the first line break moves to the title, with its inline markup.
	title := ast.NewParagraph()
	title.SetAttributeString("class", []byte("callout-title"))
	for c := para.FirstChild(); c != nil; {
		next := c.NextSibling()
		para.RemoveChild(para, c)
		t, isText := c.(*ast.Text)
		if !isText {
			title.AppendChild(title, c)
			c = next
			continue
		}
		lineEnd := t.SoftLineBreak() || t.HardLineBreak()
		t.SetSoftLineBreak(false)
		t.SetHardLineBreak(false)
		if t.Segment.Start <= markerEnd {
			t.Segment = t.Segment.WithStart(min(markerEnd, t.Segment.Stop))
			t.Segment = t.Segment.TrimLeftSpace(source)
		}
		if t.Segment.Len() > 0 {
			title.AppendChild(title, c)
		}
		if lineEnd {
			break
		}
		c = next
	}
	if !title.HasChildren() {
		name := calloutTitles[kind].en
		if lang == "ru" {
			name = calloutTitles[kind].ru
		}
		title.AppendChild(title, ast.NewString([]byte(name)))
	}
	q.InsertBefore(q, para, title)
	if !para.HasChildren() {
		q.RemoveChild(q, para)
	}
	q.SetAttributeString("class", []byte("callout callout-"+kind))
}

// codeBlockWrapper adds the header above a code block. The language is an attribute,
// shown by CSS, so it stays out of the search text and RSS. Unknown languages arrive
// unhighlighted, without the <pre> that chroma writes.
func codeBlockWrapper(w util.BufWriter, c highlighting.CodeBlockContext, entering bool) {
	if !entering {
		if !c.Highlighted() {
			_, _ = w.WriteString("</code></pre>")
		}
		_, _ = w.WriteString("</div>\n")
		return
	}
	_, _ = w.WriteString(`<div class="code-block"><div class="code-head"`)
	if lang, ok := c.Language(); ok {
		_, _ = w.WriteString(` data-lang="` + stdhtml.EscapeString(string(lang)) + `"`)
	}
	_, _ = w.WriteString("></div>")
	if !c.Highlighted() {
		_, _ = w.WriteString("<pre><code>")
	}
}

// MathML from the TeX converter. Only presentation elements and plain attributes:
// no href, style, event handlers or annotation-xml, which can carry HTML.
var mathElements = []string{"math", "semantics", "mrow", "mi", "mn", "mo", "mtext", "mspace",
	"mpadded", "merror", "mover", "munder", "munderover", "mfrac", "msup", "msub", "msubsup",
	"msqrt", "mroot", "mstyle", "mtable", "mtr", "mtd", "mlabeledtr", "menclose",
	"mmultiscripts", "mprescripts", "none"}

func allowMathML(p *bluemonday.Policy) {
	p.AllowNoAttrs().OnElements(mathElements...)
	p.AllowNoAttrs().OnElements("annotation") // the TeX source; browsers do not show it
	p.AllowAttrs("encoding").Matching(regexp.MustCompile(`^application/x-tex$`)).OnElements("annotation")
	p.AllowAttrs("xmlns").Matching(regexp.MustCompile(`^http://www\.w3\.org/1998/Math/MathML$`)).OnElements("math")
	p.AllowAttrs("display").Matching(regexp.MustCompile(`^(block|inline)$`)).OnElements("math")
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^math-(display|text)style$`)).OnElements("math")
	p.AllowAttrs("displaystyle", "accent", "fence", "largeop", "movablelimits", "stretchy", "symmetric").
		Matching(regexp.MustCompile(`^(true|false)$`)).OnElements(mathElements...)
	p.AllowAttrs("width", "height", "lspace", "rspace", "voffset", "minsize", "linethickness", "mathsize").
		Matching(regexp.MustCompile(`^-?[0-9.]{1,10}(em|ex|px|pt|%)?$`)).OnElements(mathElements...)
	p.AllowAttrs("scriptlevel").Matching(regexp.MustCompile(`^[+-]?[0-9]{1,2}$`)).OnElements(mathElements...)
	p.AllowAttrs("mathvariant").Matching(regexp.MustCompile(`^[a-z-]{1,30}$`)).OnElements(mathElements...)
	p.AllowAttrs("form").Matching(regexp.MustCompile(`^(prefix|infix|postfix)$`)).OnElements("mo")
	p.AllowAttrs("columnalign", "rowalign", "columnlines", "rowspacing", "columnspacing", "notation").
		Matching(regexp.MustCompile(`^[a-z0-9. ]{1,100}$`)).OnElements(mathElements...)
	p.AllowAttrs("rowspan", "columnspan").Matching(regexp.MustCompile(`^[0-9]{1,3}$`)).OnElements("mtd")
	p.AllowAttrs("mathcolor").Matching(regexp.MustCompile(`^(#[0-9a-fA-F]{3,8}|[a-zA-Z]{1,20})$`)).OnElements(mathElements...)
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
