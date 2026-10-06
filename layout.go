package main

import (
	"bytes"
	"fmt"
	"strings"

	glamour "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Own the outer margin and quote prefixes: Glamour otherwise wraps completed
// child blocks again at the document margin, discarding their continuation
// prefixes. Use its public ANSI renderer on the parsed tree so references and
// inline formatting still resolve across the entire article.
func renderMarkdown(markdown string, width int, key string) (string, error) {
	t := lookupTheme(key)
	if t.Key != key {
		return "", fmt.Errorf("unknown theme %q", key)
	}
	base := styles.DefaultStyles["dark"]
	if !t.Dark {
		base = styles.DefaultStyles["light"]
	}
	style := *base
	margin := int(*style.Document.Margin)
	if width <= 2*margin {
		margin = 0
	}
	zero := uint(0)
	style.Document.Margin = &zero
	style.Code.Prefix, style.Code.Suffix = "", ""
	// Assign fresh pointers: the copy shares them with the global default.
	color := func(c string) *string { return &c }
	style.Document.Color = color(t.Text)
	style.Heading.Color = color(t.Secondary)
	style.H1.Color, style.H1.BackgroundColor = color(t.Base), color(t.Accent)
	style.H6.Color = color(t.Muted)
	style.Link.Color = color(t.Muted)
	style.LinkText.Color = color(t.Accent)
	style.Image.Color = color(t.Muted)
	style.ImageText.Color = color(t.Muted)
	style.Code.Color = color(t.Tertiary)
	style.HorizontalRule.Color = color(t.Muted)
	// Glamour registers every custom Chroma palette as "charm" and keeps the
	// first one. Named built-ins avoid first-session theme leakage entirely.
	style.CodeBlock.Chroma = nil
	style.CodeBlock.Theme = t.Chroma
	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList))
	doc := parser.Parser().Parse(text.NewReader(source))
	result, err := renderBlocks(doc, source, max(1, width-2*margin), style)
	if err != nil {
		return "", err
	}
	result = strings.NewReplacer("\u00a0", " ", nbHyphen, "-", mathBreak, "").Replace(result)
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			lines[i] = strings.Repeat(" ", margin) + line
		}
	}
	return strings.Join(lines, "\n"), nil
}

func renderBlocks(parent ast.Node, source []byte, width int, style glamour.StyleConfig) (string, error) {
	var parts []string
	group := ast.NewDocument()
	flush := func() error {
		if group.FirstChild() == nil {
			return nil
		}
		wrapped := wrapCode(group, source, width, style)
		wrapTables := true
		r := renderer.NewRenderer(renderer.WithNodeRenderers(util.Prioritized(glamour.NewRenderer(glamour.Options{WordWrap: width, Styles: style, TableWrap: &wrapTables}), 1000)))
		var out bytes.Buffer
		if err := r.Render(&out, wrapped, group); err != nil {
			return err
		}
		parts = append(parts, dimCodeContinuations(trimBlankLines(out.String())))
		group = ast.NewDocument()
		return nil
	}
	for node := parent.FirstChild(); node != nil; {
		next := node.NextSibling()
		if node.Kind() == ast.KindBlockquote {
			if err := flush(); err != nil {
				return "", err
			}
			quote, err := renderBlocks(node, source, max(1, width-2), style)
			if err != nil {
				return "", err
			}
			lines := strings.Split(quote, "\n")
			for i, line := range lines {
				lines[i] = "│ " + line
			}
			parts = append(parts, strings.Join(lines, "\n"))
		} else {
			group.AppendChild(group, node)
		}
		node = next
	}
	if err := flush(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n\n"), nil
}

// Wrap source code before syntax highlighting, reserving the code margin and
// any nested list indent. Glamour then applies the same indentation to each
// continuation, instead of its document wrapper breaking a highlighted line.
func wrapCode(doc ast.Node, original []byte, width int, style glamour.StyleConfig) []byte {
	source := original
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || (node.Kind() != ast.KindCodeBlock && node.Kind() != ast.KindFencedCodeBlock) {
			return ast.WalkContinue, nil
		}
		indent := int(*style.CodeBlock.Margin)
		lists := 0
		for ancestor := node.Parent(); ancestor != nil; ancestor = ancestor.Parent() {
			if ancestor.Kind() == ast.KindList {
				lists++
			}
		}
		indent += max(0, lists-1) * int(style.List.LevelIndent)
		lines := node.Lines()
		var code strings.Builder
		for i := 0; i < lines.Len(); i++ {
			segment := lines.At(i)
			code.Write(segment.Value(original))
		}
		var wrappedLines []string
		for _, line := range strings.Split(strings.TrimSuffix(code.String(), "\n"), "\n") {
			wrappedLines = append(wrappedLines, wrapCodeLine(line, max(1, width-indent)))
		}
		wrapped := strings.Join(wrappedLines, "\n") + "\n"
		start := len(source)
		source = append(source, wrapped...)
		segments := text.NewSegments()
		for _, line := range strings.SplitAfter(wrapped, "\n") {
			if line == "" {
				continue
			}
			segments.Append(text.NewSegment(start, start+len(line)))
			start += len(line)
		}
		node.SetLines(segments)
		return ast.WalkSkipChildren, nil
	})
	return source
}

// Each source newline remains unmarked. A visible gutter distinguishes layout
// continuations from actual shell commands or TeX newlines. Hard wrapping also
// preserves whitespace inside quoted arguments rather than dropping it.
func wrapCodeLine(line string, width int) string {
	line = strings.ReplaceAll(line, "\t", "    ")
	if ansi.StringWidth(line) <= width {
		return line
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
	// Leave room for the marker and one wide character after the indent.
	if len(indent)+4 > width {
		indent = ""
	}
	marker := "↪ "
	if width <= 2 {
		marker = ""
	}
	// Reserve the marker on the first line as well, keeping one consistent
	// content width and leaving the original source indentation intact.
	body := strings.TrimPrefix(line, indent)
	parts := strings.Split(ansi.Hardwrap(body, max(1, width-len(indent)-ansi.StringWidth(marker)), true), "\n")
	for i := range parts {
		prefix := indent
		if i > 0 {
			prefix = marker + indent
		}
		parts[i] = prefix + parts[i]
	}
	return strings.Join(parts, "\n")
}

func dimCodeContinuations(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		plain := ansi.Strip(line)
		indent := len(plain) - len(strings.TrimLeft(plain, " "))
		if strings.HasPrefix(plain[indent:], "↪ ") {
			lines[i] = ansi.Cut(line, 0, indent) + "\x1b[2m↪\x1b[22m" + ansi.Cut(line, indent+1, ansi.StringWidth(line))
		}
	}
	return strings.Join(lines, "\n")
}

func trimBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
