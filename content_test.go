package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func fixtureCatalog(t *testing.T) *catalog {
	t.Helper()
	c, err := loadCatalog("testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func fixture(t *testing.T, root, file, lang, slug string, draft bool) {
	t.Helper()
	full := filepath.Join(root, file)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf("---\ntitle: Example\ndescription: Summary\ndate: 2026-01-02\nlang: %s\ntranslationSlug: %s\ndraft: %t\n---\nBody\n", lang, slug, draft)
	if err := os.WriteFile(full, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLoader(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "zh/actual/folder/a.md", "zh-CN", "different/slug", false)
	fixture(t, root, "en/english/path/b.md", "en", "different/slug", false)
	fixture(t, root, "en/draft.md", "en", "draft", true)
	c, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Posts) != 2 {
		t.Fatalf("draft included: %d", len(c.Posts))
	}
	p, fallback := c.resolve("different/slug", zh)
	if fallback || p.Folder != "actual/folder" || p.Body != "Body\n" {
		t.Fatalf("wrong pairing/folder/body: %+v", p)
	}
	p, fallback = c.resolve("different/slug", en)
	if fallback || p.Folder != "english/path" {
		t.Fatalf("wrong English pair: %+v", p)
	}
	if defaultSite.postURL(p.Slug, en) != defaultSite.URL+"/en/posts/different/slug/" || defaultSite.postURL(p.Slug, zh) != defaultSite.URL+"/zh/posts/different/slug/" {
		t.Fatal("URLs lost nested slug")
	}
	fixture(t, root, "en/duplicate.md", "en", "different/slug", false)
	if _, err := loadCatalog(root); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate accepted: %v", err)
	}
}

func TestRootSearchAllFoldersAndTranslations(t *testing.T) {
	c := fixtureCatalog(t)
	for _, lang := range []language{zh, en} {
		items, fallback := c.listing(lang, "", "LeCtUrE")
		if fallback || len(items) != 3 {
			t.Fatalf("%s root lecture search: %d results, fallback=%v", lang, len(items), fallback)
		}
		for i, e := range items {
			if e.Post == nil || e.Post.Folder != "course-notes" || e.Post.Lang != en {
				t.Fatalf("wrong search result: %+v", e)
			}
			if i > 0 && e.Post.Date.After(items[i-1].Post.Date) {
				t.Fatal("search results not newest first")
			}
		}
		items, _ = c.listing(lang, "", "")
		if len(items) == 7 {
			t.Fatal("search changed unfiltered home")
		}
	}
	// A description-only query matches, while paired articles appear once in
	// the requested language. A folder search still excludes other folders.
	root := t.TempDir()
	fixture(t, root, "en/folder/a.md", "en", "paired", false)
	fixture(t, root, "zh/folder/a.md", "zh-CN", "paired", false)
	fixture(t, root, "en/other/b.md", "en", "other", false)
	c, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	items, _ := c.listing(zh, "", "summary")
	if len(items) != 2 {
		t.Fatalf("description matching or deduplication: %d", len(items))
	}
	for _, e := range items {
		if e.Post.Slug == "paired" && e.Post.Lang != zh {
			t.Fatal("paired search ignored preferred language")
		}
	}
	items, _ = c.listing(en, "folder", "summary")
	if len(items) != 1 || items[0].Post.Slug != "paired" {
		t.Fatal("folder search escaped its scope")
	}
}

func TestMalformedFrontmatter(t *testing.T) {
	valid := "---\ntitle: Test\ndate: 2026-01-02\nlang: en\ntranslationSlug: test\n---\nBody\n"
	for name, data := range map[string]string{
		"missing": "no frontmatter", "unclosed": "---\ntitle: hi", "missing date": strings.ReplaceAll(valid, "date: 2026-01-02\n", ""),
		"bad date": strings.ReplaceAll(valid, "2026-01-02", "yesterday"), "bad language": strings.ReplaceAll(valid, "lang: en", "lang: fr"),
		"numeric title": strings.ReplaceAll(valid, "title: Test", "title: 42"), "null description": strings.ReplaceAll(valid, "title: Test", "title: Test\ndescription: null"),
		"bad draft": strings.ReplaceAll(valid, "title: Test", "title: Test\ndraft: maybe"), "traversal": strings.ReplaceAll(valid, "translationSlug: test", "translationSlug: ../test"),
		"duplicate field": strings.ReplaceAll(valid, "title: Test", "title: Test\ntitle: Other"), "unknown field": strings.ReplaceAll(valid, "title: Test", "title: Test\ntitlle: Typo"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePost([]byte(data)); err == nil {
				t.Fatal("malformed frontmatter accepted")
			}
		})
	}
	root := t.TempDir()
	fixture(t, root, "zh/invalid.md", "en", "invalid", false)
	if _, err := loadCatalog(root); err == nil || !strings.Contains(err.Error(), "zh/invalid.md") {
		t.Fatalf("directory mismatch must name the file: %v", err)
	}
}

func TestFixtureListingAndFallback(t *testing.T) {
	c := fixtureCatalog(t)
	if len(c.Posts) != 12 {
		t.Fatalf("fixture changed: expected 12 published posts, got %d; review listing assertions", len(c.Posts))
	}
	counts := map[language]int{}
	for _, p := range c.Posts {
		counts[p.Lang]++
		if p.Draft {
			t.Fatal("draft included")
		}
	}
	if counts[zh] != 4 || counts[en] != 8 {
		t.Fatalf("unexpected language counts: %v", counts)
	}
	for _, lang := range []language{zh, en} {
		items, fallback := c.listing(lang, "", "")
		if fallback {
			t.Fatal("home never falls back")
		}
		folders := 0
		var previous *post
		for _, e := range items {
			if e.Post == nil {
				folders++
				continue
			}
			if e.Post.Lang != lang || e.Post.Folder != "" {
				t.Fatalf("wrong home listing: %+v", e.Post)
			}
			if previous != nil && e.Post.Date.After(previous.Date) {
				t.Fatal("not newest first")
			}
			previous = e.Post
		}
		if folders != 2 {
			t.Fatalf("both homes must expose English-only folders; got %d", folders)
		}
	}
	items, fallback := c.listing(zh, "course-notes", "")
	if !fallback || len(items) != 4 {
		t.Fatalf("Chinese folder fallback: %d, %v", len(items), fallback)
	}
	for _, e := range items {
		if e.Post.Lang != en || e.Post.Folder != "course-notes" {
			t.Fatal("wrong folder fallback")
		}
	}
	p, missing := c.resolve("course-notes/lecture-0-search", zh)
	if !missing || p.Lang != en {
		t.Fatal("English-only post fallback")
	}
	for _, slug := range []string{"attention-notes", "server-setup"} {
		for _, lang := range []language{zh, en} {
			p, missing := c.resolve(slug, lang)
			if missing || p.Lang != lang {
				t.Fatal("translation pair lost")
			}
		}
	}
	items, _ = c.listing(en, "reference", "")
	for _, e := range items {
		if !strings.HasPrefix(e.Post.Slug, "reference/") {
			t.Fatal("slug from filename instead of metadata")
		}
	}
}

func TestFolderFallbackIsWholeStructure(t *testing.T) {
	root := t.TempDir()
	fixture(t, root, "zh/mixed/only-zh.md", "zh-CN", "only-zh", false)
	fixture(t, root, "en/mixed/only-en.md", "en", "only-en", false)
	fixture(t, root, "zh/zh-only/a.md", "zh-CN", "zh-only", false)
	c, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	items, missing := c.listing(en, "mixed", "")
	if missing || len(items) != 1 || items[0].Post.Slug != "only-en" {
		t.Fatal("mixed folders must not inject untranslated sibling posts")
	}
	items, missing = c.listing(en, "zh-only", "")
	if !missing || len(items) != 1 || items[0].Post.Lang != zh {
		t.Fatal("English folder should fall back to Chinese")
	}
	p, missing := c.resolve("only-zh", en)
	if !missing || p.Lang != zh {
		t.Fatal("en article route should fall back to zh")
	}
}

func TestSiteOverride(t *testing.T) {
	c := fixtureCatalog(t)
	c.setSite(site{URL: "https://notes.example.org", Title: "example"})
	p, _ := c.resolve("course-notes/lecture-0-search", en)
	if p.URL != "https://notes.example.org/en/posts/course-notes/lecture-0-search/" {
		t.Fatalf("article URL: %s", p.URL)
	}
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 24)
	if brand := ansi.Strip(m.brand()); !strings.HasPrefix(brand, "example · notes.example.org") {
		t.Fatalf("header: %q", brand)
	}
	m.open(p)
	if _, footer := m.chrome(); !strings.Contains(ansi.Strip(footer), p.URL) {
		t.Fatal("footer must link to the configured site")
	}
	out, err := m.cache.render(p, 80, "rose-pine", colorprofile.TrueColor)
	if err != nil || !strings.Contains(ansi.Strip(out), "https://example.com/course/lecture-0") {
		t.Fatalf("absolute links must survive: %v", err)
	}
	r, _ := c.resolve("reference/tips-and-tricks", en)
	if out, _ := m.cache.render(r, 120, "rose-pine", colorprofile.TrueColor); !strings.Contains(ansi.Strip(out), "https://notes.example.org/lab/demo/") {
		t.Fatal("relative links must resolve against the configured site")
	}
}
