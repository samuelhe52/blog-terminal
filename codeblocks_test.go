package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// fences returns the contents of the top-level fenced blocks in a post, read
// straight from its Markdown.
func fences(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile("(?ms)^```[^\n]*\n(.*?)\n```$").FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// clipboard returns the text a command copies.
func clipboard(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	for _, c := range cmd().(tea.BatchMsg) {
		if msg := c(); fmt.Sprintf("%T", msg) == "tea.setClipboardMsg" {
			return fmt.Sprint(msg)
		}
	}
	t.Fatal("nothing copied")
	return ""
}

func TestYankCodeBlockMatchesSource(t *testing.T) {
	p, _ := fixtureCatalog(t).resolve("server-setup", en)
	want := fences(p.Body)
	for _, width := range []int{40, 80} {
		var cache renderCache
		doc, err := cache.renderDoc(p, width, "rose-pine", colorprofile.TrueColor)
		if err != nil {
			t.Fatal(err)
		}
		blocks := doc.codeBlocks()
		if len(blocks) != len(want) {
			t.Fatalf("%d: found %d blocks, want %d", width, len(blocks), len(want))
		}
		wrapped := false
		for i, b := range blocks {
			if got := doc.yank(b.start, b.end-1); got != want[i] {
				t.Fatalf("%d: block %d yanked\n%q\nwant\n%q", width, i, got, want[i])
			}
			if b.source != want[i] {
				t.Fatalf("%d: block %d source %q", width, i, b.source)
			}
			lines := strings.Split(ansi.Strip(doc.text), "\n")
			wrapped = wrapped || strings.Contains(strings.Join(lines[b.start:b.end], "\n"), "↪")
			// The lines around a block are not code.
			if strings.TrimSpace(lines[b.start-1]) != "" || (b.end < len(lines) && strings.TrimSpace(lines[b.end]) != "") {
				t.Fatalf("%d: block %d range [%d,%d) is off", width, i, b.start, b.end)
			}
		}
		if !wrapped {
			t.Fatalf("%d: no wrapped code lines; the test checks nothing", width)
		}
		if !strings.Contains(want[1], "'id={{.Id}} architecture={{.Architecture}} created={{.Created}} quoted argument with spaces'") {
			t.Fatal("fixture lost its long quoted argument")
		}
	}
}

func TestYankPlainText(t *testing.T) {
	text := strings.Join([]string{
		"  \x1b[1mHeading\x1b[0m    ",
		"",
		"  │ quoted \x1b[3mtext\x1b[0m  ",
		"  │ │ nested",
		"    echo 'a long command that",
		"    ↪ wraps'",
		"      indented 'and",
		"    ↪   wraps too'",
	}, "\n")
	doc := rendered{text: text, margin: 2}
	want := "Heading\n\nquoted text\nnested\n  echo 'a long command thatwraps'\n    indented 'andwraps too'"
	if got := doc.yank(0, 7); got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
	if got := doc.yank(7, 2); got != doc.yank(2, 7) {
		t.Fatal("yank must not depend on direction")
	}
}

// Every block in every post must land on the lines that show its code.
func TestCodeBlocksLocated(t *testing.T) {
	// Quote bars are dropped from both sides; code may draw boxes with them.
	squash := strings.NewReplacer(" ", "", "\t", "", "\n", "", "↪", "", "│", "")
	for name, c := range corpora(t) {
		for _, p := range c.Posts {
			t.Run(fmt.Sprintf("%s/%s/%s", name, p.Lang, p.Slug), func(t *testing.T) {
				t.Parallel()
				for _, width := range []int{40, 80} {
					index := &codeIndex{}
					md := preprocess(p.Body, p.URL)
					if _, err := layoutMarkdown(md, width, "rose-pine", index); err != nil {
						t.Fatal(err)
					}
					doc, err := renderDocument(md, width, "rose-pine")
					if err != nil {
						t.Fatal(err)
					}
					if len(doc.blocks) != len(index.blocks) {
						t.Fatalf("%s %s/%s at %d: located %d of %d blocks", name, p.Lang, p.Slug, width, len(doc.blocks), len(index.blocks))
					}
					lines := strings.Split(ansi.Strip(doc.text), "\n")
					for _, b := range doc.blocks {
						if b.math {
							continue // typeset, so its rows don't show the source
						}
						shown := strings.Join(lines[b.start:b.end], "\n")
						if squash.Replace(shown) != squash.Replace(b.source) {
							t.Fatalf("%s %s/%s at %d: lines %d-%d show\n%s\nnot\n%s", name, p.Lang, p.Slug, width, b.start, b.end, shown, b.source)
						}
					}
				}
			})
		}
	}
}

func TestCopyAndJumpBetweenCodeBlocks(t *testing.T) {
	shortNotes(t)
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 20)
	m.open(p)
	blocks := m.doc.codeBlocks()
	m, cmd := press(m, "c")
	if got := clipboard(t, cmd); got != blocks[0].source || !strings.Contains(m.View().Content, "Code copied") {
		t.Fatalf("c at the top copied %q", got)
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "▌") {
		t.Fatal("copied block not highlighted")
	}
	m, _ = press(m, "]")
	if m.viewport.YOffset() != blocks[0].start-1 {
		t.Fatalf("] went to %d, want %d", m.viewport.YOffset(), blocks[0].start-1)
	}
	m, _ = press(m, "]")
	if m.viewport.YOffset() != blocks[1].start-1 {
		t.Fatalf("second ] went to %d", m.viewport.YOffset())
	}
	m, cmd = press(m, "c")
	if got := clipboard(t, cmd); got != blocks[1].source {
		t.Fatalf("c copied %q, want the second block", got)
	}
	m, _ = press(m, "[")
	if m.viewport.YOffset() != blocks[0].start-1 {
		t.Fatal("[ must go back one block")
	}
	m, _ = press(m, "g")
	m, _ = press(m, "g")
	m, _ = press(m, "2")
	m, _ = press(m, "]")
	if m.viewport.YOffset() != blocks[1].start-1 {
		t.Fatal("2] must skip a block")
	}
	// Past the last block, c has nothing to copy.
	m.doc.blocks = m.doc.blocks[:1]
	m, _ = press(m, "G")
	if m, cmd = press(m, "c"); !strings.Contains(m.View().Content, "No code below") {
		t.Fatal("c below every block must say so")
	}
	if _, ok := cmd().(tea.BatchMsg); ok {
		t.Fatal("c below every block must not copy")
	}

	// Display math is not code.
	p, _ = c.resolve("attention-notes", en)
	m.open(p)
	if len(m.doc.blocks) == len(m.doc.codeBlocks()) {
		t.Fatal("display math must be marked as math")
	}
	for range m.doc.blocks {
		m, _ = press(m, "]")
		for _, b := range m.doc.blocks {
			if b.math && m.viewport.YOffset() == b.start-1 {
				t.Fatal("] stopped at display math")
			}
		}
	}
}
