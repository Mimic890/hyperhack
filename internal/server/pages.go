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

	Posts       []*content.Post
	Page, Pages int

	Post *content.Post

	Title, Text string // i18n keys for the error page
}

type pages struct {
	log   *slog.Logger
	cfg   *config.Config
	index func() *content.Index
	tmpl  map[string]map[string]*template.Template // lang → page → template
	css   []byte
	cssV  string
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
		for _, name := range []string{"home", "post", "error"} {
			t := template.Must(template.New("").Funcs(funcs(msgs[l], cfg.ReadingWPM)).
				ParseFS(webFS, "web/templates/layout.html", "web/templates/"+name+".html"))
			p.tmpl[l][name] = t
		}
	}

	base, err := webFS.ReadFile("web/static/style.css")
	if err != nil {
		panic(err)
	}
	p.css = append(base, themedChromaCSS()...)
	sum := sha256.Sum256(p.css)
	p.cssV = "/static/style.css?v=" + hex.EncodeToString(sum[:6])
	p.fonts, err = fs.Sub(webFS, "web/static/fonts")
	if err != nil {
		panic(err)
	}
	return p
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
		"minutes": func(words int) int { return max(1, (words+wpm-1)/wpm) },
		"asset":   func(key string) string { return (&url.URL{Path: "/" + key}).EscapedPath() },
		"add":     func(a, b int) int { return a + b },
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

func (p *pages) data(r *http.Request) pageData {
	d := pageData{
		Lang: p.cfg.DefaultLang,
		Path: r.URL.RequestURI(),
		CSS:  p.cssV,
		Site: site{Name: p.cfg.SiteName, GitHub: p.cfg.GitHubURL},
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
	posts := ix.Posts()
	size := p.cfg.PageSize
	d := p.data(r)
	d.Pages = max(1, (len(posts)+size-1)/size)
	d.Page = 1
	if s := r.URL.Query().Get("page"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 2 || n > d.Pages { // page 1 lives at "/"
			p.errorPage(w, r, http.StatusNotFound)
			return
		}
		d.Page = n
	}
	start := (d.Page - 1) * size
	d.Posts = posts[start:min(start+size, len(posts))]
	p.render(w, http.StatusOK, "home", d)
}

func (p *pages) post(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	post := ix.Get(r.PathValue("slug"))
	if post == nil {
		p.errorPage(w, r, http.StatusNotFound)
		return
	}
	d := p.data(r)
	d.Post = post
	d.DocLang = post.Lang
	p.render(w, http.StatusOK, "post", d)
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
