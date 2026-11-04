package server

import (
	"bytes"
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
	Logo   bool
}

// pageData is what every template gets. Fields a page does not use stay zero.
type pageData struct {
	Lang    string // UI language
	DocLang string // <html lang>: the post language on post pages
	Theme   string // "", "light" or "dark"
	Path    string // current URI, for the prefs form
	CSS     string
	Site    site

	JS string // search.js URL; empty on pages without the script

	// <head> metadata. Desc, Canonical and Image are absolute or empty.
	Desc, Canonical, Image string
	OGType                 string // "website" or "article"
	NoIndex                bool
	JSONLD                 template.JS // from json.Marshal

	Posts       []*content.Post
	Page, Pages int
	PageBase    string // pager URL for page 1; other pages add ?page=N

	Post *content.Post

	Tag   string   // tag page, or the search filter
	Tags  []string // all tags, for the search filter
	Query string
	Hits  []content.Hit
	Total int

	Title, Text string // i18n keys for the error page
}

type pages struct {
	log   *slog.Logger
	cfg   *config.Config
	index func() *content.Index
	tmpl  map[string]map[string]*template.Template // lang → page → template
	css   []byte
	cssV  string
	js    []byte
	jsV   string
	fonts fs.FS
}

func newPages(log *slog.Logger, cfg *config.Config, index func() *content.Index) *pages {
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

	p := &pages{log: log, cfg: cfg, index: index, tmpl: map[string]map[string]*template.Template{}}
	for _, l := range langs {
		p.tmpl[l] = map[string]*template.Template{}
		for _, name := range []string{"home", "post", "error", "search", "tag"} {
			t := template.Must(template.New("").Funcs(funcs(msgs[l], cfg.ReadingWPM)).
				ParseFS(webFS, "web/templates/layout.html", "web/templates/"+name+".html"))
			p.tmpl[l][name] = t
		}
	}

	base, err := webFS.ReadFile("web/static/style.css")
	if err != nil {
		panic(err)
	}
	p.css = append(append(base, tagCSS()...), themedChromaCSS()...)
	p.cssV = versioned("/static/style.css", p.css)
	if p.js, err = webFS.ReadFile("web/static/search.js"); err != nil {
		panic(err)
	}
	p.jsV = versioned("/static/search.js", p.js)
	p.fonts, err = fs.Sub(webFS, "web/static/fonts")
	if err != nil {
		panic(err)
	}
	return p
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
		"minutes":  func(words int) int { return max(1, (words+wpm-1)/wpm) },
		"asset":    assetPath,
		"add":      func(a, b int) int { return a + b },
		"tagClass": tagClass,
		"tagPath":  tagPath,
		"pageURL":  pageURL,
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
func (p *pages) paginate(r *http.Request, d *pageData, posts []*content.Post, base string) bool {
	size := p.cfg.PageSize
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
		Lang:   p.cfg.DefaultLang,
		Path:   r.URL.RequestURI(),
		CSS:    p.cssV,
		Site:   site{Name: p.cfg.SiteName, GitHub: p.cfg.GitHubURL},
		OGType: "website",
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
	if status == http.StatusServiceUnavailable {
		d.Title, d.Text = "not_ready", "not_ready_text"
		w.Header().Set("Retry-After", "30")
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
	if !p.paginate(r, &d, ix.Posts(), "/") {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	d.Tags = ix.Tags()
	p.render(w, http.StatusOK, "home", d)
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
	if len(posts) == 0 || !p.paginate(r, &d, posts, tagPath(tag)) {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	d.Tag = tag
	p.render(w, http.StatusOK, "tag", d)
}

// search handles GET /search?q=&tag=&page=. It works without JS; search.js swaps #results.
// shortcut: no RATE_LIMIT_SEARCH yet; it comes with the rate limiter in stage 5.
func (p *pages) search(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	d := p.data(r)
	d.NoIndex, d.JS, d.Tags = true, p.jsV, ix.Tags()
	q := r.URL.Query()
	d.Query = q.Get("q")
	if rs := []rune(d.Query); len(rs) > content.MaxQueryRunes {
		d.Query = string(rs[:content.MaxQueryRunes])
	}
	if t := q.Get("tag"); slices.Contains(d.Tags, t) {
		d.Tag = t
	}
	var ok bool
	if d.Page, ok = pageParam(r, content.MaxSearchPage); !ok {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	size := p.cfg.PageSize
	hits, total, err := ix.Search(d.Query, d.Tag, d.Page, size)
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
	v := url.Values{"q": {d.Query}}
	if d.Tag != "" {
		v.Set("tag", d.Tag)
	}
	d.PageBase = "/search?" + v.Encode()
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
	d.DocLang = post.Lang
	d.OGType = "article"
	d.Desc = post.Description
	d.Canonical = p.abs("/posts/" + post.Slug)
	if post.Cover != "" {
		d.Image = p.abs(assetPath(post.Cover))
	}
	d.JSONLD = p.jsonLD(post, d.Canonical, d.Image)
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

func (p *pages) script(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // URL carries a content hash
	_, _ = w.Write(p.js)
}

func (p *pages) style(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // URL carries a content hash
	_, _ = w.Write(p.css)
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
