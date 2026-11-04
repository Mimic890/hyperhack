package server

import (
	"strings"
	"testing"
)

func TestTagContrast(t *testing.T) {
	for name, c := range map[string]tagColor{"light": tagLight, "dark": tagDark} {
		for i := range tagHues {
			bg, fg := c.colors(float64(i * 360 / tagHues))
			if r := contrast(bg, fg); r < 4.5 {
				t.Errorf("%s bucket %d: contrast %.2f < 4.5 (%s on %s)", name, i, r, cssHex(fg), cssHex(bg))
			}
		}
	}
	if r := contrast([3]float64{0, 0, 0}, [3]float64{1, 1, 1}); r < 20.9 || r > 21.1 {
		t.Errorf("black on white = %.2f, want 21", r)
	}
}

func TestTagClassAndPath(t *testing.T) {
	if a, b := tagClass("go"), tagClass(strings.Clone("go")); a != b || !strings.HasPrefix(tagClass("заметки"), "t") {
		t.Error("tagClass not stable")
	}
	if got := tagPath("dev/заметки"); got != "/tags/dev/%D0%B7%D0%B0%D0%BC%D0%B5%D1%82%D0%BA%D0%B8" {
		t.Errorf("tagPath = %s", got)
	}
	css := string(tagCSS())
	if !strings.Contains(css, ".t23 {") || !strings.Contains(css, `:root[data-theme="dark"] .t0 {`) {
		t.Error("tagCSS missing buckets")
	}
}
