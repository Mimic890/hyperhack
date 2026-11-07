package content

import "testing"

func TestParseSiteLinks(t *testing.T) {
	for _, c := range []struct {
		yaml     string
		want     Links
		problems int
	}{
		{"", Links{}, 0},
		{"github:\nx: ''\ntelegram:\n", Links{}, 0},
		{"github: https://github.com/example\nx: https://x.com/example\ntelegram: 'https://t.me/example'\n",
			Links{"https://github.com/example", "https://x.com/example", "https://t.me/example"}, 0},
		{"telegram: https://t.me/example\n", Links{Telegram: "https://t.me/example"}, 0},
		{"github: javascript:alert(1)\nx: http://x.com/a\ntelegram: t.me/example\n", Links{}, 3},
		{"github: https://user:pass@github.com/a\nx: [https://x.com]\ntelegram: 'https://'\n", Links{}, 3},
		{"x: https://x.com/\"><script>\n", Links{X: "https://x.com/%22%3E%3Cscript%3E"}, 0},
	} {
		got, problems := ParseSiteLinks([]byte("---\n" + c.yaml + "blocks: []\n---\n"))
		if got != c.want || len(problems) != c.problems {
			t.Errorf("%q: %+v %q, want %+v and %d problems", c.yaml, got, problems, c.want, c.problems)
		}
	}
}

func FuzzSiteLinks(f *testing.F) {
	f.Add("---\ntelegram: https://t.me/a\nx: javascript:x\n---\n")
	f.Fuzz(func(t *testing.T, src string) {
		l, _ := ParseSiteLinks([]byte(src))
		for _, s := range []string{l.GitHub, l.X, l.Telegram} {
			if s != "" && (len(s) < 9 || s[:8] != "https://") {
				t.Fatalf("bad link %q", s)
			}
		}
	})
}
