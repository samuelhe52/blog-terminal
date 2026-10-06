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

Some tests check exact facts about the fixture, such as the post counts.
Update them when you change the fixture on purpose.

## Checking the live posts

Two corpus-wide tests also run against real content when `BLOG_CONTENT_DIR`
is set: every post must fit at 40, 60, 80, and 120 columns in both themes,
and the final overflow guard must change nothing at 60, 80, and 120 columns.

```sh
BLOG_CONTENT_DIR=../Blog/src/content/posts go test ./...
```

Without the variable these tests check only the fixture. Run them with the
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
(counts, `gg`, half pages, the help screen), theme and color-profile
messages, cache size, resizing, and very small windows.

**Rendering.** Every fixture post is rendered at 40, 60, 80, and 120
columns in both themes, and every line is checked to fit. Width is measured in
display cells, with East Asian wide characters counting as two. Every post is
also rendered at 60, 80, and 120 columns to check that the final overflow
guard in `layout.go` changes **nothing**, which confirms that prose, quotes,
code, and tables already fit before the guard runs. Both checks also cover
the live posts when `BLOG_CONTENT_DIR` is set (see above).

Other rendering tests cover:

- quote and code prefixes on continuation lines
- inline math that has no added padding and doesn't split
- removal of display math delimiters
- footer hints staying on one line at 40, 60, 80, and 120 columns
- separate render caches per session
- the dim `↪` marker on wrapped lines, and its absence on real line breaks
- spaces inside quoted arguments surviving wrapping

**Search.** Finding all three fixture lectures from the root, folder paths
in results, matching on descriptions, showing each article once in the
preferred language, limiting search to the current folder, and the
apply / clear / reopen cycle. Real SSH sessions at 40 and 90 columns send the
combined Escape sequences described in [architecture.md](architecture.md),
type a new query, and open the setup article.

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

**Theme detection.** A real SSH test waits for the OSC 11 query, sends back a
white background, and checks that the light palette is used. A second session
that receives no reply must stay dark and still respond to input. The reply
is sent by the test, not by a real terminal, so detection in specific
terminal apps still needs to be checked by hand.
