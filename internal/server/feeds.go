package server

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Mimic890/hyperhack/internal/content"
)

// abs makes a site path absolute with SITE_URL.
func (p *pages) abs(path string) string { return p.cfg.SiteURL.String() + path }

// modified is the post's last change: updated, else date.
func modified(post *content.Post) time.Time {
	if post.Updated.After(post.Date) {
		return post.Updated
	}
	return post.Date
}

func newest(posts []*content.Post) time.Time {
	var t time.Time
	for _, post := range posts {
		if m := modified(post); m.After(t) {
			t = m
		}
	}
	return t
}

func writeXML(w http.ResponseWriter, ctype string, v any) {
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	if err := enc.Encode(v); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", ctype)
	_, _ = w.Write(buf.Bytes())
}

func writeText(w http.ResponseWriter, ctype, s string) {
	w.Header().Set("Content-Type", ctype)
	_, _ = w.Write([]byte(s))
}

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Atom    string     `xml:"xmlns:atom,attr"`
	Content string     `xml:"xmlns:content,attr"`
	DC      string     `xml:"xmlns:dc,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	Language      string    `xml:"language"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Self          atomLink  `xml:"atom:link"`
	Items         []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	GUID        rssGUID  `xml:"guid"`
	PubDate     string   `xml:"pubDate"`
	Updated     string   `xml:"atom:updated,omitempty"`
	Creator     string   `xml:"dc:creator,omitempty"`
	Description string   `xml:"description,omitempty"`
	Content     string   `xml:"content:encoded"`
	Categories  []string `xml:"category"`
}

type rssGUID struct {
	IsPermaLink bool   `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// absHTML makes root-relative links and images absolute, as feed readers need.
// Post HTML comes from bluemonday, which always writes double-quoted attributes.
func (p *pages) absHTML(h string) string {
	base := p.cfg.SiteURL.String()
	return strings.NewReplacer(`href="/`, `href="`+base+`/`, `src="/`, `src="`+base+`/`).Replace(h)
}

func (p *pages) rss(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	posts := ix.Posts()
	posts = posts[:min(len(posts), p.cfg.RSSLimit)]
	doc := rssDoc{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Content: "http://purl.org/rss/1.0/modules/content/",
		DC:      "http://purl.org/dc/elements/1.1/",
		Channel: rssChannel{
			Title:       p.cfg.SiteName,
			Link:        p.abs("/"),
			Description: p.cfg.SiteName,
			Language:    p.cfg.DefaultLang,
			Self:        atomLink{Href: p.abs("/rss.xml"), Rel: "self", Type: "application/rss+xml"},
		},
	}
	if t := newest(posts); !t.IsZero() {
		doc.Channel.LastBuildDate = t.Format(time.RFC1123Z)
	}
	for _, post := range posts {
		link := p.abs("/posts/" + post.Slug)
		it := rssItem{
			Title:       post.Title,
			Link:        link,
			GUID:        rssGUID{IsPermaLink: true, Value: link},
			PubDate:     post.Date.Format(time.RFC1123Z),
			Creator:     p.cfg.AuthorName,
			Description: post.Description,
			Content:     p.absHTML(string(post.HTML)),
			Categories:  post.Tags,
		}
		if !post.Updated.IsZero() {
			it.Updated = post.Updated.Format(time.RFC3339)
		}
		doc.Channel.Items = append(doc.Channel.Items, it)
	}
	writeXML(w, "application/rss+xml; charset=utf-8", doc)
}

type urlset struct {
	XMLName xml.Name     `xml:"urlset"`
	NS      string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func (p *pages) sitemap(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	day := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format("2006-01-02")
	}
	doc := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	doc.URLs = append(doc.URLs, sitemapURL{p.abs("/"), day(newest(ix.Posts()))})
	for _, post := range ix.Posts() {
		doc.URLs = append(doc.URLs, sitemapURL{p.abs("/posts/" + post.Slug), day(modified(post))})
	}
	for _, tag := range ix.Tags() {
		doc.URLs = append(doc.URLs, sitemapURL{p.abs(tagPath(tag)), day(newest(ix.Tag(tag)))})
	}
	writeXML(w, "application/xml; charset=utf-8", doc)
}

func (p *pages) robots(w http.ResponseWriter, _ *http.Request) {
	writeText(w, "text/plain; charset=utf-8", "User-agent: *\nAllow: /\n\nSitemap: "+p.abs("/sitemap.xml")+"\n")
}

// llms writes /llms.txt (llmstxt.org): the site name and links to raw Markdown posts.
func (p *pages) llms(w http.ResponseWriter, r *http.Request) {
	ix := p.index()
	if ix == nil {
		p.errorPage(w, r, http.StatusServiceUnavailable)
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n## Posts\n\n", oneLine(p.cfg.SiteName))
	for _, post := range ix.Posts() {
		fmt.Fprintf(&b, "- [%s](%s)", oneLine(post.Title), p.abs("/posts/"+post.Slug+".md"))
		if post.Description != "" {
			fmt.Fprintf(&b, ": %s", oneLine(post.Description))
		}
		b.WriteByte('\n')
	}
	writeText(w, "text/plain; charset=utf-8", b.String())
}

// oneLine keeps frontmatter text from breaking the Markdown list.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("[", "(", "]", ")").Replace(s)), " ")
}

func (p *pages) markdown(w http.ResponseWriter, post *content.Post) {
	writeText(w, "text/markdown; charset=utf-8", string(post.Source))
}
