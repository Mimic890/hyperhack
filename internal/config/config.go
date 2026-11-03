// Package config loads and validates settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Rate is a request limit: N requests per Per.
type Rate struct {
	N   int
	Per time.Duration
}

// Config holds all settings. Secrets are never logged; use Summary for logs.
type Config struct {
	SiteName    string
	SiteURL     *url.URL
	DefaultLang string
	GitHubURL   string

	S3Endpoint  *url.URL
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	PollInterval time.Duration

	ReindexToken  string
	AltchaHMACKey string
	VisitorSecret string

	AltchaExpires   time.Duration
	AltchaMaxNumber int64

	ReadingWPM int

	LogLevel  slog.Level
	LogFormat string

	DataDir  string
	VaultDir string

	RateVote    Rate
	RateBeacon  Rate
	RateReindex Rate
	RateSearch  Rate

	PopularDays  int
	PopularLimit int
	PageSize     int
	RSSLimit     int

	DefaultOGImage string
	IndexNowKey    string

	MetricsAllow   []netip.Prefix
	ReindexAllow   []netip.Prefix
	TrustedProxies []netip.Prefix

	MaxPostBytes  int64
	MaxAssetBytes int64
	MaxObjects    int
}

const minSecretLen = 32

// privateRanges is the default for METRICS_ALLOW and REINDEX_ALLOW: loopback and private ranges.
const privateRanges = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// Load reads the config through getenv (os.Getenv in production).
// It returns every problem at once. Errors name variables, never their values.
func Load(getenv func(string) string) (*Config, error) {
	l := loader{getenv: getenv}
	c := &Config{
		SiteName:    l.required("SITE_NAME"),
		SiteURL:     l.url("SITE_URL", true, "http", "https"),
		DefaultLang: l.enum("DEFAULT_LANG", "en", "en", "ru"),
		GitHubURL:   l.optionalURL("GITHUB_URL"),

		S3Endpoint:  l.url("S3_ENDPOINT", true, "https"),
		S3Region:    l.str("S3_REGION", "garage"),
		S3Bucket:    l.required("S3_BUCKET"),
		S3AccessKey: l.required("S3_ACCESS_KEY"),
		S3SecretKey: l.required("S3_SECRET_KEY"),

		PollInterval: l.duration("POLL_INTERVAL", "60s", 10*time.Second, 24*time.Hour),

		ReindexToken:  l.secret("REINDEX_TOKEN"),
		AltchaHMACKey: l.secret("ALTCHA_HMAC_KEY"),
		VisitorSecret: l.secret("VISITOR_SECRET"),

		AltchaExpires:   l.duration("ALTCHA_EXPIRES", "5m", 10*time.Second, time.Hour),
		AltchaMaxNumber: int64(l.int("ALTCHA_MAX_NUMBER", 100000, 1000, 10_000_000)),

		ReadingWPM: l.int("READING_WPM", 200, 50, 1000),

		LogLevel:  l.level("LOG_LEVEL"),
		LogFormat: l.enum("LOG_FORMAT", "json", "json", "text"),

		DataDir:  l.str("DATA_DIR", "/data"),
		VaultDir: l.str("VAULT_DIR", "/vault"),

		RateVote:    l.rate("RATE_LIMIT_VOTE", "10/1m"),
		RateBeacon:  l.rate("RATE_LIMIT_BEACON", "30/1m"),
		RateReindex: l.rate("RATE_LIMIT_REINDEX", "5/1m"),
		RateSearch:  l.rate("RATE_LIMIT_SEARCH", "30/1m"),

		PopularDays:  l.int("POPULAR_DAYS", 30, 1, 3650),
		PopularLimit: l.int("POPULAR_LIMIT", 5, 1, 100),
		PageSize:     l.int("PAGE_SIZE", 10, 1, 100),
		RSSLimit:     l.int("RSS_LIMIT", 20, 1, 1000),

		DefaultOGImage: l.str("DEFAULT_OG_IMAGE", ""),
		IndexNowKey:    l.str("INDEXNOW_KEY", ""),

		MetricsAllow:   l.prefixes("METRICS_ALLOW", privateRanges),
		ReindexAllow:   l.prefixes("REINDEX_ALLOW", privateRanges),
		TrustedProxies: l.prefixes("TRUSTED_PROXIES", ""),

		MaxPostBytes:  int64(l.int("MAX_POST_BYTES", 1<<20, 1024, 64<<20)),
		MaxAssetBytes: int64(l.int("MAX_ASSET_BYTES", 50<<20, 1024, 1<<30)),
		MaxObjects:    l.int("MAX_OBJECTS", 10000, 1, 1_000_000),
	}
	if err := errors.Join(l.errs...); err != nil {
		return nil, err
	}
	return c, nil
}

// Summary returns non-secret settings for the startup log.
func (c *Config) Summary() []any {
	return []any{
		"site_url", c.SiteURL.String(),
		"default_lang", c.DefaultLang,
		"s3_endpoint", c.S3Endpoint.String(),
		"poll_interval", c.PollInterval.String(),
		"log_level", c.LogLevel.String(),
		"log_format", c.LogFormat,
		"data_dir", c.DataDir,
		"vault_dir", c.VaultDir,
		"indexnow", c.IndexNowKey != "",
		"trusted_proxies", len(c.TrustedProxies),
	}
}

type loader struct {
	getenv func(string) string
	errs   []error
}

func (l *loader) fail(name, format string, a ...any) {
	l.errs = append(l.errs, fmt.Errorf("%s: "+format, append([]any{name}, a...)...))
}

func (l *loader) str(name, def string) string {
	if v := strings.TrimSpace(l.getenv(name)); v != "" {
		return v
	}
	return def
}

func (l *loader) required(name string) string {
	v := l.str(name, "")
	if v == "" {
		l.fail(name, "required")
	}
	return v
}

func (l *loader) secret(name string) string {
	v := l.getenv(name)
	if len(v) < minSecretLen {
		l.fail(name, "required, at least %d bytes", minSecretLen)
	}
	return v
}

func (l *loader) enum(name, def string, allowed ...string) string {
	v := strings.ToLower(l.str(name, def))
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	l.fail(name, "must be one of %s", strings.Join(allowed, ", "))
	return def
}

func (l *loader) int(name string, def, min, max int) int {
	s := l.str(name, "")
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		l.fail(name, "must be an integer in [%d, %d]", min, max)
		return def
	}
	return n
}

func (l *loader) duration(name, def string, min, max time.Duration) time.Duration {
	d, err := time.ParseDuration(l.str(name, def))
	if err != nil || d < min || d > max {
		l.fail(name, "must be a duration in [%s, %s]", min, max)
	}
	return d
}

func (l *loader) level(name string) slog.Level {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(l.str(name, "info"))); err != nil {
		l.fail(name, "must be one of debug, info, warn, error")
	}
	return lv
}

func (l *loader) url(name string, required bool, schemes ...string) *url.URL {
	s := l.str(name, "")
	if s == "" {
		if required {
			l.fail(name, "required")
		}
		return nil
	}
	u, err := url.Parse(strings.TrimRight(s, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		l.fail(name, "must be an absolute URL without credentials, query or fragment")
		return nil
	}
	for _, sc := range schemes {
		if u.Scheme == sc {
			return u
		}
	}
	l.fail(name, "scheme must be %s", strings.Join(schemes, " or "))
	return nil
}

func (l *loader) optionalURL(name string) string {
	if u := l.url(name, false, "https"); u != nil {
		return u.String()
	}
	return ""
}

func (l *loader) rate(name, def string) Rate {
	r, err := ParseRate(l.str(name, def))
	if err != nil {
		l.fail(name, "%v", err)
	}
	return r
}

func (l *loader) prefixes(name, def string) []netip.Prefix {
	p, err := ParsePrefixes(l.str(name, def))
	if err != nil {
		l.fail(name, "%v", err)
	}
	return p
}

// ParseRate parses "N/duration", e.g. "10/1m".
func ParseRate(s string) (Rate, error) {
	n, per, ok := strings.Cut(s, "/")
	if !ok {
		return Rate{}, errors.New(`must look like "10/1m"`)
	}
	count, err := strconv.Atoi(n)
	if err != nil || count < 1 || count > 1_000_000 {
		return Rate{}, errors.New("count must be in [1, 1000000]")
	}
	d, err := time.ParseDuration(per)
	if err != nil || d < time.Second || d > 24*time.Hour {
		return Rate{}, errors.New("period must be in [1s, 24h]")
	}
	return Rate{N: count, Per: d}, nil
}

// ParsePrefixes parses a comma-separated list of CIDRs or bare IPs. Empty input gives nil.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for f := range strings.SplitSeq(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("invalid IP %q", f)
			}
			out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", f)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
