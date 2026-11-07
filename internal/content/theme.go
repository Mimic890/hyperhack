package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Theme holds color overrides for the light and dark themes: CSS token → color.
// The zero Theme changes nothing.
type Theme struct{ Light, Dark map[string]string }

// themeNames maps the names used in home.md and posts to the CSS tokens of style.css.
var themeNames = map[string]string{
	"background":  "bg",
	"surface":     "raise",
	"border":      "line",
	"text":        "text",
	"muted":       "muted",
	"accent":      "uv",
	"accent-2":    "ember",
	"accent-soft": "uv-soft",
}

// themeOrder keeps the CSS output stable.
var themeOrder = []string{"bg", "raise", "line", "text", "muted", "uv", "ember", "uv-soft"}

// Only hex colors, so nothing else can reach the CSS. The # is optional:
// in YAML an unquoted #fff is a comment.
var colorRe = regexp.MustCompile(`^#?(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)

// parseTheme reads a theme mapping. Bad entries are ignored and reported, never fatal.
func parseTheme(n *yaml.Node) (Theme, []string) {
	var t Theme
	var problems []string
	if n.ShortTag() == "!!null" {
		return t, nil
	}
	if n.Kind != yaml.MappingNode {
		return t, []string{"theme ignored: must be a map with light and dark"}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		mode, colors := n.Content[i].Value, n.Content[i+1]
		if mode != "light" && mode != "dark" {
			problems = append(problems, fmt.Sprintf("theme: %.20q ignored: must be light or dark", mode))
			continue
		}
		if colors.ShortTag() == "!!null" {
			continue
		}
		if colors.Kind != yaml.MappingNode {
			problems = append(problems, fmt.Sprintf("theme.%s ignored: must be a map of colors", mode))
			continue
		}
		m := map[string]string{}
		for j := 0; j+1 < len(colors.Content); j += 2 {
			k, v := colors.Content[j], colors.Content[j+1]
			token, ok := themeNames[k.Value]
			value := strings.TrimSpace(v.Value)
			switch {
			case !ok:
				problems = append(problems, fmt.Sprintf("theme.%s.%.30q ignored: unknown color name", mode, k.Value))
			case v.ShortTag() == "!!null" && colorRe.MatchString(k.LineComment):
				problems = append(problems, fmt.Sprintf("theme.%s.%s ignored: put the color in quotes, like \"#1b1930\"", mode, k.Value))
			case v.ShortTag() == "!!null" || v.Kind == yaml.ScalarNode && value == "":
			case v.Kind != yaml.ScalarNode || !colorRe.MatchString(value):
				problems = append(problems, fmt.Sprintf("theme.%s.%s ignored: must be a hex color like \"#1b1930\"", mode, k.Value))
			default:
				m[token] = "#" + strings.ToLower(strings.TrimPrefix(value, "#"))
			}
		}
		if len(m) > 0 {
			if mode == "light" {
				t.Light = m
			} else {
				t.Dark = m
			}
		}
	}
	return t, problems
}

// Over returns t with base underneath: colors t does not set come from base.
func (t Theme) Over(base Theme) Theme {
	merge := func(top, bottom map[string]string) map[string]string {
		if len(top) == 0 {
			return bottom
		}
		out := map[string]string{}
		for k, v := range bottom {
			out[k] = v
		}
		for k, v := range top {
			out[k] = v
		}
		return out
	}
	return Theme{Light: merge(t.Light, base.Light), Dark: merge(t.Dark, base.Dark)}
}

// CSS returns a stylesheet that overrides the tokens of style.css, with the same
// selectors, so it must load after it. Nil for an empty theme.
func (t Theme) CSS() []byte {
	decls := func(m map[string]string) string {
		var b strings.Builder
		for _, k := range themeOrder {
			if v, ok := m[k]; ok {
				b.WriteString("--" + k + ": " + v + "; ")
			}
		}
		return b.String()
	}
	light, dark := decls(t.Light), decls(t.Dark)
	if light == "" && dark == "" {
		return nil
	}
	var b strings.Builder
	if light != "" {
		b.WriteString(":root { " + light + "}\n")
	}
	if dark != "" {
		b.WriteString("@media (prefers-color-scheme: dark) { :root:not([data-theme=\"light\"]) { " + dark + "} }\n")
		b.WriteString(":root[data-theme=\"dark\"] { " + dark + "}\n")
	}
	return []byte(b.String())
}

// themeID names a stylesheet by its content.
func themeID(css []byte) string {
	sum := sha256.Sum256(css)
	return hex.EncodeToString(sum[:8])
}

// ParseSiteTheme reads the theme from home.md. Problems are for WARN logs.
func ParseSiteTheme(src []byte) (Theme, []string) {
	fm, _, err := splitFrontmatter(src)
	if err != nil {
		return Theme{}, nil // ParseHome reports it
	}
	var f struct {
		Theme yaml.Node `yaml:"theme"`
	}
	if yaml.Unmarshal(fm, &f) != nil || f.Theme.Kind == 0 {
		return Theme{}, nil
	}
	return parseTheme(&f.Theme)
}
