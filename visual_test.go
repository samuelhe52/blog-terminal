package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestVisualSelectAndYank(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 20)
	m.open(p)
	m, _ = press(m, "4")
	m, _ = press(m, "j")
	m, _ = press(m, "v")
	if mid := 4 + (m.viewport.Height()-1)/2; !m.visual || m.selecting || m.anchor != mid || m.cursor != mid {
		t.Fatalf("v must put the cursor mid-screen at %d: %v %v %d %d", mid, m.visual, m.selecting, m.anchor, m.cursor)
	}
	// Before the anchor is down, motions move a one-line cursor.
	m, _ = press(m, "3")
	m, _ = press(m, "j")
	if m.anchor != m.cursor || m.selecting {
		t.Fatalf("j before v must not select: %d %d", m.anchor, m.cursor)
	}
	if footer := ansi.Strip(m.View().Content); !strings.Contains(footer, "VISUAL") || strings.Contains(footer, "VISUAL 1 line") || !strings.Contains(footer, "v select") {
		t.Fatalf("footer must show the cursor step:\n%s", footer)
	}
	m, _ = press(m, "L")
	if bottom := 4 + m.viewport.Height() - 1; m.cursor != bottom || m.viewport.YOffset() != 4 {
		t.Fatalf("L: cursor %d, want %d without scrolling", m.cursor, bottom)
	}
	m, _ = press(m, "H")
	if m.cursor != 4 || m.anchor != 4 {
		t.Fatalf("H: cursor %d anchor %d", m.cursor, m.anchor)
	}
	m, _ = press(m, "v")
	m, _ = press(m, "2")
	m, _ = press(m, "j")
	if !m.selecting || m.anchor != 4 || m.cursor != 6 {
		t.Fatalf("v must drop the anchor: %v %d %d", m.selecting, m.anchor, m.cursor)
	}
	m, _ = press(m, "o")
	if m.anchor != 6 || m.cursor != 4 {
		t.Fatalf("o must swap the ends: %d %d", m.anchor, m.cursor)
	}
	m, _ = press(m, "o")
	footer := ansi.Strip(m.View().Content)
	if !strings.Contains(footer, "VISUAL 3 lines") || !strings.Contains(footer, "y yank") {
		t.Fatalf("footer lacks the mode and count:\n%s", footer)
	}
	m, cmd := press(m, "y")
	if m.visual {
		t.Fatal("y must leave visual mode")
	}
	if got, want := clipboard(t, cmd), m.doc.yank(4, 6); got != want || !strings.Contains(m.View().Content, "Copied 3 lines") {
		t.Fatalf("yanked %q, want %q", got, want)
	}

	// Selecting exactly a code block's lines yields its source, upwards too.
	b := m.doc.codeBlocks()[1]
	m.startVisual(b.end - 1)
	m, _ = press(m, "v")
	for range b.end - 1 - b.start {
		m, _ = press(m, "k")
	}
	if m.viewport.YOffset() > b.start {
		t.Fatal("the viewport must follow the cursor")
	}
	if m, cmd = press(m, "y"); clipboard(t, cmd) != fences(p.Body)[1] {
		t.Fatal("block selection must yank its source")
	}

	// Motions move the cursor and keep it on screen.
	m, _ = press(m, "V")
	m, _ = press(m, "G")
	if last := m.viewport.TotalLineCount() - 1; m.cursor != last || !m.viewport.AtBottom() {
		t.Fatalf("G: cursor %d, want %d", m.cursor, last)
	}
	m, _ = press(m, "g")
	m, _ = press(m, "g")
	if m.cursor != 0 || !m.viewport.AtTop() {
		t.Fatal("gg")
	}
	m, _ = press(m, "ctrl+d")
	if m.cursor != m.viewport.Height()/2 {
		t.Fatalf("ctrl+d moved the cursor to %d", m.cursor)
	}
	m, _ = press(m, "1")
	m, _ = press(m, "2")
	m, _ = press(m, "G")
	if m.cursor != 11 {
		t.Fatalf("12G: %d", m.cursor)
	}
	m, _ = press(m, "h")
	if m.article == nil || !m.visual {
		t.Fatal("h in visual mode must not close the article")
	}

	// esc and q leave visual mode before closing the article, from either
	// step; v leaves once the anchor is down.
	for _, keys := range [][]string{{"esc"}, {"q"}, {"v", "esc"}, {"v", "q"}, {"v", "v"}} {
		m.startVisual(0)
		for _, key := range keys {
			m, _ = press(m, key)
		}
		if m.visual || m.article == nil {
			t.Fatalf("%v must only leave visual mode", keys)
		}
	}
	m, _ = press(m, "esc")
	if m.article != nil {
		t.Fatal("esc must close the article after visual mode")
	}
}

func TestVisualDrawing(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI, colorprofile.ASCII} {
		for _, width := range []int{40, 60, 80, 120} {
			m := newModel(c, en, "rose-pine-dawn", profile, width, 24)
			m.open(p)
			cached := m.viewport.GetContent()
			before := strings.Split(m.viewport.View(), "\n")
			for _, key := range []string{"v", "H", "v", "3", "j"} {
				m, _ = press(m, key)
			}
			assertWidth(t, m.View().Content, width)
			after := strings.Split(m.decorate(m.viewport.View()), "\n")
			for i := range after {
				plainBefore, plainAfter := ansi.Strip(before[i]), ansi.Strip(after[i])
				if i <= 3 {
					if !strings.HasPrefix(plainAfter, "▌") || plainAfter[len("▌"):] != plainBefore[1:] {
						t.Fatalf("%d: selected row %d changed: %q -> %q", width, i, plainBefore, plainAfter)
					}
				} else if after[i] != before[i] {
					t.Fatalf("%d: unselected row %d changed", width, i)
				}
				if profile == colorprofile.ASCII && strings.Contains(after[i], "\x1b[") && !strings.Contains(before[i], "\x1b[") {
					t.Fatal("ASCII sessions must get no colors")
				}
			}
			if m.viewport.GetContent() != cached {
				t.Fatal("drawing the selection changed the cached render")
			}
			hints := strings.Split(ansi.Strip(m.View().Content), "\n")
			if last := hints[len(hints)-1]; !strings.Contains(last, "VISUAL") || !strings.Contains(last, "j/k extend") {
				t.Fatalf("%d: hints %q", width, last)
			}
			// Rerendering moves lines around, so it ends the selection.
			updated, _ := m.Update(tea.WindowSizeMsg{Width: width - 1, Height: 24})
			if updated.(model).visual {
				t.Fatal("resize must leave visual mode")
			}
		}
	}
}

func TestKeepBackground(t *testing.T) {
	for params, want := range map[string]bool{"": true, "0": true, "49": true, "1;0": true, "38;2;0;0;0": false, "48;5;0": false, "1": false, "39": false} {
		if resetsBackground(params) != want {
			t.Fatalf("resetsBackground(%q) != %v", params, want)
		}
	}
	got := keepBackground("a\x1b[1mb\x1b[0mc\x1b[38;2;0;0;0md\x1b[m", "BG")
	if want := "a\x1b[1mb\x1b[0mBGc\x1b[38;2;0;0;0md\x1b[mBG"; got != want {
		t.Fatalf("got %q", got)
	}
}
