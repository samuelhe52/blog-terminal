package main

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/doug/termtex"
	"github.com/yuin/goldmark/ast"
)

// Math is typeset as Unicode text with termtex. Inline formulas are set in
// TeX's text style on a single row and become inline code (so Markdown can't
// reinterpret the output and the theme's code color sets them apart). Display
// formulas become a fenced block tagged with displayMathInfo; layout.go
// typesets them at the width actually available. Anything termtex can't set,
// or that wouldn't fit, falls back to the TeX source, per formula.

// displayMathInfo follows "text" in a display block's info string. Glamour
// only reads the first word, so the block still renders as plain text. The
// control character can't come from a post (cleanText removes them), so a
// post's own code block is never mistaken for math.
const displayMathInfo = "\x1fmath"

// Post content is untrusted. termtex has no limits of its own, so refuse
// input that could only be pathological, and stop waiting for a formula
// that takes too long.
const (
	maxMathBytes = 1024
	maxMathDepth = 32
	maxMathRows  = 64
	mathTimeout  = 2 * time.Second
)

// mathAt reports whether s starts with a formula, and if so returns its
// Markdown replacement and the number of bytes it spans; prev is the prose
// rune before it. Inline math must end on the same line. Like Pandoc, an
// inline formula must not start or end with a space and its closing "$"
// must not be followed by a digit, so "$5 and $10" stays prose.
func mathAt(s string, prev rune) (string, int) {
	n := 1
	if strings.HasPrefix(s, "$$") {
		n = 2
	}
	search := s[n:]
	if n == 1 {
		if end := strings.IndexByte(search, '\n'); end >= 0 {
			search = search[:end]
		}
	}
	end := -1
	for at := 0; at < len(search); at++ {
		if search[at] == '\\' {
			at++
			continue
		}
		if strings.HasPrefix(search[at:], s[:n]) {
			end = at
			break
		}
	}
	if end <= 0 {
		return "", 0
	}
	tex, size := search[:end], n+end+n
	if n == 2 {
		tex = strings.Trim(tex, "\n")
		delim := "```"
		for strings.Contains(tex, delim) {
			delim += "`"
		}
		return "\n\n" + delim + "text " + displayMathInfo + "\n" + tex + "\n" + delim + "\n\n", size
	}
	first, _ := utf8.DecodeRuneInString(tex)
	last, _ := utf8.DecodeLastRuneInString(tex)
	next, _ := utf8.DecodeRuneInString(s[size:])
	if unicode.IsSpace(first) || unicode.IsSpace(last) || unicode.IsDigit(next) {
		return "", 0
	}
	shown := s[:size]
	if out, ok := typeset(tex, termtex.Style{Inline: true}); ok && !strings.Contains(out, "\n") {
		shown = out
	}
	// Keep fitting formulas together during prose layout. The renderer
	// restores ordinary spaces and hyphens after line breaks are set. The
	// zero-cell breaks around the formula count as whitespace in Markdown, so
	// leave them out next to emphasis, as in "**$x$ is**".
	md := codeSpan(strings.NewReplacer(" ", "\u00a0", "-", nbHyphen).Replace(shown))
	if !strings.ContainsRune("*_~", prev) {
		md = mathBreak + md
	}
	if !strings.ContainsRune("，。、；：！？）】》」』”’.,;:!?)]}*_~", next) {
		md += mathBreak
	}
	return md, size
}

// displayMathBlock typesets a fenced block that preprocess made from display
// math. It reports false for any other code block, and for math that falls
// back to its source.
func displayMathBlock(node ast.Node, source []byte, tex string, width int) ([]string, bool) {
	fenced, ok := node.(*ast.FencedCodeBlock)
	if !ok || fenced.Info == nil {
		return nil, false
	}
	if info := strings.Fields(string(fenced.Info.Segment.Value(source))); len(info) != 2 || info[1] != displayMathInfo {
		return nil, false
	}
	return displayMath(tex, width)
}

// displayMath typesets a display formula into at most width cells per line,
// breaking it before relations or operators where termtex can. It reports
// false if the formula can't be set or still doesn't fit.
func displayMath(tex string, width int) ([]string, bool) {
	out, ok := typeset(tex, termtex.Style{Width: width})
	if !ok || !gridAligned(out) {
		return nil, false
	}
	lines := strings.Split(out, "\n")
	if len(lines) > maxMathRows {
		return nil, false
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > width {
			return nil, false
		}
	}
	return lines, true
}

// typeset runs termtex with the guards above. A parse error, panic, or
// timeout reports false, so the caller shows the source instead.
func typeset(tex string, style termtex.Style) (string, bool) {
	return typesetWith(termtex.Render, mathTimeout, tex, style)
}

func typesetWith(render func(string, termtex.Style) (string, error), timeout time.Duration, tex string, style termtex.Style) (string, bool) {
	// A script with no base, as in "ViT$^3$", needs an empty one.
	if trimmed := strings.TrimSpace(tex); strings.HasPrefix(trimmed, "^") || strings.HasPrefix(trimmed, "_") {
		tex = `\text{}` + trimmed
	}
	if strings.TrimSpace(tex) == "" || len(tex) > maxMathBytes || mathDepth(tex) > maxMathDepth {
		return "", false
	}
	type result struct {
		out string
		ok  bool
	}
	done := make(chan result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- result{}
			}
		}()
		out, err := render(tex, style)
		done <- result{out, err == nil}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-done:
		if !r.ok || strings.TrimSpace(r.out) == "" {
			return "", false
		}
		return dropWideFillers(r.out), true
	case <-timer.C:
		return "", false
	}
}

// mathDepth is the deepest nesting of groups, \left…\right pairs, and
// environments: the input shape that makes termtex's recursive layout slow
// or its canvas large.
func mathDepth(tex string) int {
	depth, deepest := 0, 0
	for i := 0; i < len(tex); i++ {
		switch {
		case tex[i] == '\\' && hasCommand(tex[i+1:], "left", "begin"):
			depth++
		case tex[i] == '\\' && hasCommand(tex[i+1:], "right", "end"):
			depth--
		case tex[i] == '\\':
			i++ // an escaped brace doesn't open a group
		case tex[i] == '{':
			depth++
		case tex[i] == '}':
			depth--
		}
		deepest = max(deepest, depth)
	}
	return deepest
}

func hasCommand(s string, names ...string) bool {
	for _, name := range names {
		if strings.HasPrefix(s, name) && (len(s) == len(name) || !isASCIILetter(s[len(name)])) {
			return true
		}
	}
	return false
}

func isASCIILetter(b byte) bool {
	return 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// termtex paints a double-width rune into one cell of its grid and leaves
// the next cell blank, which the terminal then shows as an extra space.
func dropWideFillers(out string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		var b strings.Builder
		filler := false
		for _, r := range line {
			if filler && r == ' ' {
				filler = false
				continue
			}
			filler = gridWidth(r) == 2
			b.WriteRune(r)
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// gridAligned reports whether every rune takes as many terminal cells as
// termtex assumed when it aligned rows. Otherwise the rows of a fraction,
// matrix, or stacked limit would drift apart.
func gridAligned(out string) bool {
	if !strings.Contains(out, "\n") {
		return true
	}
	for _, r := range out {
		if r == '\n' || gridWidth(r) == 0 {
			continue
		}
		if ansi.StringWidth(string(r)) != gridWidth(r) {
			return false
		}
	}
	return true
}

// gridWidth mirrors termtex's own cell widths: combining marks sit on the
// previous cell, CJK ideographs and fullwidth forms take two, and every
// other rune takes one.
func gridWidth(r rune) int {
	switch {
	case r >= 0x0300 && r <= 0x036f, r >= 0x20d0 && r <= 0x20ff:
		return 0
	case r >= 0xff01 && r <= 0xff60, r >= 0x4e00 && r <= 0x9fff:
		return 2
	}
	return 1
}
