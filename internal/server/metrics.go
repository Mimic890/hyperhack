package server

import (
	"bytes"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mimic890/hyperhack/internal/store"
)

const (
	topQueries  = 20              // top-N search queries exposed as gauges
	topsRefresh = 5 * time.Minute // how often the top-N lists are re-read
)

var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type histogram struct {
	counts []uint64 // per bucket, not cumulative
	sum    float64
	n      uint64
}

func (h *histogram) observe(v float64) {
	if h.counts == nil {
		h.counts = make([]uint64, len(latencyBuckets))
	}
	if i := sort.SearchFloat64s(latencyBuckets, v); i < len(latencyBuckets) {
		h.counts[i]++
	}
	h.sum += v
	h.n++
}

type reqKey struct {
	route string
	code  int
}

// registry holds the in-memory metrics. Labels are bounded: routes are mux
// patterns, codes are HTTP statuses.
type registry struct {
	mu         sync.Mutex
	reqs       map[reqKey]uint64
	dur        map[string]*histogram
	search     histogram
	queries    uint64
	zero       uint64
	tops       [2][]store.QueryCount // all, zero-result
	topsLoaded time.Time
}

func newRegistry() *registry {
	return &registry{reqs: map[reqKey]uint64{}, dur: map[string]*histogram{}}
}

func (m *registry) request(pattern string, code int, d time.Duration) {
	if pattern == "" {
		pattern = "unmatched"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reqs[reqKey{pattern, code}]++
	h := m.dur[pattern]
	if h == nil {
		h = &histogram{}
		m.dur[pattern] = h
	}
	h.observe(d.Seconds())
}

func (m *registry) searchDone(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.search.observe(d.Seconds())
}

func (m *registry) searchQuery(zero bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queries++
	if zero {
		m.zero++
	}
}

// expo writes Prometheus text format.
type expo struct{ bytes.Buffer }

func (e *expo) family(name, typ, help string) {
	fmt.Fprintf(e, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

// sample writes one line; labels are name, value pairs.
func (e *expo) sample(name string, v float64, labels ...string) {
	e.WriteString(name)
	if len(labels) > 0 {
		e.WriteByte('{')
		for i := 0; i+1 < len(labels); i += 2 {
			if i > 0 {
				e.WriteByte(',')
			}
			e.WriteString(labels[i] + `="` + escapeLabel(labels[i+1]) + `"`)
		}
		e.WriteByte('}')
	}
	e.WriteString(" " + strconv.FormatFloat(v, 'g', -1, 64) + "\n")
}

func (e *expo) histogram(name string, h *histogram, labels ...string) {
	var cum uint64
	for i, b := range latencyBuckets {
		if h.counts != nil {
			cum += h.counts[i]
		}
		e.sample(name+"_bucket", float64(cum), append(slices.Clone(labels), "le", strconv.FormatFloat(b, 'g', -1, 64))...)
	}
	e.sample(name+"_bucket", float64(h.n), append(slices.Clone(labels), "le", "+Inf")...)
	e.sample(name+"_sum", h.sum, labels...)
	e.sample(name+"_count", float64(h.n), labels...)
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }

// postFamilies maps store metrics to exposed counters and their label name.
var postFamilies = []struct{ metric, name, label, help string }{
	{store.View, "hyperhack_post_views_total", "", "Post views, one per visitor per post per day."},
	{store.ReadSecs, "hyperhack_post_read_seconds_total", "", "Visible reading time; each view's sum is clamped to 4 h."},
	{store.ReadView, "hyperhack_post_read_views_total", "", "Views that reported reading time (divisor for the average)."},
	{store.Depth, "hyperhack_post_read_depth_total", "depth", "Readers who reached a share of the text."},
	{store.Source, "hyperhack_post_source_total", "source", "Views by traffic source."},
	{store.Device, "hyperhack_post_device_total", "device", "Views by device class."},
	{store.Lang, "hyperhack_post_ui_lang_total", "lang", "Views by UI language."},
	{store.Theme, "hyperhack_post_ui_theme_total", "theme", "Views by theme."},
	{store.Outbound, "hyperhack_post_outbound_clicks_total", "host", "Clicks on external links, by host."},
	{store.Copy, "hyperhack_post_code_copies_total", "", "Copies from code blocks."},
	{store.VoteCast, "hyperhack_post_votes_total", "vote", "Likes and dislikes cast."},
}

// metricsPage handles GET /metrics: private network only (METRICS_ALLOW).
func (p *pages) metricsPage(w http.ResponseWriter, r *http.Request) {
	if !contains(p.cfg.MetricsAllow, clientIP(r, p.cfg.TrustedProxies)) {
		p.mlog.Warn("metrics denied", "reason", "address not in METRICS_ALLOW")
		apiError(w, http.StatusForbidden)
		return
	}
	ctx := r.Context()
	var e expo

	// Post metrics: only posts in the current index, so labels stay bounded.
	if ix := p.index(); ix != nil {
		totals, err := p.store.Totals(ctx)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable)
			return
		}
		for _, f := range postFamilies {
			e.family(f.name, "counter", f.help)
			for _, t := range totals {
				post := ix.Get(t.Slug)
				if t.Metric != f.metric || post == nil || (f.metric == store.Outbound && !slices.Contains(post.Outbound, t.Label)) {
					continue
				}
				if f.label == "" {
					e.sample(f.name, float64(t.N), "slug", t.Slug)
				} else {
					e.sample(f.name, float64(t.N), "slug", t.Slug, f.label, t.Label)
				}
			}
		}
		votes, err := p.store.VoteCounts(ctx)
		if err != nil {
			apiError(w, http.StatusServiceUnavailable)
			return
		}
		e.family("hyperhack_post_likes", "gauge", "Current likes.")
		for _, post := range ix.Posts() {
			e.sample("hyperhack_post_likes", float64(votes[post.Slug].Likes), "slug", post.Slug)
		}
		e.family("hyperhack_post_dislikes", "gauge", "Current dislikes.")
		for _, post := range ix.Posts() {
			e.sample("hyperhack_post_dislikes", float64(votes[post.Slug].Dislikes), "slug", post.Slug)
		}
		e.family("hyperhack_posts", "gauge", "Published posts in the index.")
		e.sample("hyperhack_posts", float64(len(ix.Posts())))
	}

	if p.syncStats != nil {
		st := p.syncStats()
		e.family("hyperhack_sync_last_attempt_timestamp_seconds", "gauge", "Start of the last sync.")
		e.sample("hyperhack_sync_last_attempt_timestamp_seconds", float64(st.LastAttempt))
		e.family("hyperhack_sync_last_success_timestamp_seconds", "gauge", "End of the last successful sync.")
		e.sample("hyperhack_sync_last_success_timestamp_seconds", float64(st.LastSuccess))
		e.family("hyperhack_sync_errors_total", "counter", "Failed syncs.")
		e.sample("hyperhack_sync_errors_total", float64(st.Errors))
		e.family("hyperhack_sync_fetch_failures_total", "counter", "Objects that failed to download.")
		e.sample("hyperhack_sync_fetch_failures_total", float64(st.FetchFailures))
		e.family("hyperhack_sync_objects", "gauge", "Keys in the last bucket listing.")
		e.sample("hyperhack_sync_objects", float64(st.Objects))
	}

	p.refreshTops(r)
	m := p.metrics
	m.mu.Lock()
	e.family("hyperhack_search_queries_total", "counter", "Settled search queries.")
	e.sample("hyperhack_search_queries_total", float64(m.queries))
	e.family("hyperhack_search_zero_results_total", "counter", "Settled search queries with no results.")
	e.sample("hyperhack_search_zero_results_total", float64(m.zero))
	e.family("hyperhack_search_duration_seconds", "histogram", "Search latency.")
	e.histogram("hyperhack_search_duration_seconds", &m.search)
	for i, name := range []string{"hyperhack_search_top_queries", "hyperhack_search_top_zero_result_queries"} {
		e.family(name, "gauge", "Top stored search queries (normalized) and their count.")
		for _, q := range m.tops[i] {
			e.sample(name, float64(q.N), "query", q.Query)
		}
	}
	e.family("hyperhack_http_requests_total", "counter", "HTTP requests by route pattern and status.")
	keys := make([]reqKey, 0, len(m.reqs))
	for k := range m.reqs {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b reqKey) int {
		return strings.Compare(a.route+strconv.Itoa(a.code), b.route+strconv.Itoa(b.code))
	})
	for _, k := range keys {
		e.sample("hyperhack_http_requests_total", float64(m.reqs[k]), "route", k.route, "code", strconv.Itoa(k.code))
	}
	e.family("hyperhack_http_request_duration_seconds", "histogram", "HTTP latency by route pattern.")
	routes := make([]string, 0, len(m.dur))
	for k := range m.dur {
		routes = append(routes, k)
	}
	slices.Sort(routes)
	for _, route := range routes {
		e.histogram("hyperhack_http_request_duration_seconds", m.dur[route], "route", route)
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(e.Bytes())
}

// refreshTops re-reads the top search queries at most every topsRefresh.
func (p *pages) refreshTops(r *http.Request) {
	m := p.metrics
	m.mu.Lock()
	fresh := time.Since(m.topsLoaded) < topsRefresh
	m.mu.Unlock()
	if fresh {
		return
	}
	all, err1 := p.store.TopSearches(r.Context(), topQueries, false)
	zero, err2 := p.store.TopSearches(r.Context(), topQueries, true)
	if err1 != nil || err2 != nil {
		return // logged by store; keep the old lists
	}
	m.mu.Lock()
	m.tops, m.topsLoaded = [2][]store.QueryCount{all, zero}, time.Now()
	m.mu.Unlock()
}
