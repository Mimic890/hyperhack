package mirror

import (
	"encoding/xml"
	"image"
	_ "image/gif" // register decoders for DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

// ImageSize returns the pixel size of a mirrored image, or 0, 0 if unknown
// (not an image, AVIF, unreadable, or an SVG without a size).
func (m *Mirror) ImageSize(key string) (w, h int) {
	f, err := m.root.Open(key)
	if err != nil {
		return 0, 0
	}
	defer func() { _ = f.Close() }()
	switch strings.ToLower(path.Ext(key)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		c, _, err := image.DecodeConfig(f)
		if err != nil {
			return 0, 0
		}
		return c.Width, c.Height
	case ".svg":
		return svgSize(io.LimitReader(f, 64<<10))
	}
	return 0, 0
}

// svgSize reads width/height of the root <svg>, else its viewBox.
func svgSize(r io.Reader) (int, int) {
	d := xml.NewDecoder(r)
	for {
		tok, err := d.Token()
		if err != nil {
			return 0, 0
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if el.Name.Local != "svg" {
			return 0, 0
		}
		var w, h float64
		var box []string
		for _, a := range el.Attr {
			switch a.Name.Local {
			case "width":
				w = svgLength(a.Value)
			case "height":
				h = svgLength(a.Value)
			case "viewBox":
				box = strings.Fields(strings.ReplaceAll(a.Value, ",", " "))
			}
		}
		if (w == 0 || h == 0) && len(box) == 4 {
			w, h = svgLength(box[2]), svgLength(box[3])
		}
		if !(w >= 1 && w <= 1e5 && h >= 1 && h <= 1e5) { // also rejects NaN
			return 0, 0
		}
		return int(math.Round(w)), int(math.Round(h))
	}
}

// svgLength parses "120" or "120px". Other units (%, em) return 0.
func svgLength(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
	if err != nil {
		return 0
	}
	return v
}
