package server

import (
	"fmt"
	"io/fs"
	"maps"
	"math"
	"regexp"
	"strings"
	"testing"
)

type rgba struct{ r, g, b, a float64 }

func parseColor(t *testing.T, s string) rgba {
	t.Helper()
	var c rgba
	if strings.HasPrefix(s, "rgba(") {
		if _, err := fmt.Sscanf(s, "rgba(%f,%f,%f,%f)", &c.r, &c.g, &c.b, &c.a); err != nil {
			t.Fatalf("color %q: %v", s, err)
		}
		return c
	}
	var r, g, b int
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("color %q: %v", s, err)
	}
	return rgba{float64(r), float64(g), float64(b), 1}
}

// over composes c on an opaque background.
func (c rgba) over(bg rgba) rgba {
	mix := func(f, b float64) float64 { return f*c.a + b*(1-c.a) }
	return rgba{mix(c.r, bg.r), mix(c.g, bg.g), mix(c.b, bg.b), 1}
}

// contrast is the WCAG 2 contrast ratio.
func contrast(a, b rgba) float64 {
	lum := func(c rgba) float64 {
		ch := func(x float64) float64 {
			x /= 255
			if x <= 0.04045 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.r) + 0.7152*ch(c.g) + 0.0722*ch(c.b)
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}

// themeTokens reads the color tokens of one CSS block in style.css.
func themeTokens(t *testing.T, css, selector string) map[string]string {
	t.Helper()
	_, rest, ok := strings.Cut(css, selector+" {")
	if !ok {
		t.Fatalf("no block %q", selector)
	}
	block, _, _ := strings.Cut(rest, "}")
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(--[a-z-]+):\s*((?:#|rgba)[^;]+);`).FindAllStringSubmatch(block, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func TestTokenContrast(t *testing.T) {
	b, err := webFS.ReadFile("web/static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	dark := themeTokens(t, css, `:root[data-theme="dark"]`)
	// The dark theme is written twice: for the cookie and for the system setting.
	if sys := themeTokens(t, css, `:root:not([data-theme="light"])`); !maps.Equal(dark, sys) {
		t.Errorf("dark tokens differ: %v and %v", dark, sys)
	}
	for name, tok := range map[string]map[string]string{"light": themeTokens(t, css, ":root"), "dark": dark} {
		if len(tok) != 8 {
			t.Errorf("%s: %d color tokens, want 8: %v", name, len(tok), tok)
		}
		col := func(k string) rgba { return parseColor(t, tok[k]) }
		soft := col("--uv-soft")
		pairs := []struct {
			fg, bg string
			on     rgba
		}{
			{"--text", "--bg", col("--bg")},
			{"--text", "--raise", col("--raise")},
			{"--muted", "--bg", col("--bg")},
			{"--muted", "--raise", col("--raise")},
			{"--uv", "--bg", col("--bg")},
			{"--uv", "--raise", col("--raise")},
			{"--ember", "--raise", col("--raise")},
			{"--text", "--uv-soft on --bg", soft.over(col("--bg"))},
			{"--text", "--uv-soft on --raise", soft.over(col("--raise"))},
		}
		for _, p := range pairs {
			if r := contrast(col(p.fg), p.on); r < 4.5 {
				t.Errorf("%s: %s on %s = %.2f, want at least 4.5", name, p.fg, p.bg, r)
			}
		}
	}
	if r := contrast(rgba{0, 0, 0, 1}, rgba{255, 255, 255, 1}); math.Abs(r-21) > 0.01 {
		t.Errorf("black on white = %.2f, want 21", r)
	}
}

func TestCSSFontsExist(t *testing.T) {
	b, err := webFS.ReadFile("web/static/style.css")
	if err != nil {
		t.Fatal(err)
	}
	urls := regexp.MustCompile(`url\((fonts/[^)]+)\)`).FindAllSubmatch(b, -1)
	if len(urls) != 4 {
		t.Errorf("%d font files in style.css, want 4", len(urls))
	}
	for _, u := range urls {
		if _, err := fs.Stat(webFS, "web/static/"+string(u[1])); err != nil {
			t.Errorf("style.css refers to a missing file: %v", err)
		}
	}
}
