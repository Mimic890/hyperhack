package content

import (
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestParseSiteTheme(t *testing.T) {
	src := "---\n" + `blocks:
  - type: featured
theme:
  light:
    background: "#FFF"
    text: 1b1930
    accent-soft: "#5b3de61a"
    border:
    muted: #abc
    surface: "#fff;}body{color:red"
    shadow: "#000"
    accent: [1, 2]
  dark:
    accent: "#8b6cff"
  sepia:
    text: "#000"
` + "---\n"
	th, problems := ParseSiteTheme([]byte(src))
	wantLight := map[string]string{"bg": "#fff", "text": "#1b1930", "uv-soft": "#5b3de61a"}
	if len(th.Light) != len(wantLight) {
		t.Errorf("light = %v, want %v", th.Light, wantLight)
	}
	for k, v := range wantLight {
		if th.Light[k] != v {
			t.Errorf("light %s = %q, want %q", k, th.Light[k], v)
		}
	}
	if th.Dark["uv"] != "#8b6cff" || len(th.Dark) != 1 {
		t.Errorf("dark = %v", th.Dark)
	}
	for _, want := range []string{"light.muted ignored: put the color in quotes", "light.surface ignored", `light."shadow" ignored`, "light.accent ignored", `"sepia" ignored`} {
		if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, want) }) {
			t.Errorf("no problem %q in %q", want, problems)
		}
	}
	if len(problems) != 5 {
		t.Errorf("problems = %q", problems)
	}

	for _, src := range []string{"---\nblocks: []\n---\n", "---\ntheme:\n---\n", "no frontmatter", "---\ntheme: [\n---\n"} {
		if th, _ := ParseSiteTheme([]byte(src)); th.CSS() != nil {
			t.Errorf("%q: theme %v", src, th)
		}
	}
	if _, p := ParseSiteTheme([]byte("---\ntheme: dark\n---\n")); len(p) != 1 {
		t.Errorf("scalar theme: problems %q", p)
	}
}

func TestThemeCSS(t *testing.T) {
	site := Theme{Light: map[string]string{"bg": "#fff", "text": "#111"}, Dark: map[string]string{"bg": "#000"}}
	post := Theme{Light: map[string]string{"text": "#222", "uv": "#f00"}}
	got := string(post.Over(site).CSS())
	want := ":root { --bg: #fff; --text: #222; --uv: #f00; }\n" +
		"@media (prefers-color-scheme: dark) { :root:not([data-theme=\"light\"]) { --bg: #000; } }\n" +
		":root[data-theme=\"dark\"] { --bg: #000; }\n"
	if got != want {
		t.Errorf("CSS =\n%s\nwant\n%s", got, want)
	}
	if site.Light["text"] != "#111" {
		t.Error("Over changed the site theme")
	}
	if css := (Theme{Dark: map[string]string{"uv": "#abc"}}).CSS(); strings.Contains(string(css), ":root {") {
		t.Errorf("dark-only theme writes light rules: %s", css)
	}
	if (Theme{}).CSS() != nil {
		t.Error("empty theme has CSS")
	}
}

func TestIndexTheme(t *testing.T) {
	r := NewRenderer("en")
	parse := func(key, fm string) *Post {
		p, err := r.Parse(key, []byte("---\ntitle: T\ndate: 2026-11-01\n"+fm+"---\nbody\n"))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := parse("plain.md", "")
	own := parse("own.md", "theme:\n  dark:\n    text: \"#eee\"\n    nope: \"#000\"\n")
	if len(own.Warnings) != 1 {
		t.Errorf("warnings = %q", own.Warnings)
	}
	ix := NewIndex([]*Post{plain, own}, slog.New(slog.DiscardHandler))
	ix.SetTheme(Theme{})
	if ix.ThemeID(nil) != "" || ix.ThemeID(plain) != "" {
		t.Error("no site theme, but pages get one")
	}
	id := ix.ThemeID(own)
	if css, ok := ix.ThemeCSS(id); !ok || !strings.Contains(string(css), "--text: #eee;") {
		t.Errorf("post theme %q: %s", id, css)
	}

	ix.SetTheme(Theme{Dark: map[string]string{"bg": "#010101", "text": "#fff"}})
	site := ix.ThemeID(nil)
	if site == "" || ix.ThemeID(plain) != site || ix.ThemeID(own) == site {
		t.Errorf("site %q, plain %q, own %q", site, ix.ThemeID(plain), ix.ThemeID(own))
	}
	css, _ := ix.ThemeCSS(ix.ThemeID(own))
	if !strings.Contains(string(css), "--bg: #010101; --text: #eee;") {
		t.Errorf("post theme does not fall back to the site: %s", css)
	}
	if _, ok := ix.ThemeCSS("x"); ok {
		t.Error("unknown ID found")
	}
}

var themeCSSRe = regexp.MustCompile(`^(:root \{ (--[a-z-]+: #[0-9a-f]+; )+\}\n)?` +
	`(@media \(prefers-color-scheme: dark\) \{ :root:not\(\[data-theme="light"\]\) \{ (--[a-z-]+: #[0-9a-f]+; )+\} \}\n` +
	`:root\[data-theme="dark"\] \{ (--[a-z-]+: #[0-9a-f]+; )+\}\n)?$`)

func FuzzTheme(f *testing.F) {
	f.Add("---\ntheme:\n  light:\n    text: \"#fff\"\n---\n")
	f.Add("---\ntheme:\n  dark:\n    accent: \"#fff;}*{color:red\"\n    bg: \"</style>\"\n---\n")
	f.Add("---\ntheme: {light: {background: abc, text: [1]}, dark: null}\n---\n")
	f.Fuzz(func(t *testing.T, src string) {
		th, _ := ParseSiteTheme([]byte(src))
		for _, m := range []map[string]string{th.Light, th.Dark} {
			for k, v := range m {
				if !slices.Contains(themeOrder, k) || !strings.HasPrefix(v, "#") || !colorRe.MatchString(v) {
					t.Fatalf("bad entry %q: %q", k, v)
				}
			}
		}
		if css := th.CSS(); css != nil && !themeCSSRe.Match(css) {
			t.Fatalf("unexpected CSS: %s", css)
		}
	})
}
