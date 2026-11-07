package content

import (
	"path"
	"strings"
	"testing"

	"github.com/Mimic890/hyperhack/internal/mirror"
)

func TestParseSiteLogo(t *testing.T) {
	for _, c := range []struct {
		yaml     string
		want     Logo
		problems int
	}{
		{"", Logo{}, 0},
		{"logo:\n", Logo{}, 0},
		{"logo: /assets/logo.svg\n", Logo{"assets/logo.svg", "assets/logo.svg", "assets/logo.svg"}, 0},
		{"logo:\n  light: assets/a.svg\n  dark: assets/b.PNG\n  favicon: assets/f.png\n", Logo{"assets/a.svg", "assets/b.PNG", "assets/f.png"}, 0},
		{"logo:\n  dark: assets/b.svg\n  light:\n", Logo{Dark: "assets/b.svg"}, 0},
		{"logo:\n  light: notes/a.svg\n  dark: assets/v.mp4\n  favicon: assets/../x.svg\n  sepia: assets/c.svg\n", Logo{}, 4},
		{"logo:\n  light: [assets/a.svg]\n", Logo{}, 1},
		{"logo: [assets/a.svg]\n", Logo{}, 1},
		{"logo: assets/.hidden/a.svg\n", Logo{}, 1},
	} {
		got, problems := ParseSiteLogo([]byte("---\n" + c.yaml + "blocks: []\n---\n"))
		if got != c.want || len(problems) != c.problems {
			t.Errorf("%q: %+v %q, want %+v and %d problems", c.yaml, got, problems, c.want, c.problems)
		}
	}
	if _, p := ParseSiteLogo([]byte("---\nlogo:\n  favicon: x.txt\n---\n")); len(p) != 1 || !strings.Contains(p[0], "logo.favicon ignored") {
		t.Errorf("problems = %q", p)
	}
}

func TestLogoOr(t *testing.T) {
	for _, c := range []struct{ in, want Logo }{
		{Logo{}, Logo{"fb", "fb", "fb"}},
		{Logo{Light: "l"}, Logo{"l", "l", "l"}},
		{Logo{Dark: "d"}, Logo{"d", "d", "d"}},
		{Logo{Light: "l", Dark: "d"}, Logo{"l", "d", "l"}},
		{Logo{Favicon: "f"}, Logo{"fb", "fb", "f"}},
	} {
		if got := c.in.Or("fb"); got != c.want {
			t.Errorf("%+v.Or = %+v, want %+v", c.in, got, c.want)
		}
	}
	if (Logo{}).Or("") != (Logo{}) {
		t.Error("no logos must stay empty")
	}
}

func FuzzSiteLogo(f *testing.F) {
	f.Add("---\nlogo: assets/a.svg\n---\n")
	f.Add("---\nlogo:\n  light: /assets/a b.png\n  dark: ../x.svg\n  favicon: assets/.git/x.svg\n---\n")
	f.Fuzz(func(t *testing.T, src string) {
		l, _ := ParseSiteLogo([]byte(src))
		for _, k := range []string{l.Light, l.Dark, l.Favicon} {
			if k == "" {
				continue
			}
			if kind, err := mirror.Classify(k); err != nil || kind != mirror.Asset || !logoExts[strings.ToLower(path.Ext(k))] {
				t.Fatalf("bad logo key %q", k)
			}
		}
	})
}
