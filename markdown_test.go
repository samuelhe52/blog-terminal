package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func TestPreprocess(t *testing.T) {
	base := defaultSite.postURL("folder/article", en)
	for _, tt := range []struct {
		name, source string
		contains     []string
	}{
		{"HTML image", `<img alt="A &amp; B_中文" src='/images/a.svg' width="600" />`, []string{"[Image: A & B_中文]", defaultSite.URL + "/images/a.svg"}},
		{"Markdown destinations", `[root](/lab/a/) ![plot](../plot.png) [nested](./a_(b).md "title")`, []string{defaultSite.URL + "/lab/a/", defaultSite.URL + "/en/posts/folder/plot.png", defaultSite.URL + `/en/posts/folder/article/a_(b).md "title"`}},
		{"reference links", "[x]: /images/x.png \"title\"\n![plot][x]", []string{defaultSite.URL + "/images/x.png"}},
		{"inline math", `Text $a_i * b_j + \alpha$ end.`, []string{codeSpan(strings.ReplaceAll(`aᵢ * bⱼ + α`, " ", "\u00a0"))}},
		{"display math", "Before\n$$\na_i * b_j + \\alpha\n\\frac{1}{2}\n$$\nAfter", []string{"```text " + displayMathInfo + "\na_i * b_j + \\alpha\n\\frac{1}{2}\n```"}},
		{"code span", "`$HOME * a_i \\x` and ``$x`y$``", []string{"`$HOME * a_i \\x`", "``$x`y$``"}},
		{"fenced code", "```sh\necho '$HOME' # $a_i$\n[link](/dont-touch)\n```\n~~~python\ns = '$$'\n~~~", []string{"```sh\necho '$HOME' # $a_i$\n[link](/dont-touch)\n```", "~~~python\ns = '$$'\n~~~"}},
		{"indented code", "    echo '$HOME $x$'\n", []string{"    echo '$HOME $x$'\n"}},
		{"escaped dollar", `cost \$20 and $x_i$`, []string{`\$20`, codeSpan(`xᵢ`)}},
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
	p := &post{Slug: "math", Lang: en, Body: "Before $a_i * b_j + \\alpha$ after.\n\n$$\nc_i * d_j + \\beta\n$$\n\n```sh\necho '$HOME'\n```"}
	var cache renderCache
	out, err := cache.render(p, 80, "rose-pine", colorprofile.TrueColor)
	if err != nil {
		t.Fatal(err)
	}
	out = ansi.Strip(out)
	for _, want := range []string{`Before aᵢ * bⱼ + α after`, `cᵢ * dⱼ + β`, `echo '$HOME'`} {
		if !strings.Contains(out, want) {
			t.Fatalf("math/code corrupted: missing %q\n%s", want, out)
		}
	}
	if strings.Count(out, "$") != 1 {
		t.Fatal("math delimiters leaked into rendered math")
	}
}

func TestInlineMathSpacingAndAtomicWrapping(t *testing.T) {
	if ansi.StringWidth(mathBreak) != 0 {
		t.Fatal("math break must occupy zero terminal cells")
	}
	formulas := []string{`S ∈ ℝ^{r×dᵥ}`, `z ∈ ℝʳ`, `O(N²dₖ)`}
	body := `令 $Q$ 分别。其中，$S \in \mathbb{R}^{r \times d_v}$、$z \in \mathbb{R}^{r}$。复杂度为 $O(N^2 d_k)$。`
	for _, width := range []int{40, 60, 80, 120} {
		out, err := renderMarkdown(preprocess(body, defaultSite.URL), width, "rose-pine")
		if err != nil {
			t.Fatal(err)
		}
		out = ansi.Strip(out)
		if !strings.Contains(out, "令 Q 分别") {
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
			if strings.HasPrefix(line, "   ") {
				t.Fatalf("stray leading math padding: %q", line)
			}
			if strings.HasPrefix(line, "  。") || strings.HasPrefix(line, "  、") {
				t.Fatalf("punctuation detached from formula: %q", line)
			}
		}
	}
}

// corpora returns the fixture corpus plus, when BLOG_CONTENT_DIR is set or
// ./content exists, the live blog content, so corpus-wide invariants can be checked against real
// posts without committing them.
func corpora(t *testing.T) map[string]*catalog {
	t.Helper()
	all := map[string]*catalog{"fixture": fixtureCatalog(t)}
	if raceEnabled {
		// Rendering a post is single-threaded, and the race detector makes
		// the live posts take minutes; plain go test checks them.
		t.Log("race detector on; checking the fixture corpus only")
	} else if dir := defaultContentDir(); dir != "" {
		c, err := loadCatalog(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		all["external"] = c
	} else {
		t.Log("no BLOG_CONTENT_DIR or ./content; checking the fixture corpus only")
	}
	return all
}

func TestQuoteAndCodeContinuations(t *testing.T) {
	quote := "> " + strings.Repeat("中文引用 with some English ", 12) + "\n>\n> Another paragraph.\n>\n> > Nested quote with a long " + strings.Repeat("word ", 30)
	for _, width := range []int{40, 60, 80, 120} {
		out, err := renderMarkdown(preprocess(quote, defaultSite.URL), width, "rose-pine")
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
		out, err = renderMarkdown(preprocess(code, defaultSite.URL), width, "rose-pine")
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
	lightBefore, err := renderMarkdown(code, 80, "rose-pine-dawn")
	if err != nil {
		t.Fatal(err)
	}
	dark, err := renderMarkdown(code, 80, "rose-pine")
	if err != nil {
		t.Fatal(err)
	}
	lightAfter, err := renderMarkdown(code, 80, "rose-pine-dawn")
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
		body := "```sh\n" + line + "\necho 'real newline'\n```\n\n$$\n" + strings.Repeat(`\left(QK^\top\right)V,`, 12) + "\n$$"
		out, err := renderMarkdown(preprocess(body, defaultSite.URL), width, "rose-pine")
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
	out, err := renderMarkdown("```sh\n"+short+"\n```", 80, "rose-pine")
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

// TestEveryPostWidth renders every post at 40, 60, 80, and 120 columns in
// every theme. From 60 columns up, the layout must fit before the final guard
// runs, so the guard changes nothing. At 40 the guard may wrap, and the cached
// output, guard included, must fit.
func TestEveryPostWidth(t *testing.T) {
	for name, c := range corpora(t) {
		for _, p := range c.Posts {
			t.Run(fmt.Sprintf("%s/%s/%s", name, p.Lang, p.Slug), func(t *testing.T) {
				t.Parallel()
				checkWidths(t, p)
			})
		}
	}
}

func checkWidths(t *testing.T, p *post) {
	t.Helper()
	md := preprocess(p.Body, p.URL)
	for _, th := range themes {
		for _, width := range []int{60, 80, 120} {
			out, err := renderMarkdown(md, width, th.Key)
			if err != nil {
				t.Fatal(err)
			}
			assertWidth(t, out, width)
			if fitWidth(out, width) != out {
				t.Fatalf("final guard changed normal output at %d (%s)", width, th.Key)
			}
			if strings.TrimSpace(ansi.Strip(out)) == "" {
				t.Fatal("empty rendered post")
			}
		}
		var cache renderCache
		out, err := cache.render(p, 40, th.Key, colorprofile.TrueColor)
		if err != nil {
			t.Fatal(err)
		}
		assertWidth(t, out, 40)
	}
}

func TestWidthUnicodeAndCode(t *testing.T) {
	p := &post{Slug: "wide", Lang: zh, Body: strings.Repeat("中文没有空格段落", 100) + "\n\n```text\n" + strings.Repeat("very_long_identifier_", 50) + "\n```\n\n| A | B |\n|---|---|\n| " + strings.Repeat("中文", 40) + " | " + strings.Repeat("abcdef", 40) + " |"}
	for _, width := range []int{1, 2, 10, 40, 60, 80, 120} {
		var cache renderCache
		out, err := cache.render(p, width, "rose-pine", colorprofile.TrueColor)
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
		out, err := cache.render(p, 80, "rose-pine", profile)
		if err != nil {
			t.Fatal(err)
		}
		if profile == colorprofile.ASCII && strings.Contains(out, "38;2;") {
			t.Fatal("truecolor leaked into ASCII session")
		}
	}
	for width := 40; width < 80; width++ {
		if _, err := cache.render(p, width, "rose-pine-dawn", colorprofile.ANSI256); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.values) != 16 {
		t.Fatalf("cache unbounded: %d", len(cache.values))
	}
	old := len(cache.order)
	if _, err := cache.render(p, 79, "rose-pine-dawn", colorprofile.ANSI256); err != nil {
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
		m := newModel(c, tt.lang, "rose-pine", colorprofile.TrueColor, tt.width, 32)
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

func TestCJKLineBreaking(t *testing.T) {
	strip := func(out string) []string {
		var lines []string
		for _, line := range strings.Split(ansi.Strip(out), "\n") {
			if line = strings.TrimLeft(strings.TrimSpace(line), "│ "); line != "" {
				lines = append(lines, line)
			}
		}
		return lines
	}
	para := "本文先回顾标准的 softmax 注意力，再说明为什么把相似度函数换成可分解的核函数之后，整个计算可以按照序列长度线性增长。"
	for _, width := range []int{30, 40, 60} {
		out, err := renderMarkdown(preprocess(para, defaultSite.URL), width, "rose-pine")
		if err != nil {
			t.Fatal(err)
		}
		// Lines fill up instead of the Chinese run after "softmax" moving to
		// a new line as one unbreakable word. The slack allows for a space
		// plus a character kept together with its punctuation.
		lines := strip(out)
		for _, line := range lines[:len(lines)-1] {
			if w := ansi.StringWidth(line); w < width-4-5 {
				t.Fatalf("short line at %d (%d cells): %q", width, w, lines)
			}
		}
	}

	// No line may start with closing punctuation or end with opening
	// punctuation, across every fixture post and width.
	for _, p := range fixtureCatalog(t).Posts {
		for _, width := range []int{30, 40, 60, 80} {
			out, err := renderMarkdown(preprocess(p.Body, p.URL), width, "rose-pine")
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range strip(out) {
				first, _ := utf8.DecodeRuneInString(line)
				last, _ := utf8.DecodeLastRuneInString(line)
				if strings.ContainsRune("，。、；：！？）」』》", first) || strings.ContainsRune("（「『《", last) {
					t.Fatalf("%s at %d: bad break around punctuation: %q", p.Slug, width, line)
				}
			}
		}
	}

	// Inline math stays whole even with hyphens; code is never altered.
	out, err := renderMarkdown(preprocess("在每一步中维护状态 $S_t = -S_{t-1} + \\phi(k_t)$ 与归一化项 $z_t = z_{t-1}$，"+strings.Repeat("成本与长度无关，", 6), defaultSite.URL), 40, "rose-pine")
	if err != nil {
		t.Fatal(err)
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "Sₜ = -Sₜ₋₁ + φ(kₜ)") || !strings.Contains(plain, "zₜ = zₜ₋₁") || strings.ContainsAny(plain, mathBreak+nbHyphen+" ") {
		t.Fatalf("inline math split or markers leaked:\n%s", plain)
	}
	code := "`中文代码`\n\n```text\n中文代码块，不应被修改\n```\n"
	if got := preprocess(code, defaultSite.URL); got != code {
		t.Fatalf("code changed: %q", got)
	}
}
