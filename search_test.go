package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestFindMatchesSmartcaseAndCells(t *testing.T) {
	lines := []string{"Attention and attention", "线性注意力 attention", "ATTENTION"}
	got := findMatches(lines, "attention")
	want := []searchMatch{{0, 0, 9}, {0, 14, 23}, {1, 11, 20}, {2, 0, 9}}
	if len(got) != len(want) {
		t.Fatalf("lowercase query must ignore case: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("match %d: got %v want %v", i, got[i], want[i])
		}
	}
	if got := findMatches(lines, "Attention"); len(got) != 1 || got[0] != (searchMatch{0, 0, 9}) {
		t.Fatalf("an uppercase letter must make the query case-sensitive: %v", got)
	}
	// Wide characters count as two cells, before and inside a match.
	if got := findMatches(lines, "注意力"); len(got) != 1 || got[0] != (searchMatch{1, 4, 10}) {
		t.Fatalf("CJK cells: %v", got)
	}
	if got := findMatches([]string{"aaaa"}, "aa"); len(got) != 2 || got[1].start != 2 {
		t.Fatalf("matches must not overlap: %v", got)
	}
	if findMatches(lines, "") != nil || findMatches(lines, "missing") != nil {
		t.Fatal("empty or absent query must find nothing")
	}
}

func TestOverlayKeepsWidthAndColors(t *testing.T) {
	line := "ab\x1b[31mcd注意力ef\x1b[0mgh"
	style := lipgloss.NewStyle().Reverse(true)
	got := overlay(line, 4, 8, style)
	if ansi.Strip(got) != ansi.Strip(line) || ansi.StringWidth(got) != ansi.StringWidth(line) {
		t.Fatalf("overlay changed text or width: %q", got)
	}
	// The text after the match is still red.
	if !strings.Contains(got, "\x1b[31m力ef") {
		t.Fatalf("color after the match lost: %q", got)
	}
	if overlay(line, 20, 25, style) != line {
		t.Fatal("a match past the end must leave the line alone")
	}
}

func typeText(m model, text string) model {
	for _, r := range text {
		m, _ = press(m, string(r))
	}
	return m
}

func openPost(t *testing.T, slug string, lang language, width, height int) model {
	t.Helper()
	c := fixtureCatalog(t)
	m := newModel(c, lang, "rose-pine", colorprofile.TrueColor, width, height)
	p, _ := c.resolve(slug, lang)
	m.open(p)
	return m
}

func TestSearchInPost(t *testing.T) {
	m := openPost(t, "attention-notes", en, 80, 24)
	cached := m.viewport.GetContent()
	m, _ = press(m, "/")
	if !m.search.typing || m.filtering {
		t.Fatal("/ in the reader must open the post search, not the filter")
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "Search:") {
		t.Fatal("search prompt missing")
	}
	m = typeText(m, "attention")
	n := len(m.search.matches)
	if n < 5 || m.search.current != 0 {
		t.Fatalf("incremental search: %d matches, current %d", n, m.search.current)
	}
	m = typeText(m, " is")
	if line, ok := m.currentMatchLine(); !ok || !strings.Contains(strings.ToLower(m.search.plain[line]), "attention is") {
		t.Fatal("refining the query must move to the new match")
	}
	m, _ = press(m, "backspace")
	m, _ = press(m, "backspace")
	m, _ = press(m, "backspace")
	m, _ = press(m, "enter")
	if m.search.typing || m.search.query != "attention" || len(m.search.matches) != n {
		t.Fatal("enter must keep the query and matches")
	}
	view := m.View().Content
	if !strings.Contains(ansi.Strip(view), "match 1/") {
		t.Fatalf("footer must count matches: %q", ansi.Strip(view))
	}
	assertWidth(t, view, 80)
	if m.viewport.GetContent() != cached {
		t.Fatal("highlighting must not change the rendered article")
	}

	// n and N step through the matches and wrap with a note.
	m, _ = press(m, "n")
	if m.search.current != 1 {
		t.Fatalf("n: current %d", m.search.current)
	}
	line, _ := m.currentMatchLine()
	if m.viewport.YOffset() != max(0, min(line-searchMargin, m.viewport.TotalLineCount()-m.viewport.Height())) {
		t.Fatalf("match line %d shown at offset %d", line, m.viewport.YOffset())
	}
	m, _ = press(m, "N")
	m, cmd := press(m, "N")
	if m.search.current != n-1 || cmd == nil || !strings.Contains(m.View().Content, "wrapped to the bottom") {
		t.Fatal("N must wrap to the last match with a note")
	}
	m, _ = press(m, "2")
	m, _ = press(m, "n")
	if m.search.current != 1 {
		t.Fatalf("2n from the last match: current %d", m.search.current)
	}

	// After scrolling away, n continues from the screen.
	m, _ = press(m, "G")
	want := m.search.firstMatchFrom(m.viewport.YOffset()) % n
	m, _ = press(m, "n")
	if m.search.current != want {
		t.Fatalf("n after G must pick the first match on screen or wrap: got %d want %d", m.search.current, want)
	}

	// A resize finds the matches again in the new layout.
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 24})
	m = updated.(model)
	if len(m.search.matches) == 0 || m.search.plain[m.search.matches[0].line] == "" {
		t.Fatal("matches not recomputed after resize")
	}
	for _, w := range []int{40, 60, 80, 120} {
		updated, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: 24})
		m = updated.(model)
		m, _ = press(m, "n")
		assertWidth(t, m.View().Content, w)
		if len(strings.Split(m.View().Content, "\n")) > 24 {
			t.Fatal("search view exceeds height")
		}
	}

	// esc clears the highlight first, then closes the article.
	m, _ = press(m, "esc")
	if m.article == nil || m.search.query != "" || strings.Contains(ansi.Strip(m.View().Content), "match ") {
		t.Fatal("first esc must clear the search and keep the article")
	}
	m, _ = press(m, "esc")
	if m.article != nil {
		t.Fatal("second esc must close the article")
	}
	m, _ = press(m, "/")
	if !m.filtering || m.search.typing {
		t.Fatal("/ in a listing must still open the filter")
	}
}

func TestSearchCancelAndClear(t *testing.T) {
	m := openPost(t, "attention-notes", en, 80, 24)
	m, _ = press(m, "4")
	m, _ = press(m, "j")
	m, _ = press(m, "/")
	m = typeText(m, "linear")
	if line, ok := m.currentMatchLine(); !ok || line < 4 || m.search.current != m.search.firstMatchFrom(4) {
		t.Fatal("typing must jump to the first match below the top line")
	}
	m, _ = press(m, "esc")
	if m.search.typing || m.search.query != "" || m.viewport.YOffset() != 4 || m.article == nil {
		t.Fatal("esc while typing must cancel and restore the scroll position")
	}
	m, _ = press(m, "/")
	m = typeText(m, "zzzz")
	if !strings.Contains(m.View().Content, "No matches") || m.viewport.YOffset() != 4 {
		t.Fatal("a query without matches must say so and stay put")
	}
	m, _ = press(m, "enter")
	if _, cmd := press(m, "n"); cmd == nil {
		t.Fatal("n without matches must flash a note")
	}
	m, _ = press(m, "q")
	if m.article == nil || m.searchActive() {
		t.Fatal("q must clear the search before closing")
	}

	// A coalesced Escape and / restarts the search without leaving the post.
	m, _ = press(m, "/")
	m = typeText(m, "kernel")
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Mod: tea.ModAlt})
	m = updated.(model)
	if m.article == nil || !m.search.typing || m.search.input.Value() != "" || m.viewport.YOffset() != 4 {
		t.Fatal("Alt+/ must cancel the old search and open a fresh prompt")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape, Mod: tea.ModAlt})
	if m = updated.(model); m.search.typing || m.article == nil {
		t.Fatal("Alt+Escape must cancel the prompt")
	}

	// Opening another article clears the search.
	m, _ = press(m, "/")
	m = typeText(m, "linear")
	m, _ = press(m, "enter")
	other, _ := m.catalog.resolve("server-setup", en)
	m.open(other)
	if m.searchActive() || len(m.search.matches) != 0 {
		t.Fatal("search survived opening another article")
	}
}

func TestSearchCJKHighlightAndProfiles(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI, colorprofile.ASCII} {
		m := openPost(t, "attention-notes", zh, 60, 24)
		updated, _ := m.Update(tea.ColorProfileMsg{Profile: profile})
		m = updated.(model)
		m, _ = press(m, "/")
		m = typeText(m, "注意力")
		m, _ = press(m, "enter")
		if len(m.search.matches) == 0 {
			t.Fatal("no CJK matches")
		}
		line, _ := m.currentMatchLine()
		match := m.search.matches[m.search.current]
		if got := ansi.Cut(m.search.plain[line], match.start, match.end); got != "注意力" {
			t.Fatalf("CJK match cells cover %q", got)
		}
		view := m.View().Content
		assertWidth(t, view, 60)
		if profile == colorprofile.ASCII && (strings.Contains(view, "38;") || strings.Contains(view, "48;")) {
			t.Fatal("highlight colors leaked to a client without color")
		}
		body := m.viewport.View()
		marked := m.highlightSearch(body)
		if marked == body || ansi.Strip(marked) != ansi.Strip(body) {
			t.Fatal("highlight missing or changed the text")
		}
	}
}
