# Testing

```sh
go vet ./...
go test ./...
go test -race ./...
go test -short ./...   # skip the test that builds and runs the binary
```

## Fixture blog

The tests read posts from `testdata/fixture/`, a small made-up blog with the
same layout and frontmatter as the Blog's `src/content/posts`. None of it is
real post content, so the repo doesn't need to change when posts are
published. It has 12 published posts (4 Chinese, 8 English) chosen to cover
the cases that matter:

- paired posts in both languages, a Chinese-only post, and drafts
- an English-only folder (`course-notes/`, three lectures and a project) that
  both home listings must show, and a second folder (`reference/`) whose file
  name differs from its slug
- long Chinese paragraphs, inline and display math, raw `<img>` tags, quotes
  with long links, tables with Chinese cells, long shell commands with quoted
  arguments, and `$` signs in code that are not math
- relative, reference-style, and autolink URLs
- a small PNG next to a post (`reference/imgs/pipeline.png`), and an SVG in
  `testdata/assets/` for the site-absolute `<img>` in the attention notes

Some tests check exact facts about the fixture, such as the post counts.
Update them when you change the fixture on purpose.

## Checking the live posts

Corpus-wide tests also run against real content when `BLOG_CONTENT_DIR`
is set or a `./content` link exists (see the README): every post must fit at 40, 60, 80, and 120 columns in a dark and a
light theme, and the final overflow guard must change nothing at 60, 80, and
120 columns in every theme. Source mode is checked against the same posts.

```sh
ln -s ../Blog/src/content/posts content   # once
go test ./...
```

Without either, these tests check only the fixture. Run them with the
live posts before deploying, or after publishing posts with unusual content.

## Captures

`testdata/captures/` holds four reference transcripts of the reader with ANSI
codes removed. Each one contains the header, the **entire** article body, and
the footer as it looks at the top of the article. They are full transcripts
of the article, not a single screenful or a recording of the SSH output.

- `zh-attention-80.txt`: the Chinese attention notes at 80 columns, with
  math, an HTML image, a quote, and a table.
- `zh-attention-60.txt`: the same post at 60 columns, where long formulas
  wrap with `↪`.
- `en-lecture-1-80.txt`: an English lecture with a truth table and code.
- `en-server-setup-80.txt`: the English setup post, with long shell commands,
  a nested list, and wide tables.

When a change to the fixture or to the rendering is intended, review the
difference and then regenerate the captures:

```sh
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

## What the tests cover

**Content and navigation.** Frontmatter validation, draft exclusion,
translation pairing, listings and folders for the fixture blog, the `--site-url` and `--title` overrides, language
fallback, and URLs. Navigation, filtering, switching translations, vim keys
(counts, `gg`, half pages, the help screen), the theme picker's preview,
apply, and cancel, theme and color-profile
messages, cache size, resizing, and very small windows.

**Rendering.** Every fixture post is rendered at 40, 60, 80, and 120
columns in a dark and a light theme, and every line is checked to fit. Width is measured in
display cells, with East Asian wide characters counting as two. Every post is
also rendered at 60, 80, and 120 columns to check that the final overflow
guard in `layout.go` changes **nothing**, which confirms that prose, quotes,
code, and tables already fit before the guard runs. Both checks also cover
the live posts when `BLOG_CONTENT_DIR` is set (see above).

Source mode is checked the same way: every post's source fits at 40, 60,
80, and 120 columns in every theme, the final guard changes nothing, and
joining the `↪` continuations gives back every source line.

Other rendering tests cover:

- quote and code prefixes on continuation lines
- inline math that has no added padding and doesn't split
- removal of display math delimiters
- footer hints staying on one line at 40, 60, 80, and 120 columns
- separate render caches per session
- the dim `↪` marker on wrapped lines, and its absence on real line breaks
- spaces inside quoted arguments surviving wrapping
- source mode: the colors of each kind of Markdown syntax, CJK text and
  deep indentation wrapping at any width, control characters removed and
  links left unrewritten, colors adapted to ASCII and 256-color clients,
  fenced block ranges and text, separate cache entries, the header
  indicator, keeping the heading in view when toggling, and keeping or
  resetting the mode on translation, theme, resize, and close

**Search.** Finding all three fixture lectures from the root, folder paths
in results, matching on descriptions, showing each article once in the
preferred language, limiting search to the current folder, and the
apply / clear / reopen cycle. Real SSH sessions at 40 and 90 columns send the
combined Escape sequences described in [architecture.md](architecture.md),
type a new query, and open the setup article.

**Search inside a post.** Smartcase matching, match positions in display
cells for CJK text, and highlighting that keeps line widths and the colors
after a match. In the reader: incremental jumps, `enter` and `esc` (restoring
the scroll position), `n`/`N` with counts and wrapping, continuing from the
screen after scrolling away, matches found again after a resize, widths at
40, 60, 80, and 120 columns, no colors for clients without color, the
`esc`/`q` order, a coalesced `Alt+/`, and clearing when another article
opens. An SSH session opens a post, searches it, steps with `n`, and checks
that `esc` clears the search before `q` closes the article.

**SSH.** `TestServeSSHCLI` builds the binary and runs `serve` on a free
local port with a temporary host key. It then connects with the
`golang.org/x/crypto/ssh` client without authenticating and:

1. requests a PTY and checks the home screen,
2. filters for a post and opens it,
3. switches its translation,
4. resizes the window and scrolls to the end,
5. quits,
6. checks that forbidden requests are rejected,
7. checks the host key's file permissions,
8. sends SIGTERM and checks the shutdown.

Other SSH tests cover choosing Chinese from the session environment, the idle
and maximum-duration timeouts, the idle timeout while the filter's cursor is
blinking, and the session and connection limits. The rate limit and global
limits also have unit tests.

**Images.** Detection by terminal name, including old kitty versions,
WezTerm, and tmux; resolving sources, including `..` and symlinks that leave
the root, remote URLs, and site-absolute paths with and without `--assets`;
loading PNGs and SVGs (through a stand-in `rsvg-convert`), downscaling, and
warnings for broken files without stopping startup. Image blocks appear only
before captions that are outside code, and the caption and link stay the
same. Articles with images fit at 40, 60, 80, and 120 columns in a dark and
a light theme, and the final guard changes nothing at 60 and above; this also
covers the live posts, with `BLOG_ASSETS_DIR` pointing at the site's
`public/` for `/images`. Session tests cover sending each image once,
resizing a placement, and deleting the least recently shown image when ids
run out. `TestSSHKittyImages` opens a session with `TERM=xterm-kitty`,
answers the version and graphics queries as kitty would, and checks the
bytes sent: the image transmission, and placeholder cells that keep their
color and diacritics through Bubble Tea's renderer. Sessions that report
WezTerm, tmux, or nothing must not receive any graphics command.

**Theme detection.** A real SSH test waits for the OSC 11 query, sends back a
white background, and checks that `auto` switches to the light palette. A second session
that receives no reply must stay dark and still respond to input. The reply
is sent by the test, not by a real terminal, so detection in specific
terminal apps still needs to be checked by hand.
