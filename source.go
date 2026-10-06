package main

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// Source mode shows a post's Markdown body as the author wrote it, after
// cleanText but without preprocess, so links stay relative and math stays in
// its dollar signs. The frontmatter is left out: the header already shows the
// title and date. Highlighting is deliberately small: one color per kind of
// Markdown syntax, decided line by line, with fences and display math the only
// state carried between lines.

// codeBlock is a fenced block's position on screen and its contents.
type codeBlock struct {
	Start, End int    // rows of the opening fence and just past the closing one
	Text       string // the lines between the fences, without the fences
}

type sourceClass uint8

const (
	srcText sourceClass = iota
	srcHeadingMark
	srcHeading
	srcEmphasis
	srcCode // inline code and fence lines
	srcCodeBody
	srcBracket
	srcURL
	srcMath
	srcQuote
	srcList
	srcHTML
	srcRule
)

// sourceSegment is one row of a soft-wrapped source line: a prefix (the
// line's indentation repeated after the ↪ marker) and a byte range of the line.
type sourceSegment struct {
	prefix     string
	start, end int
}

type sourceLayout struct {
	lines    []string // source lines, tabs expanded as in code blocks
	classes  [][]sourceClass
	segments [][]sourceSegment
	first    []int // the row each line starts on
	rows     int
	headings []int // lines holding an ATX heading
	blocks   []codeBlock
}

// renderSource lays out and colors a post's body for the theme key. Colors are
// written in true color; the render cache adapts them to the client.
func renderSource(body string, width int, key string) string {
	return layoutSource(body, width).render(lookupTheme(key))
}

func layoutSource(body string, width int) sourceLayout {
	width = max(1, width)
	var l sourceLayout
	var st sourceState
	open, quoted := -1, false
	var code []string
	for i, raw := range strings.Split(strings.Trim(cleanText(body), "\n"), "\n") {
		line := strings.ReplaceAll(raw, "\t", "    ")
		wasFenced := st.fence != 0
		classes, heading := st.classify(line)
		l.lines = append(l.lines, line)
		l.classes = append(l.classes, classes)
		l.first = append(l.first, l.rows)
		l.segments = append(l.segments, wrapSource(line, width))
		l.rows += len(l.segments[i])
		if heading {
			l.headings = append(l.headings, i)
		}
		switch {
		case !wasFenced && st.fence != 0:
			open, quoted, code = i, st.quoted, nil
		case wasFenced && st.fence != 0:
			// Copy what the author wrote: the raw line, with any quote prefix
			// the fence opened under removed.
			if quoted {
				_, n := quotePrefix(raw)
				raw = raw[n:]
			}
			code = append(code, raw)
		case wasFenced:
			l.blocks = append(l.blocks, codeBlock{Start: l.first[open], End: l.rows, Text: strings.Join(code, "\n")})
			open = -1
		}
	}
	if open >= 0 {
		l.blocks = append(l.blocks, codeBlock{Start: l.first[open], End: l.rows, Text: strings.Join(code, "\n")})
	}
	return l
}

// wrapSource soft-wraps a line exactly like a code line and reports where
// each row's text came from, so it can be colored after wrapping.
func wrapSource(line string, width int) []sourceSegment {
	parts := strings.Split(wrapCodeLine(line, width), "\n")
	if len(parts) == 1 {
		return []sourceSegment{{start: 0, end: len(line)}}
	}
	marker := "↪ "
	if width <= 2 {
		marker = ""
	}
	// wrapCodeLine repeats the indentation unless it leaves no room; try both.
	lead := line[:len(line)-len(strings.TrimLeft(line, " "))]
	for _, indent := range []string{lead, ""} {
		segments := make([]sourceSegment, 0, len(parts))
		pos := 0
		for i, part := range parts {
			prefix := ""
			if i > 0 {
				prefix = marker + indent
			}
			text, ok := strings.CutPrefix(part, prefix)
			if !ok || !strings.HasPrefix(line[pos:], text) {
				segments = nil
				break
			}
			segments = append(segments, sourceSegment{prefix: prefix, start: pos, end: pos + len(text)})
			pos += len(text)
		}
		if segments != nil && pos == len(line) {
			return segments
		}
	}
	return []sourceSegment{{start: 0, end: len(line)}}
}

func (l sourceLayout) render(t theme) string {
	styles := sourceStyles(t)
	rows := make([]string, 0, l.rows)
	for i, line := range l.lines {
		classes := l.classes[i]
		for j, seg := range l.segments[i] {
			var row strings.Builder
			row.WriteString(seg.prefix)
			for at := seg.start; at < seg.end; {
				end := at + 1
				for end < seg.end && classes[end] == classes[at] {
					end++
				}
				row.WriteString(styles[classes[at]].Render(line[at:end]))
				at = end
			}
			if j > 0 {
				rows = append(rows, dimCodeContinuations(row.String()))
			} else {
				rows = append(rows, row.String())
			}
		}
	}
	return strings.Join(rows, "\n")
}

func sourceStyles(t theme) [srcRule + 1]lipgloss.Style {
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	var s [srcRule + 1]lipgloss.Style
	s[srcText] = fg(t.Text)
	s[srcHeadingMark] = fg(t.Accent).Bold(true)
	s[srcHeading] = fg(t.Accent).Bold(true)
	s[srcEmphasis] = fg(t.Tertiary)
	s[srcCode] = fg(t.Tertiary)
	s[srcCodeBody] = fg(t.Secondary)
	s[srcBracket] = fg(t.Accent)
	s[srcURL] = fg(t.Muted)
	s[srcMath] = fg(t.Secondary)
	s[srcQuote] = fg(t.Muted)
	s[srcList] = fg(t.Accent)
	s[srcHTML] = fg(t.Muted)
	s[srcRule] = fg(t.Muted)
	return s
}

// sourceState carries an open fence or display-math block between lines.
type sourceState struct {
	fence    byte
	fenceLen int
	quoted   bool // the fence opened inside a block quote
	math     bool
}

var (
	ruleLine      = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	listItem      = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])(?: +|$)(?:\[[ xX]\](?: |$))?`)
	htmlTag       = regexp.MustCompile(`^(?:<!--.*?-->|</?[A-Za-z][A-Za-z0-9-]*(?:\s[^<>]*)?/?>)`)
	autolink      = regexp.MustCompile(`^<[A-Za-z][A-Za-z0-9+.-]{1,31}:[^\s<>]*>`)
	referenceDef  = regexp.MustCompile(`^\[[^\]]+\]:[ \t]*(\S+)`)
	bareURL       = regexp.MustCompile(`^https?://[^\s<>]*[^\s<>.,;:!?'")\]]`)
	headingMarker = regexp.MustCompile(`^#{1,6}(?: |$)`)
)

// quotePrefix measures the block-quote markers at the start of a line and
// returns their byte positions.
func quotePrefix(line string) (marks []int, n int) {
	for {
		j := n
		for j < len(line) && j-n < 3 && line[j] == ' ' {
			j++
		}
		if j >= len(line) || line[j] != '>' {
			return marks, n
		}
		marks = append(marks, j)
		n = j + 1
		if n < len(line) && line[n] == ' ' {
			n++
		}
	}
}

// classify assigns a class to every byte of line and reports whether the line
// is an ATX heading.
func (st *sourceState) classify(line string) ([]sourceClass, bool) {
	c := make([]sourceClass, len(line))
	fill := func(from, to int, class sourceClass) {
		for i := from; i < to; i++ {
			c[i] = class
		}
	}
	at := 0
	if st.fence == 0 || st.quoted {
		marks, n := quotePrefix(line)
		for _, m := range marks {
			c[m] = srcQuote
		}
		at = n
	}
	rest := line[at:]
	if st.fence != 0 {
		ch, n, found := fenceAt(rest)
		if found && ch == st.fence && n >= st.fenceLen && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(rest), string(ch))) == "" {
			st.fence = 0
			fill(at, len(line), srcCode)
		} else {
			fill(at, len(line), srcCodeBody)
		}
		return c, false
	}
	if ch, n, found := fenceAt(rest); found {
		st.fence, st.fenceLen, st.quoted = ch, n, at > 0
		fill(at, len(line), srcCode)
		return c, false
	}
	if st.math {
		end := strings.Index(rest, "$$")
		if end < 0 {
			fill(at, len(line), srcMath)
			return c, false
		}
		st.math = false
		fill(at, at+end+2, srcMath)
		inlineSource(line, at+end+2, c)
		return c, false
	}
	if ruleLine.MatchString(rest) {
		fill(at, len(line), srcRule)
		return c, false
	}
	at += len(rest) - len(strings.TrimLeft(rest, " "))
	rest = line[at:]
	if m := headingMarker.FindString(rest); m != "" {
		marks := len(strings.TrimRight(m, " "))
		fill(at, at+marks, srcHeadingMark)
		fill(at+marks, len(line), srcHeading)
		return c, true
	}
	if m := listItem.FindString(rest); m != "" {
		fill(at, at+len(strings.TrimRight(m, " ")), srcList)
		at += len(m)
		rest = line[at:]
	}
	if strings.HasPrefix(rest, "$$") && !strings.Contains(rest[2:], "$$") {
		st.math = true
		fill(at, len(line), srcMath)
		return c, false
	}
	if m := referenceDef.FindStringSubmatchIndex(rest); m != nil {
		label := strings.IndexByte(rest, ']')
		c[at] = srcBracket
		fill(at+label, at+label+2, srcBracket)
		fill(at+m[2], at+m[3], srcURL)
		inlineSource(line, at+m[1], c)
		return c, false
	}
	inlineSource(line, at, c)
	return c, false
}

// inlineSource colors code spans, math, HTML tags, links, and emphasis markers
// from byte at onward. Text inside link brackets is scanned like any other.
func inlineSource(line string, at int, c []sourceClass) {
	fill := func(from, to int, class sourceClass) {
		for i := from; i < to; i++ {
			c[i] = class
		}
	}
	jumps := map[int]int{}
	for i := at; i < len(line); {
		if to, ok := jumps[i]; ok {
			i = to
			continue
		}
		ch := line[i]
		switch {
		case ch == '\\' && i+1 < len(line):
			_, size := utf8.DecodeRuneInString(line[i+1:])
			i += 1 + size
			continue
		case ch == '`':
			n := 1
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			if end := strings.Index(line[i+n:], line[i:i+n]); end >= 0 {
				fill(i, i+n+end+n, srcCode)
				i += n + end + n
			} else {
				i += n
			}
			continue
		case ch == '$':
			if end := mathEnd(line, i); end > 0 {
				fill(i, end, srcMath)
				i = end
				continue
			}
		case ch == '<':
			if m := autolink.FindString(line[i:]); m != "" {
				c[i], c[i+len(m)-1] = srcBracket, srcBracket
				fill(i+1, i+len(m)-1, srcURL)
				i += len(m)
				continue
			}
			if m := htmlTag.FindString(line[i:]); m != "" {
				fill(i, i+len(m), srcHTML)
				i += len(m)
				continue
			}
		case ch == '[' || (ch == '!' && i+1 < len(line) && line[i+1] == '['):
			open := i
			if ch == '!' {
				open++
			}
			if close := closingBracket(line, open); close > 0 && close+1 < len(line) {
				switch line[close+1] {
				case '(':
					if end := closingParen(line, close+2); end > 0 {
						fill(i, open+1, srcBracket)
						fill(close, close+2, srcBracket)
						fill(close+2, end, srcURL)
						c[end] = srcBracket
						jumps[close] = end + 1
						i = open + 1
						continue
					}
				case '[':
					if end := closingBracket(line, close+1); end > 0 {
						fill(i, open+1, srcBracket)
						fill(close, close+2, srcBracket)
						fill(close+2, end, srcURL)
						c[end] = srcBracket
						jumps[close] = end + 1
						i = open + 1
						continue
					}
				}
			}
		case ch == 'h' && !isWordRune(lastRune(line[:i])):
			if m := bareURL.FindString(line[i:]); m != "" {
				fill(i, i+len(m), srcURL)
				i += len(m)
				continue
			}
		case ch == '*' || ch == '_' || ch == '~':
			n := 1
			for i+n < len(line) && line[i+n] == ch {
				n++
			}
			if emphasisRun(line, i, n) {
				fill(i, i+n, srcEmphasis)
			}
			i += n
			continue
		}
		i++
	}
}

func lastRune(s string) rune {
	r, _ := utf8.DecodeLastRuneInString(s)
	return r
}

// mathEnd finds the end of $…$ or $$…$$ starting at i on this line, with the
// same rule as preprocess: a closing delimiter after at least one character.
func mathEnd(line string, i int) int {
	n := 1
	if strings.HasPrefix(line[i:], "$$") {
		n = 2
	}
	search := line[i+n:]
	for at := 0; at < len(search); at++ {
		if search[at] == '\\' {
			at++
			continue
		}
		if strings.HasPrefix(search[at:], strings.Repeat("$", n)) {
			if at == 0 {
				return 0
			}
			return i + n + at + n
		}
	}
	return 0
}

// closingBracket returns the index of the ] matching the [ at open, or -1.
func closingBracket(line string, open int) int {
	depth := 0
	for i := open; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// closingParen returns the index of the ) ending a link destination that
// starts at from, allowing balanced parentheses inside it, or -1.
func closingParen(line string, from int) int {
	depth := 0
	for i := from; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// emphasisRun approximates CommonMark's flanking rules: a run of * or _ (or
// exactly two ~) that touches text on one side, and _ never inside a word.
func emphasisRun(line string, i, n int) bool {
	if line[i] == '~' && n != 2 {
		return false
	}
	before, _ := utf8.DecodeLastRuneInString(line[:i])
	after, _ := utf8.DecodeRuneInString(line[i+n:])
	space := func(r rune) bool { return r == utf8.RuneError || unicode.IsSpace(r) }
	if space(before) && space(after) {
		return false
	}
	if line[i] == '_' && isWordRune(before) && isWordRune(after) {
		return false
	}
	return true
}

// The render cache stores source output beside rendered output, keyed by mode.
func (c *renderCache) renderSource(p *post, width int, style string, profile colorprofile.Profile) (string, error) {
	width = max(1, width)
	return c.lookup(renderKey{p.Slug, p.Lang, width, style, profile, true, false}, func() (string, error) {
		return fitWidth(renderSource(p.Body, width, style), width), nil
	})
}

// articleContent renders the open article in the current mode and records the
// code blocks of the source view.
func (m *model) articleContent() (string, error) {
	m.codeBlocks = nil
	if !m.source {
		return m.cache.render(m.article, m.width, m.palette().Key, m.profile)
	}
	m.codeBlocks = layoutSource(m.article.Body, max(1, m.width)).blocks
	return m.cache.renderSource(m.article, m.width, m.palette().Key, m.profile)
}

// toggleSource switches between the rendered article and its source. It keeps
// the same section in view: headings found in both versions anchor the
// position, and rows between them map proportionally.
func (m *model) toggleSource() {
	if m.article == nil {
		return
	}
	top, before, failed := m.viewport.YOffset(), m.viewport.GetContent(), m.err != nil
	m.source = !m.source
	m.renderArticle(true)
	if failed || m.err != nil {
		return
	}
	rendered, source := before, m.viewport.GetContent()
	if !m.source {
		rendered, source = source, before
	}
	anchors := sourceAnchors(layoutSource(m.article.Body, max(1, m.width)), rendered, strings.Count(source, "\n")+1)
	if m.source {
		m.viewport.SetYOffset(mapRow(anchors, top, 0, 1))
	} else {
		m.viewport.SetYOffset(mapRow(anchors, top, 1, 0))
	}
}

// sourceAnchors pairs the rows of the rendered article with the rows of its
// source at the start, at every heading found in both, and at the end.
func sourceAnchors(l sourceLayout, rendered string, sourceRows int) [][2]int {
	lines := strings.Split(rendered, "\n")
	anchors := [][2]int{{0, 0}}
	from := 0
	for _, h := range l.headings {
		want := headingText(l.lines[h])
		if want == "" {
			continue
		}
		for r := from; r < len(lines); r++ {
			plain := strings.TrimSpace(ansi.Strip(lines[r]))
			got := headingText(plain)
			match := got == want
			// Glamour prefixes H2–H6 with #, so a wrapped heading's first row
			// can be matched on its own.
			if strings.HasPrefix(plain, "#") && got != "" && strings.HasPrefix(want, got) {
				match = true
			}
			if match {
				last := anchors[len(anchors)-1]
				if r > last[0] && l.first[h] > last[1] {
					anchors = append(anchors, [2]int{r, l.first[h]})
				}
				from = r + 1
				break
			}
		}
	}
	return append(anchors, [2]int{len(lines), sourceRows})
}

// headingText normalizes a heading for comparison: no # markers, emphasis or
// code delimiters, or extra spaces.
func headingText(s string) string {
	s = strings.TrimLeft(strings.TrimSpace(s), "#")
	s = strings.TrimRight(strings.TrimSpace(s), "#")
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune("*_`", r) {
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// mapRow interpolates row from one side of the anchors to the other.
func mapRow(anchors [][2]int, row, from, to int) int {
	for i := len(anchors) - 2; i >= 0; i-- {
		a, b := anchors[i], anchors[i+1]
		if row >= a[from] {
			span := b[from] - a[from]
			if span <= 0 {
				return a[to]
			}
			return a[to] + min(row-a[from], span)*(b[to]-a[to])/span
		}
	}
	return 0
}
