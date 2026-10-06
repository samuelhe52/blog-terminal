package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const siteURL = "https://blog.konakona.dev"

type language string

const (
	zh language = "zh"
	en language = "en"
)

func (l language) other() language {
	if l == zh {
		return en
	}
	return zh
}

type post struct {
	Title       string    `yaml:"title"`
	Description string    `yaml:"description"`
	Date        time.Time `yaml:"date"`
	Locale      string    `yaml:"lang"`
	Slug        string    `yaml:"translationSlug"`
	Author      string    `yaml:"author"`
	Draft       bool      `yaml:"draft"`
	Lang        language  `yaml:"-"`
	Folder      string    `yaml:"-"`
	Body        string    `yaml:"-"`
	File        string    `yaml:"-"`
}

func webURL(slug string, lang language) string {
	u := url.URL{Scheme: "https", Host: "blog.konakona.dev", Path: "/" + string(lang) + "/posts/" + slug + "/"}
	return u.String()
}

type catalog struct {
	Posts []*post
	pairs map[string]map[language]*post
}

func loadCatalog(root string) (*catalog, error) {
	c := &catalog{pairs: make(map[string]map[language]*post)}
	for _, lang := range []language{zh, en} {
		dir := filepath.Join(root, string(lang))
		err := filepath.WalkDir(dir, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Ext(file) != ".md" {
				return nil
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			p, err := parsePost(data)
			if err != nil {
				return fmt.Errorf("%s: %w", file, err)
			}
			expected := "en"
			if lang == zh {
				expected = "zh-CN"
			}
			if p.Locale != expected {
				return fmt.Errorf("%s: lang %q conflicts with directory %s", file, p.Locale, lang)
			}
			if p.Draft {
				return nil
			}
			rel, err := filepath.Rel(dir, file)
			if err != nil {
				return err
			}
			p.Lang, p.File = lang, file
			p.Folder = path.Dir(filepath.ToSlash(rel))
			if p.Folder == "." {
				p.Folder = ""
			}
			if c.pairs[p.Slug] == nil {
				c.pairs[p.Slug] = make(map[language]*post)
			}
			if c.pairs[p.Slug][lang] != nil {
				return fmt.Errorf("%s: duplicate translationSlug %q for %s", file, p.Slug, lang)
			}
			c.pairs[p.Slug][lang] = p
			c.Posts = append(c.Posts, p)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.SliceStable(c.Posts, func(i, j int) bool {
		if !c.Posts[i].Date.Equal(c.Posts[j].Date) {
			return c.Posts[i].Date.After(c.Posts[j].Date)
		}
		return c.Posts[i].Slug < c.Posts[j].Slug
	})
	return c, nil
}

func parsePost(data []byte) (*post, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, fmt.Errorf("missing YAML frontmatter")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("unterminated YAML frontmatter")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data[4 : 4+end]))
	dec.KnownFields(true)
	var p post
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("frontmatter must be one YAML document")
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data[4:4+end], &node); err != nil {
		return nil, err
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("frontmatter must be a mapping")
	}
	fields := node.Content[0].Content
	for i := 0; i < len(fields); i += 2 {
		key, value := fields[i].Value, fields[i+1]
		switch key {
		case "title", "description", "lang", "translationSlug", "author":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return nil, fmt.Errorf("%s must be a string", key)
			}
		case "draft":
			if value.Tag != "!!bool" {
				return nil, fmt.Errorf("draft must be a boolean")
			}
		}
	}
	if strings.TrimSpace(p.Title) == "" || p.Date.IsZero() || (p.Locale != "zh-CN" && p.Locale != "en") || p.Slug == "" {
		return nil, fmt.Errorf("title, date, lang (zh-CN|en), and translationSlug are required")
	}
	if path.Clean(p.Slug) != p.Slug || strings.HasPrefix(p.Slug, "/") || strings.ContainsAny(p.Slug, "?#\\") || p.Slug == "." || p.Slug == ".." || strings.HasPrefix(p.Slug, "../") {
		return nil, fmt.Errorf("invalid translationSlug %q", p.Slug)
	}
	p.Body = string(data[4+end+5:])
	return &p, nil
}

func (c *catalog) resolve(slug string, preferred language) (*post, bool) {
	if p := c.pairs[slug][preferred]; p != nil {
		return p, false
	}
	p := c.pairs[slug][preferred.other()]
	return p, p != nil
}

type entry struct {
	Post   *post
	Folder string
	Count  int
}

// Home lists its own language, merging folders from both languages. Inside a
// folder, fall back only when the preferred structure is entirely empty.
func (c *catalog) listing(lang language, folder, query string) ([]entry, bool) {
	query = strings.ToLower(query)
	matches := func(p *post) bool {
		return strings.Contains(strings.ToLower(p.Title), query) || strings.Contains(strings.ToLower(p.Description), query)
	}
	// Root search spans every folder, resolving each article just once to the
	// preferred language (or its available original). Browsing stays unchanged.
	if folder == "" && query != "" {
		var results []entry
		for _, p := range c.Posts {
			resolved, _ := c.resolve(p.Slug, lang)
			if p == resolved && matches(p) {
				results = append(results, entry{Post: p})
			}
		}
		return results, false
	}
	structure := func(l language) []entry {
		var posts []entry
		counts := make(map[string]int)
		prefix := ""
		if folder != "" {
			prefix = folder + "/"
		}
		for _, p := range c.Posts {
			if p.Lang != l {
				continue
			}
			if p.Folder == folder {
				posts = append(posts, entry{Post: p})
			}
			if p.Folder != folder && strings.HasPrefix(p.Folder, prefix) {
				child := prefix + strings.Split(strings.TrimPrefix(p.Folder, prefix), "/")[0]
				// Match folders.ts: expose folders with direct posts, not
				// empty intermediate nodes. The real corpus has no such nodes.
				if child == p.Folder {
					counts[child]++
				}
			}
		}
		var folders []string
		for f := range counts {
			folders = append(folders, f)
		}
		sort.Strings(folders)
		var result []entry
		for _, f := range folders {
			result = append(result, entry{Folder: f, Count: counts[f]})
		}
		return append(result, posts...)
	}
	items := structure(lang)
	fallback := false
	if folder == "" {
		seen := make(map[string]bool)
		for _, e := range items {
			if e.Post == nil {
				seen[e.Folder] = true
			}
		}
		for _, e := range structure(lang.other()) {
			if e.Post == nil && !seen[e.Folder] {
				items = append(items, e)
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			if items[i].Post == nil && items[j].Post == nil {
				return items[i].Folder < items[j].Folder
			}
			return items[i].Post == nil && items[j].Post != nil
		})
	} else if len(items) == 0 {
		items = structure(lang.other())
		fallback = len(items) > 0
	}
	if query != "" {
		filtered := []entry{}
		for _, e := range items {
			if e.Post != nil && matches(e.Post) {
				filtered = append(filtered, e)
			}
		}
		items = filtered
	}
	return items, fallback
}
