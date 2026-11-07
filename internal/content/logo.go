package content

import (
	"fmt"
	"path"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Mimic890/hyperhack/internal/mirror"
)

// Logo holds asset keys of the site logos. Empty means the built-in mark.
type Logo struct{ Light, Dark, Favicon string }

var logoExts = map[string]bool{".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".avif": true, ".gif": true}

// ParseSiteLogo reads logo from home.md: one path for all, or a map with light, dark
// and favicon. Bad entries are ignored and reported.
func ParseSiteLogo(src []byte) (Logo, []string) {
	fm, _, err := splitFrontmatter(src)
	if err != nil {
		return Logo{}, nil // ParseHome reports it
	}
	var f struct {
		Logo yaml.Node `yaml:"logo"`
	}
	if yaml.Unmarshal(fm, &f) != nil || f.Logo.Kind == 0 || f.Logo.ShortTag() == "!!null" {
		return Logo{}, nil
	}
	var l Logo
	var problems []string
	one := func(name string, n *yaml.Node) string {
		if n.ShortTag() == "!!null" || n.Kind == yaml.ScalarNode && strings.TrimSpace(n.Value) == "" {
			return ""
		}
		key := strings.TrimPrefix(strings.TrimSpace(n.Value), "/")
		if k, err := mirror.Classify(key); n.Kind != yaml.ScalarNode || err != nil || k != mirror.Asset || !logoExts[strings.ToLower(path.Ext(key))] {
			problems = append(problems, fmt.Sprintf("%s ignored: must be an image inside assets/ (svg, png, jpg, webp, avif, gif)", name))
			return ""
		}
		return key
	}
	switch f.Logo.Kind {
	case yaml.ScalarNode:
		k := one("logo", &f.Logo)
		l = Logo{k, k, k}
	case yaml.MappingNode:
		c := f.Logo.Content
		for i := 0; i+1 < len(c); i += 2 {
			switch name := c[i].Value; name {
			case "light":
				l.Light = one("logo.light", c[i+1])
			case "dark":
				l.Dark = one("logo.dark", c[i+1])
			case "favicon":
				l.Favicon = one("logo.favicon", c[i+1])
			default:
				problems = append(problems, fmt.Sprintf("logo.%.30q ignored: use light, dark or favicon", name))
			}
		}
	default:
		problems = append(problems, "logo ignored: must be a path or a map with light, dark and favicon")
	}
	return l, problems
}

// Or fills the gaps: a missing top bar logo takes the other one, then fallback;
// the favicon takes light, dark, then fallback.
func (l Logo) Or(fallback string) Logo {
	first := func(s ...string) string {
		for _, v := range s {
			if v != "" {
				return v
			}
		}
		return ""
	}
	return Logo{
		Light:   first(l.Light, l.Dark, fallback),
		Dark:    first(l.Dark, l.Light, fallback),
		Favicon: first(l.Favicon, l.Light, l.Dark, fallback),
	}
}
