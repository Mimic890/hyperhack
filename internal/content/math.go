package content

import (
	"bytes"
	stdhtml "html"
	"strings"

	"github.com/wyatt915/treeblood"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Math follows Obsidian: $…$ is inline math, $$…$$ is display math. The TeX
// becomes MathML on the server, so formulas need no script and work in RSS.

// maxTeX bounds one formula. The converter is recursive, so this also bounds its depth.
const maxTeX = 4096

var (
	kindMath      = ast.NewNodeKind("Math")
	kindMathBlock = ast.NewNodeKind("MathBlock")
)

type mathInline struct {
	ast.BaseInline
	tex     text.Segment
	display bool
}

func (n *mathInline) Kind() ast.NodeKind            { return kindMath }
func (n *mathInline) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type mathBlock struct {
	ast.BaseBlock
	closed bool // a one-line $$…$$ block
}

func (n *mathBlock) Kind() ast.NodeKind            { return kindMathBlock }
func (n *mathBlock) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

// mathExtension adds the math parsers and the MathML renderer.
type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 750)),
		parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 150)),
	)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(mathRenderer{}, 100)))
}

var dollars = []byte("$$")

// mathBlockParser reads display math that starts a line with $$. It opens only
// when a closing $$ follows, so a lone $$ stays text.
type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || !bytes.HasPrefix(line[pos:], dollars) {
		return nil, parser.NoChildren
	}
	start := seg.Start + pos + 2
	body := line[pos+2:]
	node := &mathBlock{}
	if i := bytes.Index(body, dollars); i >= 0 {
		// $$…$$ on one line is a block only when nothing follows it.
		if len(util.TrimRightSpace(body[i+2:])) > 0 {
			return nil, parser.NoChildren
		}
		node.Lines().Append(text.NewSegment(start, start+i))
		node.closed = true
		reader.AdvanceToEOL()
		return node, parser.NoChildren
	}
	if !closerAhead(reader) {
		return nil, parser.NoChildren
	}
	node.Lines().Append(text.NewSegment(start, seg.Stop))
	reader.AdvanceToEOL()
	return node, parser.NoChildren
}

// closerAhead reports whether a later line holds $$. The scan stops at the first
// such line, and that line closes the block, so scans never overlap.
func closerAhead(reader text.Reader) bool {
	line, seg := reader.Position()
	defer reader.SetPosition(line, seg)
	for {
		reader.AdvanceLine()
		next, _ := reader.PeekLine()
		if next == nil {
			return false
		}
		if bytes.Contains(next, dollars) {
			return true
		}
	}
}

func (mathBlockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	if node.(*mathBlock).closed {
		return parser.Close
	}
	line, seg := reader.PeekLine()
	if line == nil {
		return parser.Close
	}
	if i := bytes.Index(line, dollars); i >= 0 {
		node.Lines().Append(text.NewSegment(seg.Start, seg.Start+i))
		reader.AdvanceToEOL()
		return parser.Close
	}
	node.Lines().Append(seg)
	reader.AdvanceToEOL()
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(ast.Node, text.Reader, parser.Context) {}
func (mathBlockParser) CanInterruptParagraph() bool                 { return true }
func (mathBlockParser) CanAcceptIndentedLine() bool                 { return false }

// mathInlineParser reads $…$ and $$…$$ inside a line. As in Obsidian and Pandoc,
// the opening $ is not followed by a space and the closing $ is not preceded by
// a space or followed by a digit, so "$5 and $10" stays text.
type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$'} }

func (mathInlineParser) Parse(_ ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, seg := block.PeekLine()
	if bytes.HasPrefix(line, dollars) {
		i := closingDollar(line[2:], true)
		if i <= 0 {
			return nil
		}
		block.Advance(i + 4)
		return &mathInline{tex: text.NewSegment(seg.Start+2, seg.Start+2+i), display: true}
	}
	if len(line) < 2 || util.IsSpace(line[1]) {
		return nil
	}
	i := closingDollar(line[1:], false)
	if i <= 0 {
		return nil
	}
	block.Advance(i + 2)
	return &mathInline{tex: text.NewSegment(seg.Start+1, seg.Start+1+i)}
}

// closingDollar returns the index of the closing $ (or $$) in s, or -1.
// A backslash escapes the next byte.
func closingDollar(s []byte, double bool) int {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\':
			i++
		case s[i] != '$':
		case double:
			if i+1 < len(s) && s[i+1] == '$' {
				return i
			}
			return -1
		case i > 0 && !util.IsSpace(s[i-1]) && (i+1 == len(s) || !isDigit(s[i+1])):
			return i
		}
	}
	return -1
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

type mathRenderer struct{}

func (mathRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMath, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			m := n.(*mathInline)
			writeMath(w, m.tex.Value(source), m.display)
		}
		return ast.WalkSkipChildren, nil
	})
	reg.Register(kindMathBlock, func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			writeMath(w, n.Lines().Value(source), true)
			_ = w.WriteByte('\n')
		}
		return ast.WalkSkipChildren, nil
	})
}

// writeMath writes the formula as MathML. TeX that fails to convert is shown as code.
// The output goes through the sanitizer like the rest of the post.
func writeMath(w util.BufWriter, tex []byte, display bool) {
	tex = bytes.TrimSpace(tex)
	if len(tex) > 0 && len(tex) <= maxTeX {
		doc := treeblood.NewDocument(nil, false) // per call: a document holds parse state
		doc.PrintOneLine = true                  // no line breaks: they would put a space before "." after inline math
		var out string
		var err error
		if display {
			out, err = doc.DisplayStyle(string(tex))
		} else {
			out, err = doc.TextStyle(string(tex))
		}
		if err == nil && out != "" {
			_, _ = w.WriteString(strings.TrimSpace(out))
			return
		}
	}
	_, _ = w.WriteString(`<code class="math-error">` + stdhtml.EscapeString(string(tex)) + `</code>`)
}
