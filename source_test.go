package main

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// joinContinuations recovers source lines from source-mode output: a row
// starting with ↪ continues the previous line after its repeated indentation.
func joinContinuations(out string) []string {
	var lines []string
	for _, row := range strings.Split(ansi.Strip(out), "\n") {
		if rest, ok := strings.CutPrefix(row, "↪ "); ok && len(lines) > 0 {
			prev := lines[len(lines)-1]
			indent := prev[:len(prev)-len(strings.TrimLeft(prev, " "))]
			lines[len(lines)-1] += strings.TrimPrefix(rest, indent)
			continue
		}
		lines = append(lines, row)
	}
	return lines
}

func sourceLines(body string) []string {
	return strings.Split(strings.ReplaceAll(strings.Trim(cleanText(body), "\n"), "\t", "    "), "\n")
}

func TestEveryPostSourceFitsAndRoundTrips(t *testing.T) {
	for name, c := range corpora(t) {
		for _, p := range c.Posts {
			t.Run(fmt.Sprintf("%s/%s/%s", name, p.Lang, p.Slug), func(t *testing.T) {
				t.Parallel()
				want := sourceLines(p.Body)
				for _, width := range []int{40, 60, 80, 120} {
					for _, th := range themes {
						out := renderSource(p.Body, width, th.Key)
						assertWidth(t, out, width)
						if fitWidth(out, width) != out {
							t.Fatalf("final guard changed source: %s %s/%s at %d (%s)", name, p.Lang, p.Slug, width, th.Key)
						}
						got := joinContinuations(out)
						if strings.Join(got, "\n") != strings.Join(want, "\n") {
							t.Fatalf("%s %s/%s at %d: source lines not recoverable", name, p.Lang, p.Slug, width)
						}
					}
				}
			})
		}
	}
}

func TestSourceShowsAuthorTextSafely(t *testing.T) {
	body := "See [the notes](/en/posts/other/) and <img src=\"a.png\" alt=\"x\">.\n\n[ref]: ../relative\n\n$x$ and $$y$$\n\x1b]52;c;aGk=\x07evil\x1b[2J text\r\n\u009b31m"
	for _, profile := range []colorprofile.Profile{colorprofile.ASCII, colorprofile.ANSI256, colorprofile.TrueColor} {
		var cache renderCache
		doc, err := cache.renderSource(&post{Slug: "s", Lang: en, Body: body, URL: "https://example.com/en/posts/s/"}, 120, "rose-pine", profile)
		out := doc.text
		if err != nil {
			t.Fatal(err)
		}
		plain := ansi.Strip(out)
		for _, want := range []string{"(/en/posts/other/)", "<img src=\"a.png\" alt=\"x\">", "[ref]: ../relative", "$x$ and $$y$$", "]52;c;aGk=evil[2J text"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("source changed the author's text: missing %q in %q", want, plain)
			}
		}
		if strings.Contains(plain, "https://example.com") {
			t.Fatal("source mode must not rewrite links")
		}
		for _, bad := range []string{"\x07", "\u009b", "\r", "\x1b]"} {
			if strings.Contains(out, bad) {
				t.Fatalf("%v: content control sequence %q reached the terminal", profile, bad)
			}
		}
		if profile == colorprofile.ASCII && strings.Contains(out, "38;") {
			t.Fatal("color leaked into an ASCII session")
		}
		if profile == colorprofile.ANSI256 && strings.Contains(out, "38;2;") {
			t.Fatal("true color leaked into a 256-color session")
		}
	}
}

func classesOf(t *testing.T, lines ...string) map[string]sourceClass {
	t.Helper()
	var st sourceState
	got := map[string]sourceClass{}
	for _, line := range lines {
		classes, _ := st.classify(line)
		for at := 0; at < len(line); {
			end := at + 1
			for end < len(line) && classes[end] == classes[at] {
				end++
			}
			got[line[at:end]] = classes[at]
			at = end
		}
	}
	return got
}

func TestSourceHighlighting(t *testing.T) {
	for _, tt := range []struct {
		lines []string
		want  map[string]sourceClass
	}{
		{[]string{"## Title *here*"}, map[string]sourceClass{"##": srcHeadingMark, " Title *here*": srcHeading}},
		{[]string{"Some **bold**, _it_, ~~gone~~ and `co*de*` but not 2 * 3 or snake_case_name."},
			map[string]sourceClass{"**": srcEmphasis, "_": srcEmphasis, "~~": srcEmphasis, "`co*de*`": srcCode, ", ": srcText, " and ": srcText, " but not 2 * 3 or snake_case_name.": srcText}},
		{[]string{"A [link](https://x.org/a_(b)) and ![alt](img.png) and [ref][1]."},
			map[string]sourceClass{"[": srcBracket, "link": srcText, "](": srcBracket, "https://x.org/a_(b)": srcURL, ")": srcBracket, "![": srcBracket, "img.png": srcURL, "][": srcBracket, "1": srcURL}},
		{[]string{"Bare https://example.com/x, <https://a.b/c> and <span class=\"k\">x</span> <!-- note -->"},
			map[string]sourceClass{"https://example.com/x": srcURL, ",": srcText, "<": srcBracket, "https://a.b/c": srcURL, "<span class=\"k\">": srcHTML, "</span>": srcHTML, "<!-- note -->": srcHTML}},
		{[]string{"Inline $a_b * c$ and $$d$$, but \\$5 and $ 6 is $7."}, map[string]sourceClass{"$a_b * c$": srcMath, "$$d$$": srcMath}},
		{[]string{"$$", "x * y_1", "$$"}, map[string]sourceClass{"$$": srcMath, "x * y_1": srcMath}},
		{[]string{"> > quoted *x*", "- item", "  12. item", "* [x] done", "---", "***"},
			map[string]sourceClass{">": srcQuote, "-": srcList, "12.": srcList, "* [x]": srcList, "---": srcRule, "***": srcRule}},
		{[]string{"````md", "```", "# not a heading", "````"},
			map[string]sourceClass{"````md": srcCode, "```": srcCodeBody, "# not a heading": srcCodeBody, "````": srcCode}},
		{[]string{"[id]: /path \"Title\""}, map[string]sourceClass{"[": srcBracket, "]:": srcBracket, "/path": srcURL}},
	} {
		got := classesOf(t, tt.lines...)
		for text, class := range tt.want {
			if got[text] != class {
				t.Errorf("%q: %q has class %d, want %d (all: %v)", tt.lines, text, got[text], class, got)
			}
		}
	}
}

func TestSourceWrapsCJKAndIndentation(t *testing.T) {
	body := "    " + strings.Repeat("中文没有空格", 20) + "\n\n```\n\t" + strings.Repeat("x", 100) + "\n" + strings.Repeat(" ", 36) + strings.Repeat("宽", 10) + "\n```"
	for _, width := range []int{1, 2, 10, 39, 40, 41, 60} {
		out := fitWidth(renderSource(body, width, "rose-pine"), width)
		assertWidth(t, out, width)
		if width < 10 {
			continue
		}
		if out != renderSource(body, width, "rose-pine") {
			t.Fatalf("guard changed source at %d", width)
		}
		rows := strings.Split(ansi.Strip(out), "\n")
		if !strings.HasPrefix(rows[1], "↪     ") || rows[1][len("↪     ")] == ' ' {
			t.Fatalf("continuation lost its indentation at %d: %q", width, rows[1])
		}
		if !strings.Contains(out, "\x1b[2m↪\x1b[22m") {
			t.Fatal("continuation marker is not dim")
		}
		// Narrower than the indentation, wrapping drops it, and so may the join.
		if got := strings.Join(joinContinuations(out), "\n"); width >= 39 && got != strings.Join(sourceLines(body), "\n") {
			t.Fatalf("at %d: lines not recoverable:\n%s", width, got)
		}
	}
}

func TestSourceCodeBlocks(t *testing.T) {
	body := "Intro\n\n```bash\necho \"" + strings.Repeat("long ", 30) + "\"\n\tindented\n```\n\n> ```\n> quoted\n> ```\n\n~~~\nunclosed"
	for _, width := range []int{40, 80} {
		l := layoutSource(body, width)
		rows := strings.Split(ansi.Strip(l.render(themes[0])), "\n")
		want := []string{"echo \"" + strings.Repeat("long ", 30) + "\"\n\tindented", "quoted", "unclosed"}
		if len(l.blocks) != len(want) {
			t.Fatalf("blocks: %+v", l.blocks)
		}
		for i, b := range l.blocks {
			if b.Text != want[i] {
				t.Fatalf("block %d text %q", i, b.Text)
			}
			if !strings.Contains(rows[b.Start], "```") && !strings.Contains(rows[b.Start], "~~~") {
				t.Fatalf("block %d starts at %q", i, rows[b.Start])
			}
			if i < 2 && !strings.HasSuffix(rows[b.End-1], "```") {
				t.Fatalf("block %d ends at %q", i, rows[b.End-1])
			}
		}
		if l.blocks[2].End != len(rows) {
			t.Fatal("an unclosed fence runs to the end")
		}
		if width == 40 && l.blocks[0].End-l.blocks[0].Start < 6 {
			t.Fatal("wrapped rows missing from the block's range")
		}
	}
}

func TestSourceMode(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("server-setup", en)
	m := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 24)
	m.open(p)
	rendered := ansi.Strip(m.viewport.GetContent())
	header, _ := m.chrome()
	if strings.Contains(ansi.Strip(header), "Source") {
		t.Fatal("rendered view marked as source")
	}
	m, _ = press(m, "s")
	if !m.source || !strings.Contains(ansi.Strip(m.View().Content), "2026-06-30 · Source") {
		t.Fatal("s must show the source with a header indicator")
	}
	if !strings.Contains(ansi.Strip(m.viewport.GetContent()), "](/en/posts/server-setup/#pitfalls-and-fixes)") || len(m.doc.codeBlocks()) != 3 {
		t.Fatalf("source view or its code blocks missing: %d %s", len(m.doc.codeBlocks()), ansi.Strip(m.viewport.GetContent()))
	}
	if len(m.cache.values) != 2 {
		t.Fatalf("rendered and source output must be cached separately: %d", len(m.cache.values))
	}

	// The section at the top of the screen stays at the top.
	m, _ = press(m, "s")
	at := -1
	for i, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "## Check the image") {
			at = i
		}
	}
	m.viewport.SetYOffset(at)
	m, _ = press(m, "s")
	if top := strings.TrimSpace(ansi.Strip(strings.Split(m.viewport.View(), "\n")[0])); top != "## Check the image" {
		t.Fatalf("source top: %q", top)
	}
	m, _ = press(m, "s")
	if m.viewport.YOffset() != at {
		t.Fatalf("back to rendered at %d, want %d", m.viewport.YOffset(), at)
	}

	// Translation, theme, resize, and color changes keep the mode.
	m, _ = press(m, "s")
	m, _ = press(m, "ctrl+l")
	m, _ = press(m, "t")
	m, _ = press(m, "j")
	m, _ = press(m, "enter")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = updated.(model)
	if !m.source || !strings.Contains(ansi.Strip(m.viewport.GetContent()), "```bash") {
		t.Fatal("mode lost across ctrl+l, theme, or resize")
	}
	assertWidth(t, m.View().Content, 60)

	// Closing the article resets it.
	m, _ = press(m, "q")
	if m.source || m.doc.blocks != nil {
		t.Fatal("source mode must reset when the article closes")
	}
	m.open(p)
	if strings.Contains(ansi.Strip(m.viewport.GetContent()), "```bash") {
		t.Fatal("a newly opened article starts rendered")
	}
}

func TestSourceHint(t *testing.T) {
	c := fixtureCatalog(t)
	p, _ := c.resolve("attention-notes", en)
	for _, width := range []int{40, 60, 80, 120} {
		m := newModel(c, en, "rose-pine", colorprofile.TrueColor, width, 30)
		m.open(p)
		hints := ansi.Strip(m.hintLine("  0%  ", true))
		if width >= 60 != strings.Contains(hints, "s source") {
			t.Fatalf("hints at %d: %q", width, hints)
		}
	}
	for _, row := range helpRows {
		if row[0] == "s" {
			return
		}
	}
	t.Fatal(fmt.Sprint("no help row for s: ", helpRows))
}

// A deep indent must still leave room for a wide character after the marker.
func TestCodeLineIndentLeavesRoomForWideCharacters(t *testing.T) {
	line := strings.Repeat(" ", 36) + strings.Repeat("宽", 10)
	for width := 4; width <= 42; width++ {
		assertWidth(t, wrapCodeLine(line, width), width)
	}
}
