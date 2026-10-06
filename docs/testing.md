# Testing

```sh
go vet ./...
go test ./...
go test -race ./...
go test -short ./...   # skips only the binary CLI integration test
```

## The post snapshot

Tests run against `testdata/posts/`, a snapshot of the Blog's posts (taken at
Blog commit `365b1cd`), so they don't need a Blog checkout and don't break
when new posts are published. Some tests assert facts about that snapshot
(post counts, the seven CS50 lectures). The real-corpus count is
intentionally asserted so new content requires a review of the listing
checks.

To refresh it:

```sh
rsync -a --delete --include='*/' --include='*.md' --exclude='*' --prune-empty-dirs \
  ../Blog/src/content/posts/ testdata/posts/
go test ./...   # update the asserted counts if they changed
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

## Captures

The four ANSI-stripped captures in `testdata/captures/` include the actual
reader header, the **entire** rendered scrollable body, and footer at the
initial 0% position. They are expanded reader transcripts, not a single
screen or recordings of SSH cursor updates:

- `zh-ttt-80.txt`: Chinese TTT article, math and raw HTML images.
- `zh-ttt-60.txt`: The same Chinese article at 60 columns.
- `en-cs50-knowledge-80.txt`: English CS50 note, truth tables and code.
- `en-qwen38-80.txt`: English Qwen reproduction, long code, tables, diagrams.

After reviewing intentional changes to the source corpus or presentation,
regenerate them:

```sh
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

## Coverage

The suite tests schema errors, draft exclusion, pairs, real-corpus language
listings and folders, fallback behavior, URLs, Markdown hazards, navigation,
filtering, translation toggles, vim keys (counts, `gg`, half pages, help),
background/profile messages, cache bounds, resize, and tiny windows.

### Rendering

Every one of the 31 published posts (10 Chinese, 21 English) renders at 40,
60, 80, and 120 columns in both themes: 248 combinations. Width uses
ANSI-aware Unicode display cells, counting East Asian wide characters as two.
An additional 186 combinations (all posts at 60/80/120, both themes) assert
that the final guard makes **no change** to the complete rendered output,
including prose, quotes, code, and tables. Regression tests also check
quote/code continuation prefixes, unpadded and atomic math,
display-delimiter removal, single-line hints at 40/60/80/120, and independent
session caches. Code/math tests check dim soft-wrap markers, unmarked source
newlines, and preservation of spaces inside quoted arguments.

### Search

Search regressions cover all seven Lecture posts from root, folder paths,
description matching, preferred-language deduplication, folder scope, and the
Escape/apply/reopen lifecycle. Real SSH sessions at 40 and 90 columns send
adjacent Escape sequences followed by a new query and open the Qwen article.

### SSH

`TestServeSSHCLI` builds the actual binary, runs `serve` on a free loopback
port with a temporary persisted host key, then connects using the real
`golang.org/x/crypto/ssh` client without authentication. It requests a PTY,
checks the home, filters/opens a real post, toggles its translation, resizes,
scrolls to the end, quits, verifies prohibited requests are rejected, checks
host key permissions, and exercises SIGTERM shutdown.

Other SSH tests cover Chinese session language, idle/duration timeouts, a
blinking filter's idle timeout, and session/connection caps. Rate/global-cap
accounting has unit tests.

A real SSH test observes the OSC 11 query, injects a white-background reply,
checks the resulting light palette, and confirms a second session without a
reply independently stays dark and accepts input. This validates the query
path with an injected response rather than a physical terminal's automatic
reply.
