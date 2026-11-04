package server

import (
	"fmt"
	"hash/fnv"
	"math"
	"net/url"
	"strings"
)

// tagHues is the number of tag color buckets, 15° of hue apart.
const tagHues = 24

// tagColor is one theme's chip palette: background and text lightness/saturation.
type tagColor struct{ bgS, bgL, fgS, fgL float64 }

var (
	tagLight = tagColor{0.75, 0.93, 0.75, 0.26}
	tagDark  = tagColor{0.40, 0.20, 0.75, 0.82}
)

// tagClass picks a stable color bucket from a hash of the tag name.
func tagClass(tag string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(tag))
	return fmt.Sprintf("t%d", h.Sum32()%tagHues)
}

// tagPath is the escaped URL path of a tag page. Tags may contain "/".
func tagPath(tag string) string { return (&url.URL{Path: "/tags/" + tag}).EscapedPath() }

// tagCSS writes the chip colors for every bucket, scoped per theme like style.css.
func tagCSS() []byte {
	var b strings.Builder
	rules := func(scope string, c tagColor) {
		for i := range tagHues {
			bg, fg := c.colors(float64(i * 360 / tagHues))
			fmt.Fprintf(&b, "%s.t%d { --tag-bg: %s; --tag-fg: %s; }\n", scope, i, cssHex(bg), cssHex(fg))
		}
	}
	rules("", tagLight)
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	rules(`:root:not([data-theme="light"]) `, tagDark)
	b.WriteString("}\n")
	rules(`:root[data-theme="dark"] `, tagDark)
	return []byte(b.String())
}

func (c tagColor) colors(hue float64) (bg, fg [3]float64) {
	return hsl(hue, c.bgS, c.bgL), hsl(hue, c.fgS, c.fgL)
}

func hsl(h, s, l float64) [3]float64 {
	k := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		return l - s*min(l, 1-l)*max(-1, min(k-3, 9-k, 1))
	}
	return [3]float64{k(0), k(8), k(4)}
}

func cssHex(c [3]float64) string {
	return fmt.Sprintf("#%02x%02x%02x", byte(math.Round(c[0]*255)), byte(math.Round(c[1]*255)), byte(math.Round(c[2]*255)))
}

// contrast is the WCAG 2 contrast ratio, on the 8-bit colors that reach the CSS.
func contrast(a, b [3]float64) float64 {
	lum := func(c [3]float64) float64 {
		var v [3]float64
		for i, x := range c {
			x = math.Round(x*255) / 255
			if x <= 0.04045 {
				v[i] = x / 12.92
			} else {
				v[i] = math.Pow((x+0.055)/1.055, 2.4)
			}
		}
		return 0.2126*v[0] + 0.7152*v[1] + 0.0722*v[2]
	}
	la, lb := lum(a), lum(b)
	return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)
}
