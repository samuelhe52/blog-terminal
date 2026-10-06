# Testing

```sh
go vet ./...
go test ./...
go test -race ./...
go test -short ./...   # skip the test that builds and runs the binary
```

## Post snapshot

The tests read posts from `testdata/posts/`, a copy of the Blog's posts taken
at Blog commit `365b1cd`. This keeps the tests independent of a Blog checkout
and stops new posts from breaking them.

Some tests check facts about this snapshot, such as the number of posts and
the seven CS50 lectures. The counts are deliberate: after the snapshot is
refreshed, a failing count is a reminder to check that the new posts appear
correctly in the listings.

To refresh the snapshot from a Blog checkout next to this repo:

```sh
rsync -a --delete --include='*/' --include='*.md' --exclude='*' --prune-empty-dirs \
  ../Blog/src/content/posts/ testdata/posts/
go test ./...   # update the expected counts if they changed
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

## Captures

`testdata/captures/` holds four reference transcripts of the reader with ANSI
codes removed. Each one contains the header, the **entire** article body, and
the footer as it looks at the top of the article. They are full transcripts
of the article, not a single screenful or a recording of the SSH output.

- `zh-ttt-80.txt`: the Chinese TTT article at 80 columns, with math and HTML
  images.
- `zh-ttt-60.txt`: the same article at 60 columns.
- `en-cs50-knowledge-80.txt`: an English CS50 note with truth tables and
  code.
- `en-qwen38-80.txt`: the English Qwen reproduction post, with long code,
  tables, and diagrams.

When a change to the posts or to the rendering is intended, review the
difference and then regenerate the captures:

```sh
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

## What the tests cover

**Content and navigation.** Frontmatter validation, draft exclusion,
translation pairing, listings and folders for the real posts, language
fallback, and URLs. Navigation, filtering, switching translations, vim keys
(counts, `gg`, half pages, the help screen), theme and color-profile
messages, cache size, resizing, and very small windows.

**Rendering.** Every one of the 31 posts in the snapshot (10 Chinese, 21
English) is rendered at 40, 60, 80, and 120 columns in both themes, 248
combinations in total, and every line is checked to fit. Width is measured in
display cells, with East Asian wide characters counting as two.

A further 186 combinations (all posts at 60, 80, and 120 columns, both
themes) check that the final overflow guard in `layout.go` changes
**nothing**. This confirms that prose, quotes, code, and tables already fit
before the guard runs.

Other rendering tests cover:

- quote and code prefixes on continuation lines
- inline math that has no added padding and doesn't split
- removal of display math delimiters
- footer hints staying on one line at 40, 60, 80, and 120 columns
- separate render caches per session
- the dim `↪` marker on wrapped lines, and its absence on real line breaks
- spaces inside quoted arguments surviving wrapping

**Search.** Finding all seven CS50 lecture posts from the root, folder paths
in results, matching on descriptions, showing each article once in the
preferred language, limiting search to the current folder, and the
apply / clear / reopen cycle. Real SSH sessions at 40 and 90 columns send the
combined Escape sequences described in [architecture.md](architecture.md),
type a new query, and open the Qwen article.

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
