package mirror

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestImageSize(t *testing.T) {
	m, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 640, 360)))
	files := map[string]string{
		"assets/a.png":    buf.String(),
		"assets/b.svg":    `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="120px" height="80">`,
		"assets/c.svg":    `<svg viewBox="0 0 1200 630"></svg>`,
		"assets/d.svg":    `<svg width="100%" height="50%"></svg>`,
		"assets/e.svg":    `<svg width="NaN" height="Inf"></svg>`,
		"assets/f.svg":    `<html><svg width="10" height="10"></svg></html>`,
		"assets/bad.png":  "not a png",
		"assets/clip.mp4": "x",
	}
	for k, v := range files {
		if err := m.Write(k, "e", strings.NewReader(v), 1<<20); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string][2]int{
		"assets/a.png": {640, 360}, "assets/b.svg": {120, 80}, "assets/c.svg": {1200, 630},
		"assets/d.svg": {0, 0}, "assets/e.svg": {0, 0}, "assets/f.svg": {0, 0},
		"assets/bad.png": {0, 0}, "assets/clip.mp4": {0, 0}, "assets/missing.png": {0, 0},
	}
	for k, wh := range want {
		if w, h := m.ImageSize(k); w != wh[0] || h != wh[1] {
			t.Errorf("ImageSize(%s) = %d×%d, want %d×%d", k, w, h, wh[0], wh[1])
		}
	}
}

func FuzzSVGSize(f *testing.F) {
	for _, s := range []string{`<svg width="1" height="2">`, `<svg viewBox="0,0,3,4"/>`, `<svg width="1e309" height="-1">`, "<"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		w, h := svgSize(strings.NewReader(s))
		if (w == 0) != (h == 0) || w < 0 || w > 1e5 || h < 0 || h > 1e5 {
			t.Fatalf("svgSize(%q) = %d×%d", s, w, h)
		}
	})
}
