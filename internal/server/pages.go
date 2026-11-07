package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/Mimic890/hyperhack/internal/config"
	"github.com/Mimic890/hyperhack/internal/content"
	"github.com/Mimic890/hyperhack/internal/ratelimit"
	"github.com/Mimic890/hyperhack/internal/store"
	"github.com/Mimic890/hyperhack/internal/syncer"
)

//go:embed web
var webFS embed.FS

var (
	langs  = []string{"en", "ru"}
	themes = []string{"light", "dark"}
)

const cookieMaxAge = 365 * 24 * 60 * 60

type site struct {
	Name   string
	GitHub string
	Logo   content.Logo
}

// pageData is what every template gets. Fields a page does not use stay zero.
type pageData struct {
	Lang     string // UI language
	DocLang  string // <html lang>: the post language on post pages
	Theme    string // "", "light" or "dark"
	Path     string // current URI, for the prefs form
	CSS      string
	ThemeCSS string // site or post colors from the bucket, empty for the built-in ones
	Site     site

	JS           []string // versioned script URLs for this page, deferred
	TransitionJS string   // versioned, blocking: it must run before the first frame

	TagData template.JS // tags with post counts for the search palette, from json.Marshal

	// <head> metadata. Desc, Canonical and Image are absolute or empty.
	Desc, Canonical, Image string
	OGType                 string // "website" or "article"
	NoIndex                bool
	JSONLD                 template.JS // from json.Marshal

	Posts       []*content.Post
	Page, Pages int
	PageBase    string // pager URL for page 1; other pages add ?page=N

	// Main page blocks. HiddenH1: no intro title, so the site name is the h1.
	MainBlocks, SideBlocks []homeBlock
	HiddenH1               bool

	Post   *content.Post
	Older  *content.Post // next older post, for the footer card
	Views  int64
	Votes  store.Votes
	MyVote int // store.Like, store.Dislike or store.None

	Tag   string // tag page
	Query string
	Hits  []content.Hit
	Total int

	Title, Text string // i18n keys for the error page
}

// homeBlock is one main page block, ready for the template.
type homeBlock struct {
	Type        string
	Title, Text string
	H1          bool
	Tags        []content.TagCount
	AllPosts    int
	Featured    *content.Post
	Years       []yearGroup
	Posts       []*content.Post
	NoPosts     bool // the blog has no posts at all
	Pager       *pageData
	Popular     []popularPost
}

type yearGroup struct {
	Year, Count int // Count: posts of the year in the whole list, not only on this page
	Posts       []*content.Post
}

type popularPost struct {
	*content.Post
	Views int64
	store.Votes
}

type pages struct {
	log       *slog.Logger // component http
	vlog      *slog.Logger // component votes
	mlog      *slog.Logger // component metrics
	cfg       *config.Config
	index     func() *content.Index
	store     *store.Store
	syncStats func() syncer.Stats
	tmpl      map[string]map[string]*template.Template // lang → page → template
	css       []byte
	cssV      string
	mark      []byte            // built-in mark, for /favicon.ico
	scripts   map[string]script // file name → script
	fonts     fs.FS

	origin   string // SITE_URL origin, for the Origin check
	lim      limits
	altcha   *altchaGuard
	visitors *visitors
	metrics  *registry
}

type script struct {
	body []byte
	url  string // versioned
}

type limits struct{ vote, beacon, search, reindex *ratelimit.Limiter }

// newPages builds the handlers. log is the base logger; components are added here.
func newPages(log *slog.Logger, cfg *config.Config, d Deps) *pages {
	msgs := map[string]map[string]string{}
	for _, l := range langs {
		b, err := webFS.ReadFile("web/i18n/" + l + ".json")
		if err != nil {
			panic(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			panic(fmt.Sprintf("i18n %s: %v", l, err))
		}
		msgs[l] = m
	}

	p := &pages{
		log: log.With("component", "http"), vlog: log.With("component", "votes"), mlog: log.With("component", "metrics"),
		cfg: cfg, index: d.Index, store: d.Store, syncStats: d.SyncStats,
		tmpl:   map[string]map[string]*template.Template{},
		origin: cfg.SiteURL.Scheme + "://" + cfg.SiteURL.Host,
		lim: limits{
			vote: ratelimit.New(cfg.RateVote.N, cfg.RateVote.Per), beacon: ratelimit.New(cfg.RateBeacon.N, cfg.RateBeacon.Per),
			search: ratelimit.New(cfg.RateSearch.N, cfg.RateSearch.Per), reindex: ratelimit.New(cfg.RateReindex.N, cfg.RateReindex.Per),
		},
		altcha:  newAltchaGuard(cfg.AltchaHMACKey, cfg.AltchaMaxNumber, cfg.AltchaExpires),
		metrics: newRegistry(),
	}
	p.visitors = newVisitors(cfg.VisitorSecret, func(what string) {
		p.mlog.Warn("view tracking full for today, new entries are not counted", "map", what)
	})
	for _, l := range langs {
		p.tmpl[l] = map[string]*template.Template{}
		for _, name := range []string{"home", "post", "error", "search", "tag"} {
			t := template.Must(template.New("").Funcs(funcs(msgs[l], cfg.ReadingWPM)).
				ParseFS(webFS, "web/templates/layout.html", "web/templates/"+name+".html"))
			p.tmpl[l][name] = t
		}
	}

	css, err := webFS.ReadFile("web/static/style.css")
	if err != nil {
		panic(err)
	}
	p.css = append(css, themedChromaCSS()...)
	p.cssV = versioned("/static/style.css", p.css)
	p.scripts = map[string]script{}
	for _, name := range []string{"search.js", "altcha.js", "post.js", "transition.js", "pele.js", "diagram.js"} {
		b, err := webFS.ReadFile("web/static/" + name)
		if err != nil {
			panic(err)
		}
		p.scripts[name] = script{body: b, url: versioned("/static/"+name, b)}
	}
	if p.mark, err = webFS.ReadFile("web/static/favicon.svg"); err != nil {
		panic(err)
	}
	p.fonts, err = fs.Sub(webFS, "web/static/fonts")
	if err != nil {
		panic(err)
	}
	return p
}

// pluralForm picks key_one, key_few or key_many. A language with a key_few form counts
// like Russian (1, 21 → one; 2–4, 22–24 → few; others many); the rest like English.
func pluralForm(msgs map[string]string, key string, n int) string {
	if _, slavic := msgs[key+"_few"]; !slavic {
		if n == 1 {
			return "one"
		}
		return "many"
	}
	switch d, dd := n%10, n%100; {
	case d == 1 && dd != 11:
		return "one"
	case d >= 2 && d <= 4 && (dd < 12 || dd > 14):
		return "few"
	}
	return "many"
}

// versioned adds a content hash, so the file can be cached forever.
func versioned(path string, b []byte) string {
	sum := sha256.Sum256(b)
	return path + "?v=" + hex.EncodeToString(sum[:6])
}

func funcs(msgs map[string]string, wpm int) template.FuncMap {
	t := func(key string, kv ...any) string {
		s, ok := msgs[key]
		if !ok {
			return key
		}
		for i := 0; i+1 < len(kv); i += 2 {
			s = strings.ReplaceAll(s, "{"+fmt.Sprint(kv[i])+"}", fmt.Sprint(kv[i+1]))
		}
		return s
	}
	return template.FuncMap{
		"t": t,
		"date": func(d time.Time) string {
			return t("date", "day", d.Day(), "month", t("month_"+strconv.Itoa(int(d.Month()))), "year", d.Year())
		},
		"dateShort": func(d time.Time) string {
			return t("date_short", "day", d.Day(), "month", t("month_"+strconv.Itoa(int(d.Month()))))
		},
		"plural":  func(key string, n int) string { return t(key+"_"+pluralForm(msgs, key, n), "n", n) },
		"minutes": func(words int) int { return max(1, (words+wpm-1)/wpm) },
		"asset":   assetPath,
		"add":     func(a, b int) int { return a + b },
		"tagPath": tagPath,
		"pageURL": pageURL,
	}
}

// themedChromaCSS scopes the light and dark code palettes to the effective theme,
// the same way style.css scopes its tokens.
func themedChromaCSS() []byte {
	var b bytes.Buffer
	for _, th := range []struct{ name, style, other string }{{"light", "github", "dark"}, {"dark", "github-dark", "light"}} {
		b.WriteString(chromaCSS(th.style, `:root[data-theme="`+th.name+`"]`))
		b.WriteString("@media (prefers-color-scheme: " + th.name + ") {\n")
		b.WriteString(chromaCSS(th.style, `:root:not([data-theme="`+th.other+`"])`))
		b.WriteString("}\n")
	}
	return b.Bytes()
}

// chromaCSS writes the token rules of a chroma style under scope.
// The wrapper and .bg rules are dropped: style.css sets the code block background.
func chromaCSS(style, scope string) string {
	var buf strings.Builder
	if err := chromahtml.New(chromahtml.WithClasses(true)).WriteCSS(&buf, styles.Get(style)); err != nil {
		panic(err)
	}
	var out strings.Builder
	for line := range strings.Lines(buf.String()) {
		_, rule, ok := strings.Cut(line, "*/ ")
		if !ok || !strings.HasPrefix(rule, ".chroma .") {
			continue
		}
		sel, body, _ := strings.Cut(rule, "{")
		var scoped []string
		for s := range strings.SplitSeq(sel, ",") {
			scoped = append(scoped, scope+" "+strings.TrimSpace(s))
		}
		out.WriteString(strings.Join(scoped, ", ") + " {" + body)
	}
	return out.String()
}

// pageURL is base for page 1, else base with ?page=n (base may have a query).
func pageURL(base string, n int) string {
	if n <= 1 {
		return base
	}
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	q.Set("page", strconv.Itoa(n))
	u.RawQuery = q.Encode()
	return u.String()
}

// pageParam reads ?page=: absent is 1; page 1 lives at the base URL, so "1" is not valid.
func pageParam(r *http.Request, pages int) (int, bool) {
	s := r.URL.Query().Get("page")
	if s == "" {
		return 1, true
	}
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 2 && n <= pages
}

// paginate cuts posts to one page and sets the pager. False means the page does not exist.
func (p *pages) paginate(r *http.Request, d *pageData, posts []*content.Post, base string, size int) bool {
	d.Pages = max(1, (len(posts)+size-1)/size)
	var ok bool
	if d.Page, ok = pageParam(r, d.Pages); !ok {
		return false
	}
	start := (d.Page - 1) * size
	d.Posts = posts[start:min(start+size, len(posts))]
	d.PageBase = base
	d.Canonical = p.abs(pageURL(base, d.Page))
	return true
}

func (p *pages) data(r *http.Request) pageData {
	d := pageData{
		Lang:         p.cfg.DefaultLang,
		Path:         r.URL.RequestURI(),
		CSS:          p.cssV,
		Site:         site{Name: p.cfg.SiteName, GitHub: p.cfg.GitHubURL},
		OGType:       "website",
		JS:           []string{p.scripts["search.js"].url},
		TransitionJS: p.scripts["transition.js"].url,
	}
	if p.cfg.DefaultOGImage != "" {
		d.Image = p.abs(assetPath(p.cfg.DefaultOGImage))
	}
	if c, err := r.Cookie("lang"); err == nil && slices.Contains(langs, c.Value) {
		d.Lang = c.Value
	}
	if c, err := r.Cookie("theme"); err == nil && slices.Contains(themes, c.Value) {
		d.Theme = c.Value
	}
	d.DocLang = d.Lang
	if ix := p.index(); ix != nil {
		d.Site.Logo = ix.Logo
		d.ThemeCSS = themeURL(ix.ThemeID(nil))
		if b, err := json.Marshal(ix.TopTags(len(ix.Tags()))); err == nil { // json.Marshal escapes <, > and &
			d.TagData = template.JS(b)
		}
	}
	return d
}

func (p *pages) render(w http.ResponseWriter, status int, name string, d pageData) {
	var buf bytes.Buffer
	if err := p.tmpl[d.Lang][name].ExecuteTemplate(&buf, "layout.html", d); err != nil {
		p.log.Error("template", "page", name, "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (p *pages) errorPage(w http.ResponseWriter, r *http.Request, status int) {
	d := p.data(r)
	d.Title, d.Text = "not_found", "not_found_text"
	switch status {
	case http.StatusServiceUnavailable:
		d.Title, d.Text = "not_ready", "not_ready_text"
		w.Header().Set("Retry-After", "30")
	case http.StatusTooManyRequests: // Retry-After is set by limit
		d.Title, d.Text = "too_many", "too_many_text"
	}
	p.render(w, status, "error", d)
}

func (p *pages) home(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	d := p.data(r)
	all := ix.Posts()
	isType := func(t string) func(content.Block) bool {
		return func(b content.Block) bool { return b.Type == t }
	}
	// What the posts block lists: the featured post does not repeat, on any page.
	list := all
	if slices.ContainsFunc(ix.Home, isType(content.BlockFeatured)) && len(all) > 0 {
		list = all[1:]
	}
	size := p.cfg.PageSize
	if i := slices.IndexFunc(ix.Home, isType(content.BlockPosts)); i < 0 {
		list = nil // no pagination, so /?page=2 is 404
	} else if ix.Home[i].Limit > 0 {
		size = ix.Home[i].Limit
	}
	if !p.paginate(r, &d, list, "/", size) {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	var popular []popularPost
	if d.Page == 1 && slices.ContainsFunc(ix.Home, isType(content.BlockPopular)) {
		popular = p.popular(r.Context(), ix)
	}
	d.HiddenH1 = true
	for _, b := range ix.Home {
		if d.Page > 1 && b.Type != content.BlockPosts {
			continue
		}
		hb := homeBlock{Type: b.Type}
		switch b.Type {
		case content.BlockIntro:
			hb.Title, hb.Text = b.Title.In(d.Lang), b.Text.In(d.Lang)
			if hb.Title == "" && hb.Text == "" {
				continue
			}
			if hb.Title != "" && d.HiddenH1 {
				hb.H1, d.HiddenH1 = true, false
			}
		case content.BlockTags:
			hb.Tags, hb.AllPosts = ix.TopTags(b.Limit), len(all)
			if len(hb.Tags) == 0 {
				continue
			}
		case content.BlockFeatured:
			if len(all) == 0 {
				continue
			}
			hb.Featured = all[0]
		case content.BlockPosts:
			hb.Posts, hb.NoPosts, hb.Pager = d.Posts, len(all) == 0, &d
			if b.GroupYear {
				hb.Years = yearGroups(d.Posts, list)
			}
		case content.BlockPopular:
			hb.Popular = popular[:min(b.Limit, len(popular))]
			if len(hb.Popular) == 0 {
				continue
			}
		}
		if b.Side {
			d.SideBlocks = append(d.SideBlocks, hb)
		} else {
			d.MainBlocks = append(d.MainBlocks, hb)
		}
	}
	p.render(w, http.StatusOK, "home", d)
}

// yearGroups splits one page of posts by year. Posts are newest first; list is the whole list.
func yearGroups(page, list []*content.Post) []yearGroup {
	counts := map[int]int{}
	for _, post := range list {
		counts[post.Date.Year()]++
	}
	var out []yearGroup
	for _, post := range page {
		y := post.Date.Year()
		if len(out) == 0 || out[len(out)-1].Year != y {
			out = append(out, yearGroup{Year: y, Count: counts[y]})
		}
		out[len(out)-1].Posts = append(out[len(out)-1].Posts, post)
	}
	return out
}

// popular is the posts by views in the last POPULAR_DAYS days (today included), most viewed first.
func (p *pages) popular(ctx context.Context, ix *content.Index) []popularPost {
	viewed, _ := p.store.Popular(ctx, store.Day(time.Now().AddDate(0, 0, 1-p.cfg.PopularDays))) // error logged by store
	if len(viewed) == 0 {
		return nil
	}
	votes, _ := p.store.VoteCounts(ctx) // error logged by store; counts show as 0
	var out []popularPost
	for _, v := range viewed {
		if post := ix.Get(v.Slug); post != nil {
			out = append(out, popularPost{post, v.Views, votes[v.Slug]})
		}
	}
	return out
}

// tag handles /tags/{tag...}: posts with this tag, paginated.
func (p *pages) tag(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	tag := r.PathValue("tag")
	posts := ix.Tag(tag)
	d := p.data(r)
	if len(posts) == 0 || !p.paginate(r, &d, posts, tagPath(tag), p.cfg.PageSize) {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	d.Tag = tag
	p.render(w, http.StatusOK, "tag", d)
}

// search handles GET /search?q=&tag=&page=. It works without JS; search.js swaps #results.
// tag is for old links: it becomes a #tag token in the query.
// Live requests from search.js (X-Search: live) are not stored as queries, so typing
// "s", "se", "sel" does not fill the stats; the settled query is sent once as "final".
func (p *pages) search(w http.ResponseWriter, r *http.Request) {
	if p.limit(p.lim.search, "search")(w, r) {
		return
	}
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	d := p.data(r)
	d.NoIndex = true
	q := r.URL.Query()
	d.Query = q.Get("q")
	if t := q.Get("tag"); slices.Contains(ix.Tags(), t) {
		d.Query = strings.TrimSpace("#" + t + " " + d.Query)
	}
	if rs := []rune(d.Query); len(rs) > content.MaxQueryRunes {
		d.Query = string(rs[:content.MaxQueryRunes])
	}
	var ok bool
	if d.Page, ok = pageParam(r, content.MaxSearchPage); !ok {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	size := p.cfg.PageSize
	start := time.Now()
	hits, total, err := ix.Search(d.Query, d.Page, size)
	p.metrics.searchDone(time.Since(start))
	if terms := content.Terms(d.Query); err == nil && len(terms) > 0 && d.Page == 1 && r.Header.Get("X-Search") != "live" {
		p.metrics.searchQuery(total == 0)
		_ = p.store.LogSearch(r.Context(), store.Day(time.Now()), strings.Join(terms, " "), total == 0) // error logged by store
	}
	if err != nil {
		p.log.Error("search", "err", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	d.Hits, d.Total = hits, total
	d.Pages = min(max(1, (total+size-1)/size), content.MaxSearchPage)
	if d.Page > d.Pages {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	d.PageBase = "/search?" + url.Values{"q": {d.Query}}.Encode()
	p.render(w, http.StatusOK, "search", d)
}

func (p *pages) post(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	slug, md := strings.CutSuffix(r.PathValue("slug"), ".md") // slugs never contain "."
	post := ix.Get(slug)
	if post == nil {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	if md {
		p.markdown(w, post)
		return
	}
	d := p.data(r)
	d.Post = post
	d.Older = ix.Older(post)
	d.ThemeCSS = themeURL(ix.ThemeID(post))
	d.DocLang = post.Lang
	d.OGType = "article"
	d.Desc = post.Description
	d.Canonical = p.abs("/posts/" + post.Slug)
	if post.Cover != "" {
		d.Image = p.abs(assetPath(post.Cover))
	}
	d.JSONLD = p.jsonLD(post, d.Canonical, d.Image)
	d.JS = append(d.JS, p.scripts["altcha.js"].url, p.scripts["post.js"].url)
	if post.Diagrams {
		d.JS = append(d.JS, p.scripts["pele.js"].url, p.scripts["diagram.js"].url)
	}
	ctx := r.Context()
	d.Views, _ = p.store.Views(ctx, post.Slug) // errors are logged by store; the page still renders
	d.Votes, _ = p.store.PostVotes(ctx, post.Slug)
	if vid := readVID(r); vid != "" {
		d.MyVote, _ = p.store.VoteOf(ctx, vidHash(vid), post.Slug)
	}
	p.render(w, http.StatusOK, "post", d)
}

type ldPerson struct {
	Type string `json:"@type"`
	Name string `json:"name"`
	URL  string `json:"url,omitempty"`
}

type ldPosting struct {
	Context       string   `json:"@context"`
	Type          string   `json:"@type"`
	Headline      string   `json:"headline"`
	Description   string   `json:"description,omitempty"`
	DatePublished string   `json:"datePublished"`
	DateModified  string   `json:"dateModified"`
	Author        ldPerson `json:"author"`
	Image         string   `json:"image,omitempty"`
	InLanguage    string   `json:"inLanguage"`
	Keywords      string   `json:"keywords,omitempty"`
	URL           string   `json:"url"`
	MainEntity    string   `json:"mainEntityOfPage"`
}

// jsonLD is the BlogPosting for a post. json.Marshal escapes <, > and &, so it is safe in <script>.
func (p *pages) jsonLD(post *content.Post, canonical, image string) template.JS {
	b, err := json.Marshal(ldPosting{
		Context: "https://schema.org", Type: "BlogPosting",
		Headline: post.Title, Description: post.Description,
		DatePublished: post.Date.Format(time.DateOnly), DateModified: modified(post).Format(time.DateOnly),
		Author: ldPerson{Type: "Person", Name: p.cfg.AuthorName, URL: p.cfg.GitHubURL},
		Image:  image, InLanguage: post.Lang, Keywords: strings.Join(post.Tags, ", "),
		URL: canonical, MainEntity: canonical,
	})
	if err != nil {
		panic(err) // only strings: cannot fail
	}
	return template.JS(b) // safe: json.Marshal output
}

func (p *pages) notFound(w http.ResponseWriter, r *http.Request) {
	p.errorPage(w, r, http.StatusNotFound)
}

// prefs handles the top-bar form: it sets the theme or lang cookie and goes back.
func (p *pages) prefs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	set := func(name string, allowed []string) bool {
		v := r.PostForm.Get(name)
		if v == "" {
			return true
		}
		if !slices.Contains(allowed, v) {
			return false
		}
		http.SetCookie(w, &http.Cookie{
			Name: name, Value: v, Path: "/", MaxAge: cookieMaxAge,
			HttpOnly: true, Secure: p.cfg.SiteURL.Scheme == "https", SameSite: http.SameSiteLaxMode,
		})
		return true
	}
	if !set("theme", themes) || !set("lang", langs) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, localPath(r.PostForm.Get("back")), http.StatusSeeOther)
}

// localPath returns back if it is a path on this site, else "/". This blocks open redirects.
func localPath(back string) string {
	if len(back) > 2048 || !strings.HasPrefix(back, "/") || strings.HasPrefix(back, "//") || strings.HasPrefix(back, `/\`) {
		return "/"
	}
	u, err := url.Parse(back)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	for _, c := range back {
		if c < 0x20 || c == 0x7f {
			return "/"
		}
	}
	return back
}

// assetPath is the escaped URL path of a vault asset key.
func assetPath(key string) string { return (&url.URL{Path: "/" + key}).EscapedPath() }

func (p *pages) script(w http.ResponseWriter, r *http.Request) {
	s, ok := p.scripts[r.PathValue("script")]
	if !ok {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // URL carries a content hash
	_, _ = w.Write(s.body)
}

// limit returns a check that answers 429 with Retry-After when the client is over l.
func (p *pages) limit(l *ratelimit.Limiter, endpoint string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		ok, wait := l.Allow(clientIP(r, p.cfg.TrustedProxies))
		if ok {
			return false
		}
		p.log.Warn("rate limit hit", "endpoint", endpoint) // never the client address
		w.Header().Set("Retry-After", strconv.Itoa(ratelimit.RetryAfter(wait)))
		if endpoint == "search" {
			p.errorPage(w, r, http.StatusTooManyRequests)
		} else {
			http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
		}
		return true
	}
}

func (p *pages) style(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // URL carries a content hash
	_, _ = w.Write(p.css)
}

func themeURL(id string) string {
	if id == "" {
		return ""
	}
	return "/static/theme/" + id + ".css"
}

// theme serves a stylesheet built from home.md or post colors. The ID is its content hash.
func (p *pages) theme(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	id, ok := strings.CutSuffix(r.PathValue("file"), ".css")
	var css []byte
	if ok && ix != nil {
		css, ok = ix.ThemeCSS(id)
	}
	if !ok {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(css)
}

// favicon never answers 404: it redirects to the bucket favicon when there is one, else serves the built-in mark.
func (p *pages) favicon(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=3600") // the logo can appear or go with any sync
	if ix := p.index(); ix != nil && ix.Logo.Favicon != "" {
		http.Redirect(w, r, assetPath(ix.Logo.Favicon), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	_, _ = w.Write(p.mark)
}

func (p *pages) font(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if ext := path.Ext(name); ext != ".woff2" && ext != ".txt" {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	if _, err := fs.Stat(p.fonts, name); err != nil {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=2592000")
	http.ServeFileFS(w, r, p.fonts, name)
}
