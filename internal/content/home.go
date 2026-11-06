package content

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Block types of the main page.
const (
	BlockIntro    = "intro"
	BlockTags     = "tags"
	BlockFeatured = "featured"
	BlockPosts    = "posts"
	BlockPopular  = "popular"
)

const (
	maxHomeBlocks = 20
	maxBlockTitle = 200
	maxBlockText  = 500
)

// blockSpecs lists the fields each type accepts. limitMax 0 means no limit field.
var blockSpecs = map[string]struct {
	text, group            bool
	limitMax, limitDefault int
}{
	BlockIntro:    {text: true},
	BlockTags:     {limitMax: 20, limitDefault: 6},
	BlockFeatured: {},
	BlockPosts:    {group: true, limitMax: 100},
	BlockPopular:  {limitMax: 10, limitDefault: 4},
}

// LocalText is a text in one language. Lang is empty for a plain string.
type LocalText struct{ Lang, Value string }

// Text is a block text, in file order.
type Text []LocalText

// In returns the text for lang, else the first one.
func (t Text) In(lang string) string {
	for _, e := range t {
		if e.Lang == lang {
			return e.Value
		}
	}
	if len(t) > 0 {
		return t[0].Value
	}
	return ""
}

// Block is one validated main page block.
type Block struct {
	Type        string
	Title, Text Text
	Limit       int // 0: PAGE_SIZE, for the posts block
	GroupYear   bool
	Side        bool
}

// DefaultHome is the main page without a usable home.md.
func DefaultHome() []Block {
	return []Block{
		{Type: BlockTags, Limit: 6},
		{Type: BlockFeatured},
		{Type: BlockPosts, GroupYear: true},
		{Type: BlockPopular, Limit: 4, Side: true},
	}
}

// ParseHome reads home.md and always returns blocks to show.
// The problems say what was skipped; the caller logs them as WARN.
func ParseHome(src []byte) ([]Block, []string) {
	fm, _, err := splitFrontmatter(src) // checks the size before YAML parsing
	if err != nil {
		return DefaultHome(), []string{err.Error() + ", using the default blocks"}
	}
	var f struct {
		Blocks []yaml.Node `yaml:"blocks"`
	}
	if err := yaml.Unmarshal(fm, &f); err != nil {
		return DefaultHome(), []string{"invalid YAML, using the default blocks: " + err.Error()}
	}
	if len(f.Blocks) == 0 {
		return DefaultHome(), []string{"no blocks, using the default blocks"}
	}
	var blocks []Block
	var problems []string
	hasPosts := false
	for i := range f.Blocks {
		if i == maxHomeBlocks {
			problems = append(problems, fmt.Sprintf("more than %d blocks, the rest are ignored", maxHomeBlocks))
			break
		}
		b, err := parseBlock(&f.Blocks[i])
		if err == nil && b.Type == BlockPosts && hasPosts {
			err = errors.New("only the first posts block is used")
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("block %d skipped: %v", i+1, err))
			continue
		}
		hasPosts = hasPosts || b.Type == BlockPosts
		blocks = append(blocks, b)
	}
	if len(blocks) == 0 {
		return DefaultHome(), append(problems, "no valid blocks, using the default blocks")
	}
	return blocks, problems
}

func parseBlock(n *yaml.Node) (Block, error) {
	if n.Kind != yaml.MappingNode {
		return Block{}, errors.New("not a mapping")
	}
	var b Block
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "type" {
			b.Type = n.Content[i+1].Value
		}
	}
	spec, ok := blockSpecs[b.Type]
	if !ok {
		return Block{}, fmt.Errorf("unknown type %.40q", b.Type)
	}
	b.Limit = spec.limitDefault
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i].Value, n.Content[i+1]
		var err error
		switch {
		case key == "type":
		case key == "area":
			switch {
			case val.Kind == yaml.ScalarNode && val.Value == "side":
				b.Side = true
			case val.Kind != yaml.ScalarNode || val.Value != "main":
				err = errors.New("must be main or side")
			}
		case key == "title" && spec.text:
			b.Title, err = parseText(val, maxBlockTitle)
		case key == "text" && spec.text:
			b.Text, err = parseText(val, maxBlockText)
		case key == "limit" && spec.limitMax > 0:
			b.Limit, err = parseLimit(val, spec.limitMax)
		case key == "group" && spec.group:
			switch {
			case val.Kind == yaml.ScalarNode && val.Value == "year":
				b.GroupYear = true
			case val.Kind != yaml.ScalarNode || val.Value != "none":
				err = errors.New("must be year or none")
			}
		default:
			err = errors.New("unknown field")
		}
		if err != nil {
			return Block{}, fmt.Errorf("%.40s: %w", key, err)
		}
	}
	return b, nil
}

// parseText accepts a string or a map by language. An empty Obsidian property is no text.
func parseText(n *yaml.Node, limit int) (Text, error) {
	if n.ShortTag() == "!!null" {
		return nil, nil
	}
	one := func(lang string, v *yaml.Node) (LocalText, error) {
		if v.Kind != yaml.ScalarNode || v.ShortTag() != "!!str" {
			return LocalText{}, errors.New("must be text")
		}
		s := strings.TrimSpace(v.Value)
		if utf8.RuneCountInString(s) > limit {
			return LocalText{}, fmt.Errorf("longer than %d characters", limit)
		}
		return LocalText{lang, s}, nil
	}
	if n.Kind != yaml.MappingNode {
		e, err := one("", n)
		return Text{e}, err
	}
	var t Text
	for i := 0; i+1 < len(n.Content); i += 2 {
		lang := n.Content[i].Value
		if !langRe.MatchString(lang) {
			return nil, errors.New("language must be two lower-case letters")
		}
		e, err := one(lang, n.Content[i+1])
		if err != nil {
			return nil, err
		}
		t = append(t, e)
	}
	return t, nil
}

func parseLimit(n *yaml.Node, limitMax int) (int, error) {
	v, err := strconv.Atoi(n.Value)
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" || err != nil || v < 1 || v > limitMax {
		return 0, fmt.Errorf("must be from 1 to %d", limitMax)
	}
	return v, nil
}
