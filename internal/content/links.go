package content

import (
	"net/url"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Links are the social links of the top bar. An empty one has no button.
type Links struct{ GitHub, X, Telegram string }

const maxLinkLen = 500

// ParseSiteLinks reads github, x and telegram from home.md. Bad values are ignored and reported.
func ParseSiteLinks(src []byte) (Links, []string) {
	fm, _, err := splitFrontmatter(src)
	if err != nil {
		return Links{}, nil // ParseHome reports it
	}
	var f struct {
		GitHub   yaml.Node `yaml:"github"`
		X        yaml.Node `yaml:"x"`
		Telegram yaml.Node `yaml:"telegram"`
	}
	if yaml.Unmarshal(fm, &f) != nil {
		return Links{}, nil
	}
	var problems []string
	one := func(name string, n *yaml.Node) string {
		if n.Kind == 0 || n.ShortTag() == "!!null" || n.Kind == yaml.ScalarNode && strings.TrimSpace(n.Value) == "" {
			return ""
		}
		if u, ok := parseLink(n); ok {
			return u
		}
		problems = append(problems, name+" ignored: must be an https link like https://example.com/name")
		return ""
	}
	l := Links{GitHub: one("github", &f.GitHub), X: one("x", &f.X), Telegram: one("telegram", &f.Telegram)}
	return l, problems
}

// parseLink accepts an absolute https URL with a host and no credentials.
func parseLink(n *yaml.Node) (string, bool) {
	s := strings.TrimSpace(n.Value)
	if n.Kind != yaml.ScalarNode || len(s) > maxLinkLen {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return "", false
	}
	return u.String(), true
}
