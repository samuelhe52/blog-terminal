package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/net/html"
)

var imageTag = regexp.MustCompile(`(?is)^<img\b[^>]*>`)
var referenceLink = regexp.MustCompile(`(?m)^( {0,3}\[[^\]\n]+\]:\s*)(<[^>]+>|\S+)`)

// A zero-cell Unicode space gives Glamour a discretionary word boundary next
// to CJK punctuation, without adding a visible space around inline formulas.
const mathBreak = "\u2028"

// The wrapper always breaks after "-"; inline math uses a non-breaking hyphen
// while wrapping (like its non-breaking spaces) so formulas such as
// $z_{t-1}$ stay whole. Both are restored after wrapping.
const nbHyphen = "\u2011"

func absoluteLink(raw, base string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	b, err := url.Parse(base)
	if err != nil {
		return raw
	}
	return b.ResolveReference(u).String()
}

// Strip terminal controls supplied by content without removing newlines/tabs.
func cleanText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n"))
}

func codeSpan(s string) string {
	// A longer delimiter protects even math containing literal backticks.
	delim := "`"
	for strings.Contains(s, delim) {
		delim += "`"
	}
	return delim + " " + s + " " + delim
}

func fenceAt(line string) (byte, int, bool) {
	s := strings.TrimLeft(line, " ")
	if len(line)-len(s) > 3 || len(s) < 3 || (s[0] != '`' && s[0] != '~') {
		return 0, 0, false
	}
	n := 0
	for n < len(s) && s[n] == s[0] {
		n++
	}
	return s[0], n, n >= 3
}

// Code is passed through byte for byte. Math becomes literal inline code or a
// fenced display block BEFORE Goldmark/Glamour can interpret TeX punctuation.
func preprocess(body, base string) string {
	body = cleanText(body)
	var out strings.Builder
	var fence byte
	fenceLen := 0
	var prev rune // last prose rune written, 0 after any non-prose output
	for i := 0; i < len(body); {
		last := prev
		prev = 0
		lineStart := i == 0 || body[i-1] == '\n'
		if lineStart {
			end := strings.IndexByte(body[i:], '\n')
			if end < 0 {
				end = len(body) - i
			} else {
				end++
			}
			line := body[i : i+end]
			ch, n, found := fenceAt(line)
			if fence != 0 {
				out.WriteString(line)
				if found && ch == fence && n >= fenceLen && strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), string(ch))) == "" {
					fence = 0
				}
				i += end
				continue
			}
			if found {
				fence, fenceLen = ch, n
				out.WriteString(line)
				i += end
				continue
			}
			if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
				out.WriteString(line)
				i += end
				continue
			}
			if loc := referenceLink.FindStringSubmatchIndex(line); loc != nil {
				dest := line[loc[4]:loc[5]]
				angled := strings.HasPrefix(dest, "<")
				if angled {
					dest = strings.TrimSuffix(strings.TrimPrefix(dest, "<"), ">")
				}
				dest = absoluteLink(dest, base)
				if angled {
					dest = "<" + dest + ">"
				}
				out.WriteString(line[:loc[4]] + dest + line[loc[5]:])
				i += end
				continue
			}
		}
		if body[i] == '\\' && i+1 < len(body) {
			out.WriteString(body[i : i+2])
			i += 2
			continue
		}
		if body[i] == '`' {
			n := 1
			for i+n < len(body) && body[i+n] == '`' {
				n++
			}
			delim := body[i : i+n]
			if end := strings.Index(body[i+n:], delim); end >= 0 {
				out.WriteString(body[i : i+n+end+n])
				i += n + end + n
				continue
			}
		}
		if body[i] == '$' {
			n := 1
			if strings.HasPrefix(body[i:], "$$") {
				n = 2
			}
			search := body[i+n:]
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
				if strings.HasPrefix(search[at:], strings.Repeat("$", n)) {
					end = at
					break
				}
			}
			if end > 0 {
				math := body[i : i+n+end+n]
				if n == 2 {
					math = strings.Trim(math[2:len(math)-2], "\n")
					delim := "```"
					for strings.Contains(math, delim) {
						delim += "`"
					}
					out.WriteString("\n\n" + delim + "text " + displayMath + "\n" + math + "\n" + delim + "\n\n")
				} else {
					// Keep fitting formulas together during prose layout. The
					// renderer restores ordinary spaces after line breaks are set.
					out.WriteString(mathBreak + codeSpan(strings.NewReplacer(" ", "\u00a0", "-", nbHyphen).Replace(math)))
					next, _ := utf8.DecodeRuneInString(body[i+n+end+n:])
					if !strings.ContainsRune("，。、；：！？）】》」』”’.,;:!?)]}", next) {
						out.WriteString(mathBreak)
					}
				}
				i += n + end + n
				continue
			}
		}
		if body[i] == '<' {
			if tag := imageTag.FindString(body[i:]); tag != "" {
				z := html.NewTokenizer(strings.NewReader(tag))
				z.Next()
				t := z.Token()
				alt, src := "image", ""
				for _, a := range t.Attr {
					if a.Key == "alt" && a.Val != "" {
						alt = a.Val
					}
					if a.Key == "src" {
						src = absoluteLink(a.Val, base)
					}
				}
				out.WriteString("\n\n" + codeSpan("[Image: "+alt+"]") + "\n\n" + src + "\n\n")
				i += len(tag)
				continue
			}
			// Autolinks may also contain a relative destination.
			if end := strings.IndexByte(body[i:], '>'); end > 0 {
				raw := body[i+1 : i+end]
				if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
					out.WriteString("<" + absoluteLink(raw, base) + ">")
					i += end + 1
					continue
				}
			}
		}
		if strings.HasPrefix(body[i:], "](") {
			// Locate the destination separately from an optional title; keep
			// balanced parentheses (common in external article URLs).
			start := i + 2
			for start < len(body) && body[start] == ' ' {
				start++
			}
			end, depth := start, 0
			angle := start < len(body) && body[start] == '<'
			if angle {
				start++
				end = start
				for end < len(body) && body[end] != '>' {
					end++
				}
			} else {
				for end < len(body) {
					c := body[end]
					if c == '\\' && end+1 < len(body) {
						end += 2
						continue
					}
					if c == '(' {
						depth++
					}
					if c == ')' {
						if depth == 0 {
							break
						}
						depth--
					}
					if c == ' ' || c == '\n' {
						break
					}
					end++
				}
			}
			if end > start {
				out.WriteString(body[i:start])
				out.WriteString(absoluteLink(body[start:end], base))
				i = end
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(body[i:])
		if cjkBreakable(last, r) {
			out.WriteString(mathBreak)
		}
		out.WriteString(body[i : i+size])
		i += size
		prev = r
	}
	return out.String()
}

// Chinese has no spaces between words, but Glamour's wrapper only breaks at
// whitespace, so a CJK run up to the next space moves to a new line as one
// unbreakable word. Mark the break opportunities between CJK characters with
// the same zero-cell space used around math (removed after wrapping), and
// follow the usual line-breaking rules: no line starts with closing
// punctuation or ends with opening punctuation.
func cjkBreakable(a, b rune) bool {
	if a == 0 || !(isCJK(a) || isCJK(b)) || !(isCJK(a) || isWordRune(a)) || !(isCJK(b) || isWordRune(b)) {
		return false
	}
	return !strings.ContainsRune(cjkNoLineStart, b) && !strings.ContainsRune(cjkNoLineEnd, a)
}

const (
	cjkNoLineStart = "，。、；：！？）」』》〉】〕｝］…—～·％”’,.;:!?)]}%"
	cjkNoLineEnd   = "（「『《〈【〔｛［“‘([{"
)

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) ||
		(r >= 0x3000 && r <= 0x303f) || // CJK symbols and punctuation
		(r >= 0xff00 && r <= 0xffef) || // fullwidth forms
		r == '…' || r == '—' || r == '“' || r == '”' || r == '‘' || r == '’'
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

type renderKey struct {
	Slug    string
	Lang    language
	Width   int
	Style   string
	Profile colorprofile.Profile
}

type renderCache struct {
	values map[renderKey]rendered
	order  []renderKey
}

func (c *renderCache) render(p *post, width int, style string, profile colorprofile.Profile) (string, error) {
	value, err := c.renderDoc(p, width, style, profile)
	return value.text, err
}

func (c *renderCache) renderDoc(p *post, width int, style string, profile colorprofile.Profile) (rendered, error) {
	width = max(1, width)
	key := renderKey{p.Slug, p.Lang, width, style, profile}
	if value, ok := c.values[key]; ok {
		return value, nil
	}
	value, err := renderDocument(preprocess(p.Body, p.URL), width, style)
	if err != nil {
		return rendered{}, fmt.Errorf("render %s: %w", p.File, err)
	}
	value.fit(width)
	var adapted strings.Builder
	writer := colorprofile.Writer{Forward: &adapted, Profile: profile}
	if _, err := writer.WriteString(value.text); err != nil {
		return rendered{}, err
	}
	value.text = adapted.String()
	if c.values == nil {
		c.values = make(map[renderKey]rendered)
	}
	if len(c.order) >= 16 {
		delete(c.values, c.order[0])
		c.order = c.order[1:]
	}
	c.values[key] = value
	c.order = append(c.order, key)
	return value, nil
}

func fitWidth(s string, width int) string {
	width = max(1, width)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if ansi.StringWidth(line) <= width && !strings.Contains(line, "\t") {
			continue
		}
		line = strings.ReplaceAll(line, "\t", "    ")
		plain := ansi.Strip(line)
		prefix := len(plain) - len(strings.TrimLeft(plain, " "))
		for strings.HasPrefix(plain[prefix:], "│") {
			prefix += len("│")
			prefix += len(plain[prefix:]) - len(strings.TrimLeft(plain[prefix:], " "))
		}
		cells := ansi.StringWidth(plain[:prefix])
		if cells >= width {
			cells = max(0, width-1)
		}
		indent := ansi.Cut(line, 0, cells)
		body := ansi.Cut(line, cells, ansi.StringWidth(line))
		wrapped := strings.Split(ansi.Wrap(body, width-cells, ""), "\n")
		for j, part := range wrapped {
			wrapped[j] = ansi.Truncate(indent+part, width, "")
		}
		lines[i] = strings.Join(wrapped, "\n")
	}
	return strings.Join(lines, "\n")
}
