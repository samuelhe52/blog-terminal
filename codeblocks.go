package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
)

// rendered is an article as the reader shows it: the ANSI text, plus where
// each code block ended up in it, so code can be copied as it was written.
type rendered struct {
	text   string
	margin int // blank cells left of the document on every line
	blocks []codeBlock
}

// codeBlock is one code block in the rendered text. Wrapped source lines take
// several rendered lines; lines maps each rendered line back to the source.
type codeBlock struct {
	start, end int    // rendered lines [start, end)
	lines      []int  // source line shown on each rendered line
	source     string // the code exactly as written, without the last newline
	math       bool   // display math, which c, [ and ] skip
	prose      bool   // source-mode text outside code, which c, [ and ] skip
}

// codeIndex collects the code blocks wrapCode lays out, in document order.
// With mark set, wrapCode replaces each code line with a marker naming its
// block and line. Everything else lays out the same, so a second, marked
// render shows which lines each block occupies without touching the real
// output.
type codeIndex struct {
	mark   bool
	margin int
	blocks []codeBlock
}

var codeMarker = regexp.MustCompile(`@@(\d+)\.(\d+)@@`)

// add records a block and returns the text to lay out for it. code is the
// source; wrapped holds each source line as wrapCodeLine split it, and text
// is those lines joined.
func (c *codeIndex) add(node ast.Node, source []byte, code string, wrapped []string, text string) string {
	if code == "" {
		return text
	}
	b := codeBlock{source: strings.TrimSuffix(code, "\n")}
	if f, ok := node.(*ast.FencedCodeBlock); ok && f.Info != nil {
		b.math = slices.Contains(strings.Fields(string(f.Info.Value(source))), displayMathInfo)
	}
	var marked strings.Builder
	for i, line := range wrapped {
		for range strings.Split(line, "\n") {
			if c.mark {
				fmt.Fprintf(&marked, "@@%d.%d@@\n", len(c.blocks), len(b.lines))
			}
			b.lines = append(b.lines, i)
		}
	}
	c.blocks = append(c.blocks, b)
	if c.mark {
		return marked.String()
	}
	return text
}

// codeFlash highlights a copied block while the note about it shows.
type codeFlash struct {
	start, end int
	noteID     int
}

// renderDocument renders an article and finds its code blocks. Articles
// with code are laid out a second time with markers in place of the code;
// a block whose markers don't line up exactly is left out, so copying falls
// back to the rendered text rather than copying the wrong lines.
func renderDocument(markdown string, width int, key string) (rendered, error) {
	index := &codeIndex{}
	text, err := layoutMarkdown(markdown, width, key, index)
	if err != nil {
		return rendered{}, err
	}
	doc := rendered{text: text, margin: index.margin}
	if len(index.blocks) == 0 {
		return doc, nil
	}
	index = &codeIndex{mark: true}
	shape, err := layoutMarkdown(markdown, width, key, index)
	if err != nil {
		return rendered{}, err
	}
	doc.blocks = locateBlocks(text, shape, index.blocks)
	return doc, nil
}

func locateBlocks(text, shape string, blocks []codeBlock) []codeBlock {
	lines := strings.Split(shape, "\n")
	if len(lines) != strings.Count(text, "\n")+1 {
		return nil
	}
	rows := make([][]int, len(blocks))
	for i, b := range blocks {
		rows[i] = slices.Repeat([]int{-1}, len(b.lines))
	}
	for i, line := range lines {
		for _, m := range codeMarker.FindAllStringSubmatch(ansi.Strip(line), -1) {
			b, _ := strconv.Atoi(m[1])
			j, _ := strconv.Atoi(m[2])
			if b < len(rows) && j < len(rows[b]) {
				rows[b][j] = i
			}
		}
	}
	var found []codeBlock
	for i, b := range blocks {
		ok := rows[i][0] >= 0
		for j, row := range rows[i] {
			ok = ok && row == rows[i][0]+j
		}
		if ok {
			b.start, b.end = rows[i][0], rows[i][0]+len(b.lines)
			found = append(found, b)
		}
	}
	return found
}

// fit applies the final width guard line by line, moving block ranges down
// when it wraps a line above or inside them.
func (r *rendered) fit(width int) {
	r.replaceLines(func(_ int, line string) []string {
		return strings.Split(fitWidth(line, width), "\n")
	})
}

// replaceLines replaces each line with the lines f returns for it, which may
// be none, and moves block ranges to match.
func (r *rendered) replaceLines(f func(i int, line string) []string) {
	lines := strings.Split(r.text, "\n")
	starts := make([]int, len(lines)+1)
	var out []string
	changed := false
	for i, line := range lines {
		starts[i] = len(out)
		repl := f(i, line)
		changed = changed || len(repl) != 1 || repl[0] != line
		out = append(out, repl...)
	}
	starts[len(lines)] = len(out)
	if !changed {
		return
	}
	r.text = strings.Join(out, "\n")
	for i, b := range r.blocks {
		var mapped []int
		for row, n := range b.lines {
			for range starts[b.start+row+1] - starts[b.start+row] {
				mapped = append(mapped, n)
			}
		}
		r.blocks[i].start, r.blocks[i].end, r.blocks[i].lines = starts[b.start], starts[b.end], mapped
	}
}

// yank returns the text of rendered lines from through to as plain text.
// Code comes from the source, whole lines at a time. Other lines lose their
// styling, the document margin, quote bars, and trailing spaces; a wrapped
// line marked ↪ that isn't in a known block is joined back to the line
// before it.
func (r rendered) yank(from, to int) string {
	lines := strings.Split(r.text, "\n")
	from, to = max(0, min(from, to)), min(len(lines)-1, max(from, to))
	var out []string
	for i := from; i <= to; i++ {
		if b := r.blockAt(i); b != nil && b.math {
			// Typeset rows don't match source lines; copy the whole formula.
			out = append(out, b.source)
			i = b.end - 1
			continue
		}
		if b := r.blockAt(i); b != nil {
			source := strings.Split(b.source, "\n")
			last := -1
			for ; i < b.end && i <= to; i++ {
				if n := b.lines[i-b.start]; n != last {
					out = append(out, source[n])
					last = n
				}
			}
			i--
			continue
		}
		line := ansi.Strip(lines[i])
		if strings.TrimLeft(line[:min(len(line), r.margin)], " ") == "" {
			line = line[min(len(line), r.margin):]
		}
		for strings.HasPrefix(line, "│") {
			line = strings.TrimPrefix(strings.TrimPrefix(line, "│"), " ")
		}
		line = strings.TrimRight(line, " ")
		body := strings.TrimLeft(line, " ")
		if rest, ok := strings.CutPrefix(body, "↪ "); ok && i > from {
			// A continuation repeats the code margin, then "↪ ", then the
			// indent of the line it continues.
			prev := out[len(out)-1]
			indent := len(prev) - len(strings.TrimLeft(prev, " ")) - (len(line) - len(body))
			out[len(out)-1] = prev + strings.TrimPrefix(rest, strings.Repeat(" ", max(0, indent)))
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func (r rendered) blockAt(line int) *codeBlock {
	for i := range r.blocks {
		if b := &r.blocks[i]; line >= b.start && line < b.end {
			return b
		}
	}
	return nil
}

// codeBlocks lists the blocks c, [ and ] work on: code, not display math
// or source-mode prose.
func (r rendered) codeBlocks() []codeBlock {
	var code []codeBlock
	for _, b := range r.blocks {
		if !b.math && !b.prose {
			code = append(code, b)
		}
	}
	return code
}

// copyCode copies the first code block with a line on screen, or else the
// next one below, and briefly highlights it.
func (m *model) copyCode() tea.Cmd {
	top, bottom := m.viewport.YOffset(), m.viewport.YOffset()+m.viewport.Height()
	for _, b := range m.doc.codeBlocks() {
		if b.end > top {
			cmd := m.copy(b.source, "Code copied")
			m.copied = codeFlash{b.start, b.end, m.noteID}
			if b.start >= bottom {
				m.viewport.SetYOffset(b.start - 1)
			}
			return cmd
		}
	}
	return m.flash("No code below")
}

// jumpCode scrolls so the count-th previous (dir < 0) or next code block
// starts one line below the top of the screen.
func (m *model) jumpCode(dir, count int) {
	blocks := m.doc.codeBlocks()
	offset := m.viewport.YOffset()
	for range count {
		next := -1
		for _, b := range blocks {
			target := max(0, b.start-1)
			if dir > 0 && target > offset {
				next = target
				break
			}
			if dir < 0 && target < offset {
				next = target
			}
		}
		if next < 0 {
			break
		}
		offset = next
	}
	m.viewport.SetYOffset(offset)
}
