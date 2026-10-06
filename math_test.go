package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/doug/termtex"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

func TestInlineMathDelimiters(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		want         []string // inline code spans, in order
	}{
		{"typeset", `Let $\mathbf{x} \in \mathbb{R}^d$ be`, []string{"𝐱 ∈ ℝᵈ"}},
		{"dollar amounts", `It costs $5 and $10 today.`, nil},
		{"space inside opening", `with $ symbols there are ^n$ models`, nil},
		{"digit after closing", `between $x$1 and`, nil},
		{"trailing dollar", `大约 100$ 的额度`, nil},
		{"bare script", `ViT$^3$ and $_i$`, []string{"³", "ᵢ"}},
		{"escaped", `cost \$20 and $x_i$`, []string{"xᵢ"}},
		{"first closer decides", `$a $b$`, []string{"b"}},
		{"code", "`$HOME` and ``$x`y$``", []string{"$HOME", "$x`y$"}},
		{"parse error falls back", `see $\frac{a$ here`, []string{`$\frac{a$`}},
		{"multi-row falls back", `so $\displaystyle\sum_{i=1}^n i$ here`, []string{`$\displaystyle\sum_{i=1}^n i$`}},
		{"too deep falls back", "$" + strings.Repeat("{", maxMathDepth+1) + "x" + strings.Repeat("}", maxMathDepth+1) + "$", []string{"$" + strings.Repeat("{", maxMathDepth+1) + "x" + strings.Repeat("}", maxMathDepth+1) + "$"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := inlineCode(t, preprocess(tt.source, defaultSite.URL))
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Fatalf("code spans %q, want %q", got, tt.want)
			}
		})
	}
}

// inlineCode returns the inline code spans Goldmark finds, with the layout
// markers that preprocess adds turned back into spaces and hyphens.
func inlineCode(t *testing.T, markdown string) []string {
	t.Helper()
	source := []byte(markdown)
	var spans []string
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering && n.Kind() == ast.KindCodeSpan {
			var b strings.Builder
			for c := n.FirstChild(); c != nil; c = c.NextSibling() {
				b.Write(c.(*ast.Text).Segment.Value(source))
			}
			spans = append(spans, strings.NewReplacer("\u00a0", " ", nbHyphen, "-").Replace(b.String()))
		}
		return ast.WalkContinue, nil
	})
	return spans
}

func TestDisplayMathBreaksToFit(t *testing.T) {
	tex := `\mathcal{L}(\theta) = \sum_{i=1}^{n} \left\| \mathbf{y}_i - \mathbf{W} \mathbf{x}_i \right\|^2 + \lambda \|\mathbf{W}\|^2 = \frac{1}{2} \operatorname{tr}(\mathbf{A})`
	wide, ok := displayMath(tex, 200)
	if !ok {
		t.Fatal("formula did not typeset")
	}
	narrow, ok := displayMath(tex, 40)
	if !ok {
		t.Fatal("formula did not break to 40 columns")
	}
	if len(narrow) <= len(wide) {
		t.Fatalf("expected more rows at 40 columns:\n%s", strings.Join(narrow, "\n"))
	}
	for _, line := range narrow {
		if ansi.StringWidth(line) > 40 {
			t.Fatalf("row wider than 40: %q", line)
		}
	}
	if _, ok := displayMath(tex, 8); ok {
		t.Fatal("formula that can't fit must fall back")
	}
}

func TestDisplayMathRendering(t *testing.T) {
	body := "Before\n\n$$\n\\Sigma = \\frac{1}{n} \\mathbf{X} \\mathbf{X}^T\n$$\n\nAfter\n\n$$\n\\frac{a}{\n$$\n\n- item\n\n  ```text display-math\n  not math\n  ```\n"
	for _, width := range []int{40, 80} {
		out, err := renderMarkdown(preprocess(body, defaultSite.URL), width, "rose-pine")
		if err != nil {
			t.Fatal(err)
		}
		assertWidth(t, out, width)
		plain := ansi.Strip(out)
		for _, want := range []string{"      1", "  Σ = ─── 𝐗𝐗ᵀ", `\frac{a}{`, "not math"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("missing %q at %d:\n%s", want, width, plain)
			}
		}
		if strings.Contains(plain, "$$") || strings.Contains(plain, displayMathInfo) {
			t.Fatalf("delimiters or block tag leaked:\n%s", plain)
		}
	}
}

func TestTypesetGuards(t *testing.T) {
	ok := func(string, termtex.Style) (string, error) { return "x", nil }
	for name, render := range map[string]func(string, termtex.Style) (string, error){
		"error": func(string, termtex.Style) (string, error) { return "", errors.New("bad") },
		"panic": func(string, termtex.Style) (string, error) { panic("boom") },
		"slow":  func(string, termtex.Style) (string, error) { time.Sleep(time.Second); return "x", nil },
		"blank": func(string, termtex.Style) (string, error) { return "  ", nil },
	} {
		if _, typeset := typesetWith(render, 50*time.Millisecond, "x", termtex.Style{}); typeset {
			t.Errorf("%s: should fall back", name)
		}
	}
	if _, typeset := typesetWith(ok, time.Second, strings.Repeat("x", maxMathBytes+1), termtex.Style{}); typeset {
		t.Error("oversized input should fall back")
	}
	if _, typeset := typesetWith(ok, time.Second, `\left(`+strings.Repeat(`\begin{matrix}`, maxMathDepth)+`\right)`, termtex.Style{}); typeset {
		t.Error("deeply nested input should fall back")
	}
	if out, typeset := typesetWith(ok, time.Second, `\{\{\{x`, termtex.Style{}); !typeset || out != "x" {
		t.Error("escaped braces don't nest")
	}
}

func TestMathGlyphWidths(t *testing.T) {
	// Math alphanumerics, letterlike symbols, and combining accents are one
	// cell, as termtex assumes.
	for _, tex := range []string{`\mathbf{x}`, `\mathbb{R}`, `\mathcal{L}`, `\tilde{\mathbf{w}}`, `\hat{\lambda}`} {
		out, ok := typeset(tex, termtex.Style{})
		if !ok || ansi.StringWidth(out) != 1 {
			t.Errorf("%s = %q, %d cells", tex, out, ansi.StringWidth(out))
		}
	}
	// termtex leaves a blank cell after wide runes; the terminal already
	// gives them two cells.
	if out, _ := typeset(`\text{注意力}`, termtex.Style{}); out != "注意力" {
		t.Errorf("CJK text = %q", out)
	}
	// Rows only line up if the terminal agrees with termtex on every width.
	if gridAligned("ab\n、b") || gridAligned("😀\nab") || !gridAligned("注b\nabc") || !gridAligned("😀") {
		t.Error("gridAligned misjudged widths")
	}
	if _, ok := displayMath(`\frac{\text{注意、}}{2}`, 80); ok {
		t.Error("misaligned rows should fall back")
	}
}

// TestCorpusMath typesets every formula in the fixture and, when available,
// the live posts. Every inline formula must typeset; display formulas that
// don't fit a narrow window may fall back, and are logged.
func TestCorpusMath(t *testing.T) {
	for name, c := range corpora(t) {
		var display, longest int
		var elapsed time.Duration
		for _, p := range c.Posts {
			start := time.Now()
			md := preprocess(p.Body, p.URL)
			elapsed += time.Since(start)
			for _, span := range inlineCode(t, md) {
				if strings.HasPrefix(span, "$") && !strings.Contains(p.Body, "`"+span) {
					t.Errorf("%s %s/%s: inline math fell back: %s", name, p.Lang, p.Slug, span)
				}
			}
			for _, tex := range displayFormulas(md) {
				display++
				longest = max(longest, len(tex))
				for _, width := range []int{116, 76, 36} {
					start := time.Now()
					_, ok := displayMath(tex, width)
					elapsed += time.Since(start)
					if !ok && width == 116 {
						t.Errorf("%s %s/%s: display math fell back at %d columns:\n%s", name, p.Lang, p.Slug, width, tex)
					} else if !ok {
						t.Logf("%s %s/%s: source shown at %d columns: %.60s", name, p.Lang, p.Slug, width, tex)
					}
				}
			}
		}
		t.Logf("%s: %d display formulas (longest %d bytes), %d posts preprocessed and typeset at 3 widths in %v", name, display, longest, len(c.Posts), elapsed)
	}
}

func displayFormulas(markdown string) []string {
	source := []byte(markdown)
	var formulas []string
	doc := goldmark.New().Parser().Parse(text.NewReader(source))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if fenced, ok := n.(*ast.FencedCodeBlock); entering && ok && fenced.Info != nil && strings.HasSuffix(string(fenced.Info.Segment.Value(source)), " "+displayMathInfo) {
			var b strings.Builder
			for i := 0; i < fenced.Lines().Len(); i++ {
				segment := fenced.Lines().At(i)
				b.Write(segment.Value(source))
			}
			formulas = append(formulas, b.String())
		}
		return ast.WalkContinue, nil
	})
	return formulas
}
