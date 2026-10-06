package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestPreprocess(t *testing.T) {
	base := webURL("folder/article", en)
	for _, tt := range []struct {
		name, source string
		contains     []string
	}{
		{"HTML image", `<img alt="A &amp; B_中文" src='/images/a.svg' width="600" />`, []string{"[Image: A & B_中文]", siteURL + "/images/a.svg"}},
		{"Markdown destinations", `[root](/lab/a/) ![plot](../plot.png) [nested](./a_(b).md "title")`, []string{siteURL + "/lab/a/", siteURL + "/en/posts/folder/plot.png", siteURL + `/en/posts/folder/article/a_(b).md "title"`}},
		{"reference links", "[x]: /images/x.png \"title\"\n![plot][x]", []string{siteURL + "/images/x.png"}},
		{"inline math", `Text $a_i * b_j + \alpha$ end.`, []string{codeSpan(strings.ReplaceAll(`$a_i * b_j + \alpha$`, " ", "\u00a0"))}},
		{"display math", "Before\n$$\na_i * b_j + \\alpha\n\\frac{1}{2}\n$$\nAfter", []string{"```text\na_i * b_j + \\alpha\n\\frac{1}{2}\n```"}},
		{"code span", "`$HOME * a_i \\x` and ``$x`y$``", []string{"`$HOME * a_i \\x`", "``$x`y$``"}},
		{"fenced code", "```sh\necho '$HOME' # $a_i$\n[link](/dont-touch)\n```\n~~~python\ns = '$$'\n~~~", []string{"```sh\necho '$HOME' # $a_i$\n[link](/dont-touch)\n```", "~~~python\ns = '$$'\n~~~"}},
		{"indented code", "    echo '$HOME $x$'\n", []string{"    echo '$HOME $x$'\n"}},
		{"escaped dollar", `cost \$20 and $x_i$`, []string{`\$20`, codeSpan(`$x_i$`)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := preprocess(tt.source, base)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Fatalf("missing %q in:\n%s", want, got)
				}
			}
		})
	}
}

func TestMathSurvivesRendering(t *testing.T) {
	p := &post{Slug: "math", Lang: en, Body: "Before $a_i * b_j + \\alpha$ after.\n\n$$\na_i * b_j + \\alpha\n$$\n\n```sh\necho '$HOME'\n```"}
	var cache renderCache
	out, err := cache.render(p, 80, "dark", colorprofile.TrueColor)
	if err != nil {
		t.Fatal(err)
	}
	out = ansi.Strip(out)
	for _, want := range []string{`$a_i * b_j + \alpha$`, `a_i * b_j + \alpha`, `echo '$HOME'`} {
		if !strings.Contains(out, want) {
			t.Fatalf("math/code corrupted: missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "$$") {
		t.Fatal("display delimiters leaked into rendered math")
	}
}

func TestInlineMathSpacingAndAtomicWrapping(t *testing.T) {
	if ansi.StringWidth(mathBreak) != 0 {
		t.Fatal("math break must occupy zero terminal cells")
	}
	formulas := []string{`$S \in \mathbb{R}^{r \times d_v}$`, `$z \in \mathbb{R}^{r}$`, `$O(N^2 d_k)$`}
	body := "令 $Q$ 分别。其中，" + formulas[0] + "、" + formulas[1] + "。复杂度为 " + formulas[2] + "。"
	for _, width := range []int{40, 60, 80, 120} {
		out, err := renderMarkdown(preprocess(body, siteURL), width, "dark")
		if err != nil {
			t.Fatal(err)
		}
		out = ansi.Strip(out)
		if !strings.Contains(out, "令 $Q$ 分别") {
			t.Fatalf("padded inline math: %s", out)
		}
		for _, formula := range formulas {
			if !strings.Contains(out, formula) {
				t.Fatalf("formula fitting width %d was split: %q\n%s", width, formula, out)
			}
		}
		if strings.Contains(out, mathBreak) || strings.Contains(out, "\u00a0") {
			t.Fatalf("layout markers leaked: %q", out)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "   $") {
				t.Fatalf("stray leading math padding: %q", line)
			}
			if strings.HasPrefix(line, "  。") || strings.HasPrefix(line, "  、") {
				t.Fatalf("punctuation detached from formula: %q", line)
			}
		}
	}
}

// corpora returns the fixture corpus plus, when BLOG_CONTENT_DIR is set, the
// live blog content, so corpus-wide invariants can be checked against real
// posts without committing them.
func corpora(t *testing.T) map[string]*catalog {
	t.Helper()
	all := map[string]*catalog{"fixture": fixtureCatalog(t)}
	if dir := os.Getenv("BLOG_CONTENT_DIR"); dir != "" {
		c, err := loadCatalog(dir)
		if err != nil {
			t.Fatalf("BLOG_CONTENT_DIR: %v", err)
		}
		all["external"] = c
	} else {
		t.Log("BLOG_CONTENT_DIR not set; checking the fixture corpus only")
	}
	return all
}

func TestEveryPostNeedsNoFinalGuard(t *testing.T) {
	for name, c := range corpora(t) {
		for _, p := range c.Posts {
			checkNoFinalGuard(t, name, p)
		}
	}
}

func checkNoFinalGuard(t *testing.T, corpus string, p *post) {
	t.Helper()
	{
		for _, width := range []int{60, 80, 120} {
			for _, theme := range []string{"dark", "light"} {
				out, err := renderMarkdown(preprocess(p.Body, webURL(p.Slug, p.Lang)), width, theme)
				if err != nil {
					t.Fatal(err)
				}
				assertWidth(t, out, width)
				if guarded := fitWidth(out, width); guarded != out {
					t.Fatalf("final guard changed normal output: %s %s/%s at %d (%s)", corpus, p.Lang, p.Slug, width, theme)
				}
			}
		}
	}
}

func TestQuoteAndCodeContinuations(t *testing.T) {
	quote := "> " + strings.Repeat("中文引用 with some English ", 12) + "\n>\n> Another paragraph.\n>\n> > Nested quote with a long " + strings.Repeat("word ", 30)
	for _, width := range []int{40, 60, 80, 120} {
		out, err := renderMarkdown(preprocess(quote, siteURL), width, "dark")
		if err != nil {
			t.Fatal(err)
		}
		assertWidth(t, out, width)
		for _, line := range strings.Split(ansi.Strip(out), "\n") {
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "  │") {
				t.Fatalf("quote continuation lost prefix: %q", line)
			}
		}
		code := "```text\n    " + strings.Repeat("value ", 30) + "\n" + strings.Repeat("identifier_", 30) + "\n```"
		out, err = renderMarkdown(preprocess(code, siteURL), width, "dark")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(ansi.Strip(out), "\n") {
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "    ") {
				t.Fatalf("code continuation lost indent: %q", line)
			}
		}
	}
	for _, prefix := range []string{"  │ ", "  │ │ ", "    ", "        "} {
		out := fitWidth(prefix+strings.Repeat("中文 abc ", 30), 40)
		assertWidth(t, out, 40)
		for _, line := range strings.Split(ansi.Strip(out), "\n") {
			if !strings.HasPrefix(line, prefix) {
				t.Fatalf("guard lost prefix %q: %q", prefix, line)
			}
		}
	}
}

func TestCodePalettesDoNotDependOnSessionOrder(t *testing.T) {
	code := "```python\nprint('hello')\n```"
	lightBefore, err := renderMarkdown(code, 80, "light")
	if err != nil {
		t.Fatal(err)
	}
	dark, err := renderMarkdown(code, 80, "dark")
	if err != nil {
		t.Fatal(err)
	}
	lightAfter, err := renderMarkdown(code, 80, "light")
	if err != nil {
		t.Fatal(err)
	}
	if lightBefore != lightAfter {
		t.Fatal("dark session changed the light syntax palette")
	}
	// Compare the style immediately before the literal token, rather than
	// document padding colors, to check the code palettes actually differ.
	sgr := regexp.MustCompile(`(?:\x1b\[[0-9;]*m)+print`)
	if light, dark := sgr.FindString(lightAfter), sgr.FindString(dark); light == "" || dark == "" || light == dark {
		t.Fatalf("code palettes not distinct: light %q dark %q", light, dark)
	}
}

func TestCodeAndMathSoftWrapMarkers(t *testing.T) {
	line := "  docker inspect --format 'created={{.Created}} quoted argument with spaces'"
	for _, width := range []int{40, 60, 80} {
		wrapped := wrapCodeLine(line, width-6)
		var reconstructed strings.Builder
		for i, part := range strings.Split(wrapped, "\n") {
			if i > 0 {
				if !strings.HasPrefix(part, "↪   ") {
					t.Fatalf("continuation lacks gutter/indent: %q", part)
				}
				part = strings.TrimPrefix(part, "↪   ")
			}
			reconstructed.WriteString(part)
		}
		if reconstructed.String() != line {
			t.Fatalf("soft wrapping changed code whitespace: %q", reconstructed.String())
		}
		body := "```sh\n" + line + "\necho 'real newline'\n```\n\n$$\n" + strings.Repeat(`\left(QK^\top\right)V,`, 8) + "\n$$"
		out, err := renderMarkdown(preprocess(body, siteURL), width, "dark")
		if err != nil {
			t.Fatal(err)
		}
		assertWidth(t, out, width)
		if out != fitWidth(out, width) {
			t.Fatal("continuation markers activated final guard")
		}
		if !strings.Contains(out, "\x1b[2m↪\x1b[22m") {
			t.Fatal("continuation marker must be dim")
		}
		plain := ansi.Strip(out)
		if strings.Contains(plain, "↪ echo 'real newline'") || strings.Contains(plain, "↪   docker") {
			t.Fatal("real source line was marked as a soft continuation")
		}
		sections := regexp.MustCompile(`\n[ \t]*\n`).Split(plain, -1)
		if len(sections) < 2 || !strings.Contains(sections[0], "↪ ") || !strings.Contains(sections[len(sections)-1], "↪ ") {
			t.Fatalf("shell and display math must both mark continuations:\n%s", plain)
		}
	}
	short := "echo first\necho second"
	out, err := renderMarkdown("```sh\n"+short+"\n```", 80, "dark")
	if err != nil || strings.Contains(out, "↪") {
		t.Fatal("short source lines must not acquire continuation markers")
	}
}

func assertWidth(t *testing.T, output string, width int) {
	t.Helper()
	for i, line := range strings.Split(output, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Fatalf("line %d: display width %d > %d: %q", i+1, w, width, ansi.Strip(line))
		}
	}
}

func TestEveryPostWidth(t *testing.T) {
	for name, c := range corpora(t) {
		for _, p := range c.Posts {
			checkWidths(t, name, p)
		}
	}
}

func checkWidths(t *testing.T, corpus string, p *post) {
	t.Helper()
	{
		for _, width := range []int{40, 60, 80, 120} {
			for _, style := range []string{"dark", "light"} {
				t.Run(fmt.Sprintf("%s/%s/%s/%d/%s", corpus, p.Lang, p.Slug, width, style), func(t *testing.T) {
					var cache renderCache
					out, err := cache.render(p, width, style, colorprofile.TrueColor)
					if err != nil {
						t.Fatal(err)
					}
					assertWidth(t, out, width)
					if strings.TrimSpace(ansi.Strip(out)) == "" {
						t.Fatal("empty rendered post")
					}
				})
			}
		}
	}
}

func TestWidthUnicodeAndCode(t *testing.T) {
	p := &post{Slug: "wide", Lang: zh, Body: strings.Repeat("中文没有空格段落", 100) + "\n\n```text\n" + strings.Repeat("very_long_identifier_", 50) + "\n```\n\n| A | B |\n|---|---|\n| " + strings.Repeat("中文", 40) + " | " + strings.Repeat("abcdef", 40) + " |"}
	for _, width := range []int{1, 2, 10, 40, 60, 80, 120} {
		var cache renderCache
		out, err := cache.render(p, width, "dark", colorprofile.TrueColor)
		if err != nil {
			t.Fatal(err)
		}
		assertWidth(t, out, width)
		if width >= 40 && strings.Count(ansi.Strip(out), "中文没有空格段落") == 0 {
			t.Fatal("CJK text lost")
		}
	}
}

func TestRenderCacheAndProfiles(t *testing.T) {
	p := &post{Slug: "test", Lang: en, Body: "# Heading\n\nSome **bold** words."}
	var cache renderCache
	for _, profile := range []colorprofile.Profile{colorprofile.ASCII, colorprofile.ANSI, colorprofile.ANSI256, colorprofile.TrueColor} {
		out, err := cache.render(p, 80, "dark", profile)
		if err != nil {
			t.Fatal(err)
		}
		if profile == colorprofile.ASCII && strings.Contains(out, "38;2;") {
			t.Fatal("truecolor leaked into ASCII session")
		}
	}
	for width := 40; width < 80; width++ {
		if _, err := cache.render(p, width, "light", colorprofile.ANSI256); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.values) != 16 {
		t.Fatalf("cache unbounded: %d", len(cache.values))
	}
	old := len(cache.order)
	if _, err := cache.render(p, 79, "light", colorprofile.ANSI256); err != nil {
		t.Fatal(err)
	}
	if len(cache.order) != old {
		t.Fatal("cache hit appended duplicate")
	}
}

// Regenerate deliberately: UPDATE_CAPTURES=1 go test -run TestReaderCaptures.
// Captures include the actual reader header/footer plus the complete scrollable
// body, so reviewers can inspect math, tables, code, and image placeholders.
func TestReaderCaptures(t *testing.T) {
	c := fixtureCatalog(t)
	for _, tt := range []struct {
		slug  string
		lang  language
		name  string
		width int
	}{
		{"attention-notes", zh, "zh-attention-80.txt", 80},
		{"attention-notes", zh, "zh-attention-60.txt", 60},
		{"course-notes/lecture-1-knowledge", en, "en-lecture-1-80.txt", 80},
		{"server-setup", en, "en-server-setup-80.txt", 80},
	} {
		p, _ := c.resolve(tt.slug, tt.lang)
		m := newModel(c, tt.lang, "dark", colorprofile.TrueColor, tt.width, 32)
		m.open(p)
		h, f := m.chrome()
		capture := ansi.Strip(h+"\n"+m.viewport.GetContent()+"\n"+f) + "\n"
		assertWidth(t, capture, tt.width)
		file := filepath.Join("testdata/captures", tt.name)
		if os.Getenv("UPDATE_CAPTURES") == "1" {
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte(capture), 0644); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != capture {
			t.Fatalf("%s differs; review and regenerate with UPDATE_CAPTURES=1", file)
		}
	}
}
