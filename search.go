package main

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// postSearch is the / search inside an open article. It matches the plain
// text of the lines the viewport shows, whatever produced them, so positions
// are in display cells on those lines rather than offsets into the Markdown.
type postSearch struct {
	input   textinput.Model
	typing  bool
	query   string
	plain   []string // the viewport's lines without ANSI codes
	matches []searchMatch
	current int // index into matches; meaningful only when there are some
	origin  int // viewport offset when the prompt opened
	prev    string
	prevCur int
}

// searchMatch covers cells [start, end) of a viewport line.
type searchMatch struct{ line, start, end int }

// A match is shown this many lines below the top of the screen, so the lines
// leading up to it stay in view.
const searchMargin = 2

func newSearchInput(width int) textinput.Model {
	in := textinput.New()
	in.Prompt, in.Placeholder, in.CharLimit = "Search: ", "Find in this post…", 120
	in.SetWidth(width)
	in.SetVirtualCursor(true)
	return in
}

// findMatches returns every non-overlapping occurrence of query, in order.
// Matching is case-insensitive unless the query has an uppercase letter.
// Matches don't span lines, so text broken by wrapping or a `↪` continuation
// isn't found as one phrase.
func findMatches(lines []string, query string) []searchMatch {
	if query == "" {
		return nil
	}
	fold := strings.ToLower(query) == query
	q := foldRunes(query, fold)
	var matches []searchMatch
	for i, line := range lines {
		runes := []rune(line)
		r := foldRunes(line, fold)
		for j := 0; j+len(q) <= len(r); {
			if !slices.Equal(r[j:j+len(q)], q) {
				j++
				continue
			}
			start := ansi.StringWidth(string(runes[:j]))
			end := start + ansi.StringWidth(string(runes[j:j+len(q)]))
			if end > start {
				matches = append(matches, searchMatch{i, start, end})
			}
			j += len(q)
		}
	}
	return matches
}

// foldRunes lowercases rune by rune, so indexes line up with []rune(s).
func foldRunes(s string, fold bool) []rune {
	r := []rune(s)
	if fold {
		for i, c := range r {
			r[i] = unicode.ToLower(c)
		}
	}
	return r
}

// firstMatchFrom returns the index of the first match on line or below it.
func (s postSearch) firstMatchFrom(line int) int {
	i, _ := slices.BinarySearchFunc(s.matches, line, func(m searchMatch, line int) int { return m.line - line })
	return i
}

func (m *model) searchLines() []string {
	lines := strings.Split(m.viewport.GetContent(), "\n")
	for i, line := range lines {
		lines[i] = ansi.Strip(line)
	}
	return lines
}

// startSearch opens an empty prompt. Typing jumps to the first match at or
// below the line that was at the top when the prompt opened.
func (m *model) startSearch() tea.Cmd {
	s := &m.search
	if s.typing {
		m.cancelSearch()
	}
	s.prev, s.prevCur = s.query, s.current
	s.origin = m.viewport.YOffset()
	s.plain = m.searchLines()
	s.typing, s.query, s.matches = true, "", nil
	s.input.Reset()
	return s.input.Focus()
}

// cancelSearch closes the prompt and puts back the previous search and the
// scroll position.
func (m *model) cancelSearch() {
	s := &m.search
	s.typing = false
	s.input.Blur()
	s.query = s.prev
	s.matches = findMatches(s.plain, s.query)
	s.current = min(s.prevCur, max(0, len(s.matches)-1))
	m.viewport.SetYOffset(s.origin)
}

// clearSearch removes the query and its highlights.
func (m *model) clearSearch() {
	s := &m.search
	s.typing, s.query, s.matches, s.current = false, "", nil, 0
	s.input.Blur()
	s.input.Reset()
}

func (m *model) searchActive() bool {
	return m.search.typing || m.search.query != ""
}

// refreshSearch finds the matches again after the article is rerendered. The
// current match keeps its number if the count didn't change; otherwise it
// becomes the first match on screen or below.
func (m *model) refreshSearch() {
	s := &m.search
	s.plain = m.searchLines()
	before := len(s.matches)
	s.matches = findMatches(s.plain, s.query)
	if len(s.matches) != before {
		s.current = s.firstMatchFrom(m.viewport.YOffset())
	}
	s.current = min(s.current, max(0, len(s.matches)-1))
}

func (m model) updateSearchInput(msg tea.KeyPressMsg, key string) (model, tea.Cmd) {
	s := &m.search
	switch key {
	case "esc":
		m.cancelSearch()
		return m, nil
	case "enter":
		// An empty query has nothing to keep; leave things as they were.
		if s.query == "" {
			m.cancelSearch()
			return m, nil
		}
		s.typing = false
		s.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	if q := s.input.Value(); q != s.query {
		s.query = q
		s.matches = findMatches(s.plain, q)
		s.current = s.firstMatchFrom(s.origin)
		if s.current == len(s.matches) {
			s.current = 0
		}
		if len(s.matches) > 0 {
			m.showMatch()
		} else {
			m.viewport.SetYOffset(s.origin)
		}
	}
	return m, cmd
}

func (m *model) showMatch() {
	margin := min(searchMargin, (m.viewport.Height()-1)/2)
	m.viewport.SetYOffset(m.search.matches[m.search.current].line - margin)
}

// stepMatch moves count matches forward or back, wrapping at either end. If
// the current match has been scrolled out of view, it counts from the screen
// instead, as if the cursor had moved with it.
func (m *model) stepMatch(count int, forward bool) tea.Cmd {
	s := &m.search
	n := len(s.matches)
	if n == 0 {
		if s.query != "" {
			return m.flash("No matches")
		}
		return nil
	}
	i := s.current
	top := m.viewport.YOffset()
	if line := s.matches[i].line; line < top || line >= top+m.viewport.Height() {
		if forward {
			i = s.firstMatchFrom(top) - 1
		} else {
			i = s.firstMatchFrom(top + m.viewport.Height())
		}
	}
	wrapped := false
	for range count {
		if forward {
			i++
		} else {
			i--
		}
		if i >= n || i < 0 {
			i, wrapped = (i+n)%n, true
		}
	}
	s.current = i
	m.showMatch()
	if !wrapped {
		return nil
	}
	if forward {
		return m.flash("Search wrapped to the top")
	}
	return m.flash("Search wrapped to the bottom")
}

// currentMatchLine is the viewport line of the current match, if any.
func (m model) currentMatchLine() (int, bool) {
	if m.search.query == "" || len(m.search.matches) == 0 {
		return 0, false
	}
	return m.search.matches[m.search.current].line, true
}

// searchStatus goes in the footer next to the scroll position.
func (m model) searchStatus() string {
	if m.search.query == "" {
		return ""
	}
	if len(m.search.matches) == 0 {
		return "No matches  "
	}
	return fmt.Sprintf("match %d/%d  ", m.search.current+1, len(m.search.matches))
}

// highlightSearch marks the matches on the visible lines of the viewport.
// It works on the screen only, so the cached render is never changed, and it
// keeps every line's width and the colors on either side of a match.
func (m model) highlightSearch(view string) string {
	s := m.search
	if s.query == "" || len(s.matches) == 0 {
		return view
	}
	all, current := m.searchStyles()
	lines := strings.Split(view, "\n")
	top := m.viewport.YOffset()
	for i := s.firstMatchFrom(top); i < len(s.matches) && s.matches[i].line < top+len(lines); i++ {
		match := s.matches[i]
		style := all
		if i == s.current {
			style = current
		}
		row := match.line - top
		lines[row] = overlay(lines[row], match.start, match.end, style)
	}
	return strings.Join(lines, "\n")
}

func overlay(line string, start, end int, style lipgloss.Style) string {
	width := ansi.StringWidth(line)
	if start >= width {
		return line
	}
	end = min(end, width)
	return ansi.Cut(line, 0, start) + style.Render(ansi.Strip(ansi.Cut(line, start, end))) + ansi.Cut(line, end, width)
}

// searchStyles marks matches with the theme's secondary color and the current
// one with its accent. Without color, they are underlined and reversed.
func (m model) searchStyles() (all, current lipgloss.Style) {
	if m.profile < colorprofile.ANSI {
		return lipgloss.NewStyle().Underline(true), lipgloss.NewStyle().Reverse(true).Bold(true)
	}
	t := m.palette()
	base := m.profile.Convert(lipgloss.Color(t.Base))
	all = lipgloss.NewStyle().Foreground(base).Background(m.profile.Convert(lipgloss.Color(t.Secondary)))
	current = lipgloss.NewStyle().Bold(true).Foreground(base).Background(m.profile.Convert(lipgloss.Color(t.Accent)))
	return all, current
}
