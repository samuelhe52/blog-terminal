# Architecture

blog-terminal is a single Go package. It uses the Charm v2 libraries: Wish
2.0.5, Bubble Tea 2.0.10, Bubbles 2.2.1, Glamour 2.0.1, and Lip Gloss 2.0.6,
imported from `charm.land/.../v2`. Exact versions are pinned in `go.mod` and
`go.sum`.

| File | Responsibility |
| --- | --- |
| `main.go` | Command-line modes (`local`, `serve`) and flags |
| `content.go` | Loading, validating, and pairing posts; building folders |
| `markdown.go` | Rewriting Markdown into a form that renders well in a terminal |
| `layout.go` | Rendering Markdown to ANSI text that fits the window |
| `math.go` | Typesetting math as Unicode text |
| `math_image.go` | Drawing display math as an image (not yet used by the reader) |
| `model.go` | The per-session Bubble Tea model |
| `server.go` | The Wish SSH server and which SSH requests it allows |
| `limits.go` | Connection, session, and rate limits |
| `session.go` | A workaround for a PTY race in Charm's SSH library |

## Content

`content.go` walks the content directory and reads every `.md` file. For each
post it checks the frontmatter schema and that the post's `lang` matches the
directory it is in (`zh/` or `en/`). Drafts are skipped. Two posts with the
same language and slug are an error. Any error names the file and stops
startup, so a bad post is caught immediately rather than at read time.

Posts are sorted newest first. Folders come from each file's path. Chinese
and English versions are paired by `translationSlug`, which also determines
the post URL, including any folder segments. URLs follow the website's
routes, `<site>/zh/posts/<slug>/` and `<site>/en/posts/<slug>/`, where
`<site>` is `--site-url` (default `https://blog.konakona.dev`). Relative links
and images in posts resolve against the article's URL on that site.

## Language fallback

The reader mirrors the website's rules where it can, and differs only where
the website would leave a visitor at a dead end.

The home listing for each language shows that language's root posts, plus
folders from both languages, as the Blog's `src/pages/{zh,en}/index.astro`
does.

Inside a folder, the preferred language is used for the whole folder if it
has any content there. Untranslated posts from the other language are not
mixed in. If the preferred language has nothing in that folder, the reader
shows the other language's contents with a notice. The website's folder page
shows a notice and a link in this case instead; the reader shows the
contents directly so visitors can keep browsing.

An English article can fall back to its Chinese original. `ctrl+l` changes
the language preference globally and keeps the same slug, so an English-only
article can be "viewed in Chinese" and still show its English text. Because
the website has no Chinese URL for an English-only post, the footer always
links to the language the article actually exists in.

Intermediate folders that contain no posts directly are hidden, matching
`folders.ts` on the website. The current posts don't have any.

## Navigation and search

While the filter is being edited, every printable key is treated as text,
including `h`, `l`, `t`, `q`, `?`, and digits. Matching is case-insensitive
on titles and descriptions.

- `enter` applies the query and leaves the editor.
- `esc` clears the query and leaves the editor.
- With a query applied, `/` reopens it for editing without typing a slash,
  and `esc` clears it.

Some terminals and tmux send `Alt+Escape` or `Alt+/` when `esc` and another
key are pressed in quick succession. These sequences also clear the query,
so the editor can't get stuck with an old query.

At the root, search covers every folder. Each article appears once, in the
preferred language if it exists and in the original language otherwise, and
shows its folder path. Inside a folder, search only covers that folder and
follows the same fallback rules as the folder listing.

`q` and `esc` step back one level at a time: close the article, then clear an
applied filter, then go to the parent folder. At the top level `q` quits and
`esc` does nothing. `esc` also cancels a pending count or `g`.

## Screen layout

The header shows the domain on the left and the current language preference
(`中文` or `English`) on the right. Article metadata shows only the date.

In listings, long descriptions are shortened. In the reader, long titles
wrap. The footer shows the article's web URL and the scroll position as a
percentage.

When the window is resized or the translation is switched, the article is
reflowed and the reader scrolls to roughly the same percentage.

Key hints in the footer always fit on one line. On narrow windows, less
important hints are dropped, but the keys themselves still work.

## Markdown preprocessing

Before rendering, `markdown.go` rewrites a few constructs that Glamour can't
display well in a terminal. It leaves fenced code, indented code, and inline
code untouched.

- **Images.** Raw HTML `<img>` tags become an `[Image: alt]` caption followed
  by an absolute URL.
- **Links.** Relative link and reference targets are resolved against the
  article's web URL, so they work outside the site.
- **Inline math** is typeset on one row and becomes inline code, so it
  keeps the theme's code color and Markdown can't reinterpret it. Inside the
  formula, spaces are replaced with non-breaking spaces and invisible markers
  are placed around it, so a formula that fits on a line is never split,
  including when it sits next to CJK text. The markers are removed after
  wrapping, and left out next to `*` or `_` so emphasis around a formula
  still works.
- **Display math** becomes a plain-text code block with the `$$` delimiters
  removed, tagged so that `layout.go` can typeset it (see below).

An inline formula follows Pandoc's rules: the opening `$` must not be
followed by a space, the closing `$` must not follow a space or precede a
digit, and both must be on the same line. So `$5 and $10` and `100$` stay
prose. `\$` is a literal dollar sign.

## Math

`math.go` typesets math with [termtex](https://github.com/doug/termtex),
which lays TeX out on a grid of terminal cells using Unicode symbols, math
alphanumerics (𝐱, ℝ, ℒ), sub- and superscripts, and box-drawing characters.

Inline math uses TeX's text style: fractions become `a/b` and limits move to
the side, so most formulas fit on one row. A formula that would still need
more than one row is shown as source.

Display math is typeset while `wrapCode` lays out code blocks, because only
then is the width known: the window minus the document margin, quote prefixes,
the code margin, and any list indent. termtex breaks a formula that is too
wide before relations, then before binary operators. The rows are inserted
as preformatted code, so Glamour doesn't reflow them. If the formula still
doesn't fit, it is shown as source and wrapped with `↪` like other code.

Every formula falls back to its source on its own, without affecting the
rest of the post. That happens when termtex reports a parse error, panics, or
takes more than two seconds, and for input over 1 KB or nested more than 32
groups deep, which could only be pathological. A formula that times out
keeps running in the background, since Go can't stop it.

termtex assumes every character is one cell wide except CJK ideographs and
fullwidth forms. Lines are measured with `ansi.StringWidth`, which agrees for
math alphanumerics and for letters with combining accents (𝐰̃, λ̂). termtex
leaves a blank cell after each wide character, which is removed. A formula
that spans several rows and contains a character whose terminal width
differs from termtex's (such as `、` or an emoji in `\text{}`) is shown as
source, because its rows would no longer line up.

### Math images

`math_image.go` can also draw a display formula as an image, for terminals
that show graphics. The reader doesn't use it yet. `renderMathImage` typesets
the formula with [go-tex/math](https://github.com/go-tex/math), which uses
the OpenType MATH table of an embedded STIX Two Math font and writes SVG made
of filled glyph outlines and rules. A small rasterizer built on
`golang.org/x/image/vector` draws that SVG. It accepts only the elements
go-tex/math writes and reports anything else as an error. The image is
cropped to its ink, drawn in the caller's color on a transparent background,
and depends only on the formula, color, and size. Everything is pure Go.

The font and go-tex/math's glyph cache are shared by all sessions. Both are
read-only or guarded by a mutex, and the cache grows only with the pixel
sizes in use, which are limited to 4–256 pixels per em.

go-tex/math v0.50.0 builds a delimiter that is taller than the font's largest
ready-made size (around a matrix of four or more rows, for example) from
parts, and stacks the parts in the wrong places. `renderMathImage` detects
these parts in the SVG and returns `errTallDelimiter` instead, so the caller
can show the formula as text.

## Rendering

`layout.go` parses the whole article once with Goldmark and renders it with
Glamour's ANSI renderer. Parsing the article as a whole keeps reference-style
links working across blocks.

Glamour wraps text at the document margin after a block has already been
laid out. For block quotes and code this loses the `│ ` quote prefix or the
indentation on continuation lines. To avoid that, `layout.go` does the
wrapping itself:

- It reserves the document margin on both sides.
- Block quotes are rendered at the width left over after their `│ ` prefix.
- Code is wrapped before syntax highlighting, leaving room for its
  indentation.

A final check measures every line using display width (East Asian wide
characters count as two cells). Lines that fit are left exactly as they are.
If a line still overflows, it is wrapped and each continuation keeps the
line's quote prefix or indentation.

Code and display math wrap instead of scrolling horizontally. Wrapped
continuation lines start with a dim `↪` in the gutter; real line breaks in
the source have no marker. Spaces inside quoted arguments are preserved when
wrapping. Because of the markers, wrapped commands can't be copied directly;
the web version has the original lines.

Each theme in `theme.go` names a Chroma syntax palette (Paper uses GitHub)
and recolors Glamour's dark or light base style. Palettes are selected by name, because Glamour otherwise shares the first custom
Chroma palette that gets registered across all renderers. Tables keep their
styling.

Each session caches up to 16 rendered articles. The cache key is the slug,
the language actually shown, the width, the theme, and the client's color
profile.

## Theme detection

All styling depends on the connecting client: its PTY `TERM`, its
environment, and the color profile Bubble Tea detects for it. The server's
own terminal is never consulted. Rendering has no side effects and adapts
colors explicitly for each client.

Bubble Tea sends an OSC 11 query (`ESC ] 11 ; ? ST`) to the client at
startup and reads the reply from that session's input. The `auto` theme
renders as Rosé Pine immediately and switches to Rosé Pine Dawn if the reply
reports a light background. If no reply arrives, it stays dark; it never waits
for one. The reply is remembered even when another theme is chosen, so
picking `auto` later with `t` still follows the terminal.

The picker changes the session's theme as the cursor moves, so the chrome
previews it at once. The article is rerendered only when the picker closes:
`enter` keeps the highlighted theme and `esc` restores the previous one.

## Sessions

`model.go` holds everything that changes during a session: navigation
position, filter, theme, render cache, and the Bubbles viewport. A new copy
is created for every session, so sessions never share mutable state. The
post catalog is built once at startup and shared read-only. Each render
creates its own Markdown AST and copy of the style configuration.

## SSH server

`server.go` connects the model to Wish and only allows interactive PTY
shells. Exec requests, subsystems, agent forwarding, and TCP forwarding in
either direction are rejected. Clients that don't request a PTY can be forced
to with `ssh -tt`.

`limits.go` enforces the connection limits:

- TCP connections are counted as soon as they are accepted, before the SSH
  handshake, so slow handshakes can't exceed the limit.
- Open session channels are counted separately, including channels that
  haven't started a shell yet.
- Per-IP bookkeeping is bounded in size.
- The per-IP rate limit uses a fixed one-minute window.

Timeouts:

- The SSH handshake must finish within 10 seconds.
- The idle timeout is measured from the last input the client sent. Screen
  updates from the server, such as a blinking cursor, don't count, so they
  can't keep an idle session open.
- The maximum duration applies even to clients that send keepalives.

On SIGINT or SIGTERM the server stops accepting connections, waits 3 seconds,
and then closes any remaining ones.

PTY sizes are limited to 1–512 columns and 1–256 rows, and resize events are
clamped to the same range. The size and number of client requests are also
limited.

The host key is generated on first start with owner-only permissions. Both
`.ssh/` and the built binary are git-ignored.

### PTY race workaround

Charm SSH 0.4.3 reads and writes `Pty.Window` without synchronization. A
client that resizes right after requesting a shell can trigger a data race.
`session.go` avoids this by copying the PTY details in the SSH request
goroutine before Wish starts, then delivering later sizes through the
session's resize channel. Each session gets its own copy, and Wish's
per-client terminal settings are preserved.

## Known limitations

**Rendering**

- Math is typeset as text, so it is coarser than on the website: scripts
  without a Unicode form are written as `x^{N×d}`, and tall delimiters are
  built from pieces. Display math that can't be broken to fit a narrow
  window, and the few constructs termtex doesn't support, are shown as LaTeX
  source.
- Display math inside a block quote or list item ends the quote or item,
  as it did before typesetting.
- Images are shown as captions and links, not drawn in the terminal. Other
  raw HTML is left to Glamour.
- Local image paths such as NITP's `imgs/nitp-overview.png` are turned into
  absolute URLs relative to the article. Astro may publish these images
  under hashed asset URLs instead, so the links may not work. The reader
  doesn't inspect the built site, and image links are not checked.
- Preprocessing is written for this blog's posts and common Markdown, not
  the full Markdown and HTML grammar. Code fences inside nested lists and
  unusual image attributes may need extra handling.
- Wide tables can wrap awkwardly, and Glamour may shorten table headings.
- Block quotes inside lists still use Glamour's own layout and may lose
  their prefixes when wrapped. Top-level quotes and quotes nested in quotes
  are handled.
- In a one-column window, wide characters are cut off. Very short windows
  hide parts of the header and footer.

**Features**

- No hot reload, full-text search, following links inside the reader, or
  horizontal scrolling.

**Server**

- The fixed-window rate limit allows a short burst at the boundary between
  two minutes.

**Untested**

- Colors, fonts, and OSC 11 replies have only been checked automatically.
  They still need to be reviewed by eye in several SSH clients.
