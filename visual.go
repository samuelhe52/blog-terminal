package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// startVisual selects line, the start of a line-wise selection. Motions then
// move the cursor end while the anchor stays put.
func (m *model) startVisual(line int) {
	line = min(max(0, line), max(0, m.viewport.TotalLineCount()-1))
	m.visual, m.anchor, m.cursor = true, line, line
	m.followCursor()
}

// visualKey handles a key in visual mode. Keys it doesn't handle, such as ?
// and ^L, fall through to the normal reader keys.
func (m *model) visualKey(key string, count int, explicit bool) (tea.Cmd, bool) {
	v := &m.viewport
	half := max(1, v.Height()/2)
	last := max(0, v.TotalLineCount()-1)
	switch key {
	case "j", "down", "ctrl+e", "ctrl+n":
		m.cursor += count
	case "k", "up", "ctrl+y", "ctrl+p":
		m.cursor -= count
	case "ctrl+d":
		v.ScrollDown(count * half)
		m.cursor += count * half
	case "ctrl+u":
		v.ScrollUp(count * half)
		m.cursor -= count * half
	case "ctrl+f", "space", "pgdown":
		v.ScrollDown(count * v.Height())
		m.cursor += count * v.Height()
	case "ctrl+b", "pgup":
		v.ScrollUp(count * v.Height())
		m.cursor -= count * v.Height()
	case "gg", "home":
		m.cursor = 0
		if explicit {
			m.cursor = count - 1
		}
	case "G", "end":
		m.cursor = last
		if explicit {
			m.cursor = count - 1
		}
	case "y":
		from, to := min(m.anchor, m.cursor), max(m.anchor, m.cursor)
		m.visual = false
		return m.copy(m.doc.yank(from, to), plural(to-from+1, "Copied %d line", "Copied %d lines")), true
	case "v", "V", "esc", "q":
		m.visual = false
		return nil, true
	case "?", "ctrl+l", "t":
		return nil, false
	default:
		return nil, true
	}
	m.cursor = min(max(0, m.cursor), last)
	m.followCursor()
	return nil, true
}

// followCursor scrolls just enough to show the cursor line.
func (m *model) followCursor() {
	v := &m.viewport
	if m.cursor < v.YOffset() {
		v.SetYOffset(m.cursor)
	} else if m.cursor >= v.YOffset()+v.Height() {
		v.SetYOffset(m.cursor - v.Height() + 1)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf(one, n)
	}
	return fmt.Sprintf(many, n)
}

// readerStatus starts the reader's footer: the scroll position, or in
// visual mode the mode and the number of selected lines.
func (m model) readerStatus() string {
	if !m.visual {
		return fmt.Sprintf("%3.0f%%  ", m.viewport.ScrollPercent()*100) + m.searchStatus()
	}
	n := max(m.anchor, m.cursor) - min(m.anchor, m.cursor) + 1
	return m.accent("VISUAL") + " " + m.muted(plural(n, "%d line", "%d lines")) + "  "
}

// decorate marks the selected lines of the viewport's view, or a block that
// was just copied, with a bar in the margin and a faint background. It works
// on the view, so the cached render is never changed.
func (m model) decorate(view string) string {
	from, to, cursor := m.anchor, m.cursor, m.cursor
	if !m.visual {
		if m.copied.noteID == 0 || m.copied.noteID != m.noteID {
			return view
		}
		from, to, cursor = m.copied.start, m.copied.end-1, -1
	}
	from, to = min(from, to), max(from, to)
	t := m.palette()
	bg := ""
	if c := m.profile.Convert(lipgloss.Blend1D(5, lipgloss.Color(t.Base), lipgloss.Color(t.Muted))[1]); c != nil {
		bg = ansi.Style{}.BackgroundColor(c).String()
	}
	rows := strings.Split(view, "\n")
	top := m.viewport.YOffset()
	for i, row := range rows {
		line := top + i
		if line < from || line > to || line >= m.viewport.TotalLineCount() {
			continue
		}
		if m.doc.margin > 0 {
			color := t.Secondary
			if line == cursor {
				color = t.Accent
			}
			bar := lipgloss.NewStyle().Foreground(m.profile.Convert(lipgloss.Color(color))).Render("▌")
			row = bar + ansi.Cut(row, 1, max(1, ansi.StringWidth(row)))
		}
		if bg != "" {
			row = bg + keepBackground(row, bg) + "\x1b[49m"
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// keepBackground repeats bg after every SGR sequence in s that resets the
// background, so the line's own colors and resets keep the highlight.
func keepBackground(s, bg string) string {
	var out strings.Builder
	for {
		start := strings.Index(s, "\x1b[")
		if start < 0 {
			break
		}
		end := start + 2
		for end < len(s) && (s[end] < 0x40 || s[end] > 0x7e) {
			end++
		}
		if end >= len(s) {
			break
		}
		out.WriteString(s[:end+1])
		if s[end] == 'm' && resetsBackground(s[start+2:end]) {
			out.WriteString(bg)
		}
		s = s[end+1:]
	}
	out.WriteString(s)
	return out.String()
}

// resetsBackground reports whether SGR parameters clear the background:
// a full reset (empty or 0) or 49. Color arguments such as the 0 in
// 38;2;0;0;0 are skipped.
func resetsBackground(params string) bool {
	if params == "" {
		return true
	}
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "", "0", "00", "49":
			return true
		case "38", "48", "58":
			if i+1 < len(fields) && fields[i+1] == "5" {
				i += 2
			} else if i+1 < len(fields) && fields[i+1] == "2" {
				i += 4
			}
		}
	}
	return false
}
