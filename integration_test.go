package main

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// These tests cover where the features meet: typeset math and copying,
// source mode and code blocks, search and visual mode, images and code
// block positions.

func TestYankDisplayMathCopiesSource(t *testing.T) {
	p, _ := fixtureCatalog(t).resolve("attention-notes", en)
	for _, width := range []int{40, 80} {
		var cache renderCache
		doc, err := cache.renderDoc(p, width, "rose-pine", colorprofile.TrueColor)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, b := range doc.blocks {
			if !b.math {
				continue
			}
			found = true
			if !strings.Contains(p.Body, b.source) || !strings.Contains(b.source, `\`) {
				t.Fatalf("%d: math block source %q is not the formula", width, b.source)
			}
			// Any part of a formula copies all of it, once.
			if got := doc.yank(b.start, b.end-1); got != b.source {
				t.Fatalf("%d: yanked %q, want %q", width, got, b.source)
			}
			if got := doc.yank(b.start+1, b.start+1); got != b.source {
				t.Fatalf("%d: yanking one row gave %q", width, got)
			}
		}
		if !found {
			t.Fatalf("%d: no display math located", width)
		}
		for _, b := range doc.codeBlocks() {
			if b.math {
				t.Fatal("c, [ and ] must skip display math")
			}
		}
	}
}

func TestSourceModeCodeBlocks(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	want := fences(p.Body)
	for _, width := range []int{40, 80} {
		m := newModel(c, en, "rose-pine", colorprofile.TrueColor, width, 20)
		m.open(p)
		m, _ = press(m, "s")
		if !m.source {
			t.Fatal("s must switch to source")
		}
		blocks := m.doc.codeBlocks()
		if len(blocks) != len(want) {
			t.Fatalf("%d: %d source blocks, want %d", width, len(blocks), len(want))
		}
		lines := strings.Split(ansi.Strip(m.doc.text), "\n")
		for i, b := range blocks {
			if b.source != want[i] || m.doc.yank(b.start, b.end-1) != want[i] {
				t.Fatalf("%d: block %d copies %q", width, i, m.doc.yank(b.start, b.end-1))
			}
			// The fences themselves are outside the block.
			if !strings.HasPrefix(lines[b.start-1], "```") || !strings.HasPrefix(lines[b.end], "```") {
				t.Fatalf("%d: block %d range [%d,%d) is off", width, i, b.start, b.end)
			}
		}
		m, cmd := press(m, "c")
		if got := clipboard(t, cmd); got != want[0] {
			t.Fatalf("%d: c in source mode copied %q", width, got)
		}
	}
}

func TestVisualStartsAtSearchMatch(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 20)
	m.open(p)
	m, _ = press(m, "/")
	for _, r := range "docker" {
		m, _ = press(m, string(r))
	}
	m, _ = press(m, "enter")
	line, ok := m.currentMatchLine()
	if !ok || line == m.viewport.YOffset() {
		t.Fatalf("search must land below the top line: %d %v", line, ok)
	}
	m, _ = press(m, "v")
	if !m.visual || m.anchor != line || m.cursor != line {
		t.Fatalf("v must start on the match at %d, got %d", line, m.anchor)
	}
	// esc leaves visual mode first, then clears the search, then closes.
	m, _ = press(m, "esc")
	if m.visual || m.search.query == "" || m.article == nil {
		t.Fatal("first esc must only leave visual mode")
	}
	m, _ = press(m, "esc")
	if m.search.query != "" || m.article == nil {
		t.Fatal("second esc must only clear the search")
	}
	m, _ = press(m, "esc")
	if m.article != nil {
		t.Fatal("third esc must close the article")
	}
}

func TestImagesKeepCodeBlockPositions(t *testing.T) {
	a, err := newImageAsset(image.NewNRGBA(image.Rect(0, 0, 160, 80)), 0)
	if err != nil {
		t.Fatal(err)
	}
	doc := rendered{
		text:   strings.Join([]string{"intro", "  " + imageMarker(a.id), "caption", "code 1", "code 2"}, "\n"),
		blocks: []codeBlock{{start: 3, end: 5, lines: []int{0, 1}, source: "code 1\ncode 2"}},
	}
	placed := (&imageSession{}).place(doc, 80)
	lines := strings.Split(placed.text, "\n")
	if len(lines) <= 5 {
		t.Fatal("the image took no rows")
	}
	b := placed.blocks[0]
	if b.end-b.start != 2 || lines[b.start] != "code 1" || lines[b.end-1] != "code 2" {
		t.Fatalf("block moved to [%d,%d): %q", b.start, b.end, lines[b.start:b.end])
	}
	if got := placed.yank(b.start, b.end-1); got != "code 1\ncode 2" {
		t.Fatalf("yanked %q", got)
	}
}

func TestSourceModeWrapsWordsAndYanksSource(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("attention-notes", en)
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 60, 30)
	m.open(p)
	m, _ = press(m, "s")
	lines := strings.Split(ansi.Strip(m.doc.text), "\n")
	source := strings.Split(strings.Trim(cleanText(p.Body), "\n"), "\n")
	// English prose breaks between words: a row that continues on the next
	// ends with a space, unless it is a single word too wide to fit.
	for i := 0; i+1 < len(lines); i++ {
		next := strings.TrimLeft(lines[i+1], " ")
		row := strings.TrimPrefix(strings.TrimLeft(lines[i], " "), "↪ ")
		// A row with no space in it is one word wider than the window.
		if strings.HasPrefix(next, "↪ ") && strings.Contains(strings.TrimSpace(row), " ") && !strings.HasSuffix(row, " ") {
			t.Fatalf("row %d breaks inside a word: %q / %q", i, lines[i], lines[i+1])
		}
	}
	// A selection over any rows copies whole source lines, spaces included.
	all := m.doc.yank(0, len(lines)-1)
	if all != strings.Join(source, "\n") {
		t.Fatalf("yanking everything gave\n%s", all)
	}
	wrapped := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "↪ ") {
			wrapped = i
			break
		}
	}
	if wrapped < 0 {
		t.Fatal("no wrapped rows; the test checks nothing")
	}
	got := m.doc.yank(wrapped, wrapped)
	if !slicesContains(source, got) || !strings.Contains(got, " ") {
		t.Fatalf("yanking a continuation row gave %q, not a source line", got)
	}
}

func slicesContains(lines []string, s string) bool {
	for _, l := range lines {
		if l == s {
			return true
		}
	}
	return false
}
