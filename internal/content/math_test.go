package content

import (
	"strings"
	"testing"
)

func TestMath(t *testing.T) {
	r := NewRenderer("en")
	for _, c := range []struct {
		name, src string
		want, not []string
	}{
		{"inline", "Euler $e^{i\\pi}$.", []string{"<p>Euler <math", "<msup><mi>e</mi>", "</math>.</p>", `display="inline"`}, nil},
		{"money stays text", "costs $5 and $10", []string{"<p>costs $5 and $10</p>"}, []string{"<math"}},
		{"space after opening", "a $ x$ b", []string{"$ x$"}, []string{"<math"}},
		{"digit after closing", "$x$5", []string{"$x$5"}, []string{"<math"}},
		{"escaped dollar", "a \\$x$ b", []string{"a $x$ b"}, []string{"<math"}},
		{"escaped inside", "$a\\$b$", []string{"<math"}, nil},
		{"code span wins", "`$x$`", []string{"<code>$x$</code>"}, []string{"<math"}},
		{"block", "$$\n\\frac{a}{b}\n$$", []string{`display="block"`, "<mfrac><mi>a</mi><mi>b</mi></mfrac>"}, []string{"<p>"}},
		{"one-line block", "$$x^2$$", []string{`display="block"`, "<msup>"}, []string{"<p>"}},
		{"block interrupts paragraph", "text\n$$\nx\n$$\nmore", []string{"<p>text</p>", `display="block"`, "<p>more</p>"}, nil},
		{"display inside a line", "a $$x$$ b", []string{"<p>a <math", `display="block"`, "</math> b</p>"}, nil},
		{"unclosed block stays text", "$$\nx\n\ny", []string{"<p>$$\nx</p>"}, []string{"<math"}},
		{"block in callout", "> [!note]\n> $$\n> \\sqrt{2}\n> $$", []string{`<blockquote class="callout callout-note">`, "<msqrt>"}, nil},
		{"bad TeX shows code", "$\\frac{$", []string{`<code class="math-error">\frac{</code>`}, []string{"<math"}},
		{"too long shows code", "$" + strings.Repeat("x", maxTeX+1) + "$", []string{`<code class="math-error">xxx`}, []string{"<math"}},
		{"color is kept", "$\\color{red}{x}$", []string{`mathcolor="red"`}, nil},
		{"no style attribute", "$\\LaTeX$", nil, []string{" style="}},
	} {
		got, err := r.render([]byte(c.src), "en")
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range c.want {
			if !strings.Contains(string(got), w) {
				t.Errorf("%s: want %q in\n%s", c.name, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(string(got), n) {
				t.Errorf("%s: unexpected %q in\n%s", c.name, n, got)
			}
		}
	}
}

func TestMathPlainText(t *testing.T) {
	p, err := NewRenderer("en").Parse("m.md", []byte("---\ntitle: T\ndate: 2026-11-03\n---\nArea $\\pi r^2$ of a circle.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := `Area \pi r^2 of a circle.`; p.Text != want {
		t.Errorf("text %q, want %q", p.Text, want)
	}
}

func FuzzMath(f *testing.F) {
	for _, s := range []string{`e^{i\pi}`, `\frac{a}{b}`, `\begin{pmatrix}1&2\\3&4\end{pmatrix}`, `\color{red}{x}`,
		`\text{<b>}`, `\ce{H2O}`, `\newcommand{\x}{y}\x`, `\left(\sum_{i=0}^n x_i\right)`, `{{{{`, `\sqrt[3]{x}`} {
		f.Add(s)
	}
	r := NewRenderer("en")
	f.Fuzz(func(t *testing.T, tex string) {
		for _, src := range []string{"$" + tex + "$", "$$\n" + tex + "\n$$"} {
			got, err := r.render([]byte(src), "en")
			if err != nil {
				t.Fatal(err)
			}
			if bad := unsafeHTML(string(got)); bad != "" {
				t.Fatalf("%s\n%s", bad, got)
			}
		}
	})
}
