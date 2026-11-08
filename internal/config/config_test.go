package config

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"SITE_NAME":       "Example",
		"SITE_URL":        "https://example.com/",
		"S3_ENDPOINT":     "https://s3.example.com",
		"S3_BUCKET":       "blog",
		"S3_ACCESS_KEY":   "access",
		"S3_SECRET_KEY":   "secret",
		"REINDEX_TOKEN":   strings.Repeat("r", 32),
		"ALTCHA_HMAC_KEY": strings.Repeat("a", 32),
		"VISITOR_SECRET":  strings.Repeat("v", 32),
	}
}

func load(env map[string]string) (*Config, error) {
	return Load(func(k string) string { return env[k] })
}

func TestLoadDefaults(t *testing.T) {
	c, err := load(validEnv())
	if err != nil {
		t.Fatal(err)
	}
	if c.AuthorName != "Example" || c.DefaultOGImage != "" || c.IndexNowKey != "" {
		t.Errorf("author %q, og %q, indexnow %q", c.AuthorName, c.DefaultOGImage, c.IndexNowKey)
	}
	env := validEnv()
	env["AUTHOR_NAME"], env["DEFAULT_OG_IMAGE"], env["INDEXNOW_KEY"] = "Jane", "/assets/og.png", "abcdef0123456789"
	if c2, err := load(env); err != nil || c2.AuthorName != "Jane" || c2.DefaultOGImage != "assets/og.png" || c2.IndexNowKey != "abcdef0123456789" {
		t.Errorf("custom = %+v, %v", c2, err)
	}
	if c.SiteURL.String() != "https://example.com" {
		t.Errorf("SiteURL = %s", c.SiteURL)
	}
	if c.DefaultLang != "en" || c.LogFormat != "json" || c.PageSize != 10 || c.RSSLimit != 20 {
		t.Errorf("bad defaults: %+v", c)
	}
	if c.RateVote != (Rate{10, time.Minute}) || c.RateSearch != (Rate{30, time.Minute}) {
		t.Errorf("bad rate defaults: %+v %+v", c.RateVote, c.RateSearch)
	}
	if c.MaxPostBytes != 1<<20 || c.MaxObjects != 10000 || c.AltchaExpires != 5*time.Minute {
		t.Errorf("bad limit defaults")
	}
	if !contains(c.MetricsAllow, "127.0.0.1") || !contains(c.ReindexAllow, "192.168.1.5") || contains(c.MetricsAllow, "8.8.8.8") {
		t.Errorf("bad allow defaults: %v", c.MetricsAllow)
	}
	if c.TrustedProxies != nil {
		t.Errorf("TrustedProxies default = %v, want none", c.TrustedProxies)
	}
}

func TestLoadRejectsPlaceholders(t *testing.T) {
	const long = "change-me-to-at-least-32-random-bytes"
	cases := map[string]string{
		"REINDEX_TOKEN":   long,
		"ALTCHA_HMAC_KEY": long,
		"VISITOR_SECRET":  "prefix-CHANGE-ME-suffix-with-enough-bytes",
		"S3_ACCESS_KEY":   "change-me",
		"S3_SECRET_KEY":   "change-me",
	}
	for name, value := range cases {
		env := validEnv()
		env[name] = value
		_, err := load(env)
		if err == nil || !strings.Contains(err.Error(), name+": still the placeholder") {
			t.Errorf("%s: err = %v", name, err)
			continue
		}
		if strings.Contains(err.Error(), value) {
			t.Errorf("%s: the error leaks the value: %v", name, err)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"SITE_NAME":        "",
		"SITE_URL":         "ftp://example.com",
		"S3_ENDPOINT":      "http://s3.example.com",
		"REINDEX_TOKEN":    "short",
		"ALTCHA_HMAC_KEY":  "",
		"DEFAULT_LANG":     "de",
		"LOG_LEVEL":        "loud",
		"LOG_FORMAT":       "xml",
		"POLL_INTERVAL":    "1s",
		"PAGE_SIZE":        "0",
		"RATE_LIMIT_VOTE":  "10",
		"METRICS_ALLOW":    "10.0.0.0/33",
		"GITHUB_URL":       "javascript:alert(1)",
		"DEFAULT_OG_IMAGE": "../etc/og.png",
		"INDEXNOW_KEY":     "short",
	}
	for name, val := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			env[name] = val
			_, err := load(env)
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("err = %v, want error naming %s", err, name)
			}
		})
	}
	env := validEnv()
	env["S3_ENDPOINT"] = "https://s3.example.com/blog"
	if _, err := load(env); err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT") {
		t.Errorf("endpoint with path: err = %v", err)
	}
}

func TestErrorsHideSecrets(t *testing.T) {
	env := validEnv()
	env["REINDEX_TOKEN"] = "tooshort-secret-value"
	_, err := load(env)
	if err == nil || strings.Contains(err.Error(), "tooshort-secret-value") {
		t.Fatalf("err = %v", err)
	}
}

func TestParsePrefixes(t *testing.T) {
	p, err := ParsePrefixes(" 10.0.0.1, 2001:db8::/32 ,::ffff:192.0.2.1,")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.1/32", "2001:db8::/32", "192.0.2.1/32"}
	if len(p) != len(want) {
		t.Fatalf("got %v", p)
	}
	for i := range want {
		if p[i].String() != want[i] {
			t.Errorf("p[%d] = %s, want %s", i, p[i], want[i])
		}
	}
}

func FuzzParseRate(f *testing.F) {
	for _, s := range []string{"10/1m", "1/1s", "0/1m", "5/0s", "/", "1e9/1h", "-1/1m"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r, err := ParseRate(s)
		if err == nil && (r.N < 1 || r.Per < time.Second) {
			t.Fatalf("ParseRate(%q) = %+v, want error", s, r)
		}
	})
}

func contains(ps []netip.Prefix, ip string) bool {
	a := netip.MustParseAddr(ip)
	for _, p := range ps {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
