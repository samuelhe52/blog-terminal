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
var blockImage = regexp.MustCompile(`^ {0,3}!\[[^\]\n]*\]\(\s*(<[^>\n]+>|[^\s)]+)(?:\s+"[^"\n]*")?\s*\)\s*$`)
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

// Code is passed through byte for byte. Math becomes inline code or a fenced
// display block BEFORE Goldmark/Glamour can interpret TeX punctuation.
func preprocess(body, base string) string {
	return preprocessWith(body, base, nil)
}

// preprocessWith also places drawn images before their captions when images
// is not nil.
func preprocessWith(body, base string, images *imagePass) string {
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
			if m := blockImage.FindStringSubmatch(line); m != nil {
				out.WriteString(images.image(strings.TrimSuffix(strings.TrimPrefix(m[1], "<"), ">")))
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
			if math, size := mathAt(body[i:], last); size > 0 {
				out.WriteString(math)
				i += size
				continue
			}
		}
		if body[i] == '<' {
			if tag := imageTag.FindString(body[i:]); tag != "" {
				z := html.NewTokenizer(strings.NewReader(tag))
				z.Next()
				t := z.Token()
				alt, src, raw := "image", "", ""
				for _, a := range t.Attr {
					if a.Key == "alt" && a.Val != "" {
						alt = a.Val
					}
					if a.Key == "src" {
						src, raw = absoluteLink(a.Val, base), a.Val
					}
				}
				out.WriteString(images.image(raw))
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
	Source  bool // the raw Markdown of source mode (source.go)
	Images  bool
}

type renderCache struct {
	values   map[renderKey]rendered
	order    []renderKey
	graphics bool // the terminal draws images; see images.go
}

func (c *renderCache) render(p *post, width int, style string, profile colorprofile.Profile) (string, error) {
	value, err := c.renderDoc(p, width, style, profile)
	return value.text, err
}

func (c *renderCache) renderDoc(p *post, width int, style string, profile colorprofile.Profile) (rendered, error) {
	width = max(1, width)
	// Image ids are 256-color indexes, so fewer colors turn images off.
	var images *imagePass
	if c.graphics && profile >= colorprofile.ANSI256 {
		images = &imagePass{width: width, images: p.Images}
	}
	return c.lookup(renderKey{p.Slug, p.Lang, width, style, profile, false, images != nil}, func() (rendered, error) {
		value, err := renderDocument(preprocessWith(p.Body, p.URL, images), width, style)
		if err != nil {
			return rendered{}, fmt.Errorf("render %s: %w", p.File, err)
		}
		value.fit(width)
		return value, nil
	})
}

// lookup returns the cached value for key, or builds it and adapts
// its colors to the client's profile.
func (c *renderCache) lookup(key renderKey, build func() (rendered, error)) (rendered, error) {
	if value, ok := c.values[key]; ok {
		return value, nil
	}
	value, err := build()
	if err != nil {
		return rendered{}, err
	}
	var adapted strings.Builder
	writer := colorprofile.Writer{Forward: &adapted, Profile: key.Profile}
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
