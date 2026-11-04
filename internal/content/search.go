package content

import (
	"html/template"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en" // register the "en" analyzer
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru" // register the "ru" analyzer
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/microcosm-cc/bluemonday"
)

// Search limits (SPEC → Input validation).
const (
	MaxQueryRunes = 200
	maxTerms      = 10
	minPrefix     = 2
	MaxSearchPage = 50
)

// analyzers are the post languages with their own analysis; other languages use "en".
var analyzers = []string{"en", "ru"}

// fields and their boosts: title > tags > description > body.
var fields = []struct {
	name  string
	boost float64
}{{"title", 4}, {"tags", 3}, {"description", 2}, {"body", 1}}

// searchDoc is what bleve indexes for a post. Type picks the language mapping.
type searchDoc struct {
	Title       string   `json:"title"`
	Tags        []string `json:"tags"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Tag         []string `json:"tag"` // exact tags for the filter
	lang        string
}

func (d searchDoc) Type() string { return d.lang }

func newSearchIndex(posts []*Post) (bleve.Index, error) {
	im := bleve.NewIndexMapping()
	for _, lang := range analyzers {
		dm := bleve.NewDocumentMapping()
		for _, f := range fields {
			fm := bleve.NewTextFieldMapping()
			fm.Analyzer = lang
			fm.Store = f.name != "tags"
			fm.IncludeTermVectors = fm.Store // for highlighting
			dm.AddFieldMappingsAt(f.name, fm)
		}
		kw := bleve.NewKeywordFieldMapping()
		kw.Store = false
		dm.AddFieldMappingsAt("tag", kw)
		im.AddDocumentMapping(lang, dm)
	}
	im.DefaultType = "en"
	im.DefaultMapping = im.TypeMapping["en"]
	ix, err := bleve.NewMemOnly(im)
	if err != nil {
		return nil, err
	}
	b := ix.NewBatch()
	for _, p := range posts {
		lang := p.Lang
		if lang != "ru" {
			lang = "en"
		}
		doc := searchDoc{Title: p.Title, Tags: p.Tags, Description: p.Description, Body: p.Text, Tag: p.Tags, lang: lang}
		if err := b.Index(p.Slug, doc); err != nil {
			return nil, err
		}
	}
	return ix, ix.Batch(b)
}

// Hit is one search result.
type Hit struct {
	Post    *Post
	Snippet template.HTML // escaped text with <mark> around matches
}

var snippetPolicy = bluemonday.NewPolicy().AllowElements("mark")

// Terms splits a query into lower-case words: at most MaxQueryRunes runes and maxTerms terms.
func Terms(q string) []string {
	if utf8.RuneCountInString(q) > MaxQueryRunes {
		q = string([]rune(q)[:MaxQueryRunes])
	}
	terms := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return terms[:min(len(terms), maxTerms)]
}

// fuzziness allows more typos in longer words, never more than 2.
func fuzziness(term string) int {
	switch n := utf8.RuneCountInString(term); {
	case n <= 3:
		return 0
	case n <= 6:
		return 1
	default:
		return 2
	}
}

// buildQuery makes match + fuzzy (+ prefix for the last term) queries in code.
// User input never reaches Bleve's query-string syntax, so no regex or wildcard queries.
func buildQuery(terms []string, tag string) query.Query {
	var all []query.Query
	for i, t := range terms {
		var anyOf []query.Query
		for _, f := range fields {
			for _, a := range analyzers {
				m := bleve.NewMatchQuery(t)
				m.SetField(f.name)
				m.Analyzer = a
				m.SetFuzziness(fuzziness(t))
				m.SetBoost(f.boost)
				anyOf = append(anyOf, m)
			}
			if i == len(terms)-1 && utf8.RuneCountInString(t) >= minPrefix {
				p := bleve.NewPrefixQuery(t)
				p.SetField(f.name)
				p.SetBoost(f.boost / 2)
				anyOf = append(anyOf, p)
			}
		}
		all = append(all, bleve.NewDisjunctionQuery(anyOf...))
	}
	if tag != "" {
		tq := bleve.NewTermQuery(tag)
		tq.SetField("tag")
		all = append(all, tq)
	}
	return bleve.NewConjunctionQuery(all...)
}

// Search finds posts for q, optionally only with tag. page starts at 1.
func (ix *Index) Search(q, tag string, page, size int) (hits []Hit, total int, err error) {
	terms := Terms(q)
	if len(terms) == 0 || page < 1 || page > MaxSearchPage {
		return nil, 0, nil
	}
	req := bleve.NewSearchRequestOptions(buildQuery(terms, tag), size, (page-1)*size, false)
	req.Highlight = bleve.NewHighlightWithStyle("html")
	req.Highlight.Fields = []string{"body", "description"}
	res, err := ix.search.Search(req)
	if err != nil {
		return nil, 0, err
	}
	for _, h := range res.Hits {
		p := ix.bySlug[h.ID]
		if p == nil {
			continue
		}
		hit := Hit{Post: p}
		for _, f := range req.Highlight.Fields {
			if fr := h.Fragments[f]; len(fr) > 0 {
				hit.Snippet = template.HTML(snippetPolicy.Sanitize(fr[0])) // safe: only <mark> survives
				break
			}
		}
		hits = append(hits, hit)
	}
	return hits, int(res.Total), nil
}
