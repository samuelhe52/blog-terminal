# blog-terminal

A read-only SSH reader for the bilingual [konakona blog](https://blog.konakona.dev)
([source](https://github.com/samuelhe52/Blog)). Visitors connect with plain
`ssh` and browse and read posts in a terminal UI; nothing to install.

Built with Go 1.27 and the Charm v2 stack: Wish 2.0.5, Bubble Tea 2.0.10,
Bubbles 2.2.1, Glamour 2.0.1, and Lip Gloss 2.0.6 (`charm.land/.../v2` import
paths). Versions are pinned in `go.mod`/`go.sum`.

## Run

The reader serves the Blog repo's Markdown directly. Point it at a checkout's
`src/content/posts` (the directory containing `zh/` and `en/`):

```sh
go build -o blog-terminal .
export BLOG_CONTENT_DIR=../Blog/src/content/posts   # or pass --content
./blog-terminal local    # run the TUI in this terminal, no SSH
./blog-terminal serve    # SSH server on 127.0.0.1:2222
```

In another terminal:

```sh
ssh -p 2222 localhost
# Force PTY allocation when SSH cannot infer an interactive terminal:
ssh -tt -p 2222 blog@127.0.0.1
```

Any username works, without a password or authorized key. Accept the local
host fingerprint on first connection. The private host key is generated once
at `.ssh/host_ed25519` (relative to the working directory) with restrictive
permissions; `.ssh/` and the built binary are git-ignored. Keeping this key
preserves the host fingerprint.

```sh
./blog-terminal local --lang zh --theme light
./blog-terminal serve --listen 127.0.0.1:2223 --lang en
BLOG_TERMINAL_LANG=zh ./blog-terminal local
./blog-terminal serve --content /srv/blog/posts --host-key /var/lib/blog-terminal/host_ed25519
./blog-terminal serve --help
```

Content is loaded once at startup; restart after editing posts.

## Flags

Both modes accept the same flags; SSH-specific flags apply to `serve`.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--content` | `$BLOG_CONTENT_DIR` (required) | Directory containing `zh/` and `en/` |
| `--lang` | session environment | `zh` or `en`; overrides `BLOG_TERMINAL_LANG`, then `LC_ALL`, then `LANG` |
| `--theme` | `auto` | Query the client's background; default dark without a response; `dark` / `light` force a theme |
| `--listen` | `127.0.0.1:2222` | SSH listen address |
| `--host-key` | `.ssh/host_ed25519` | Persistent Ed25519 private key |
| `--idle-timeout` | `5m` | Connection inactivity and session client-input timeout |
| `--max-duration` | `1h` | Maximum connection and session lifetime |
| `--max-sessions` | `32` | Global open session-channel cap, including channels awaiting a shell |
| `--max-connections` | `64` | Global TCP cap, including pending handshakes |
| `--per-ip` | `4` | Concurrent TCP connections per IP |
| `--rate` | `12` | Connection attempts per IP per minute, fixed window |

`zh*` locales select Chinese; everything else selects English. Over SSH, send
the environment with your client's `SendEnv LANG LC_ALL` configuration, or use
the server's override to test. Background queries may be unsupported; `t`
always gives a manual override. Styles use the session's PTY `TERM`, environment,
and Bubble Tea color-profile messages, never the server terminal's global
renderer. Rendering is pure and colors are explicitly adapted per client.
In `auto`, Bubble Tea writes an OSC 11 query (`ESC ] 11 ; ? ST`) to the SSH
session output and parses the terminal's response from that session's input.
The model starts dark immediately and switches according to the reply's
luminance. With no reply it stays dark; it never waits for a reply to render.
A manual toggle or forced theme prevents later replies from changing it.

## Keys

| Context | Keys | Action |
| --- | --- | --- |
| Everywhere | `ctrl+c` | Quit |
| Outside filter editing | `l`, `t` | Toggle language / light-dark theme |
| Home / folder | `j/k`, arrows | Select |
| Home / folder | `enter`, right arrow | Open post or folder |
| Home / folder | `h`, left arrow | Parent folder |
| Home / folder | `/` | Search titles/descriptions; all folders at root, current folder inside |
| Filter editing | `enter`, `esc` | Keep filter and return to navigation / clear filter |
| Reader | `j/k`, arrows | Scroll by line |
| Reader | `space`, `pgdn`, `b`, `pgup` | Page down / up |
| Both views | `g/G`, home/end | First / last position |
| Reader | `q`, `esc` | Back to listing |
| Home / folder | `q`, `esc` | Clear applied filter, then parent folder; `q` quits at root, `esc` stays |

While editing a filter, letters such as `l`, `t`, and `q` are text. Queries
match titles and descriptions case-insensitively. Enter applies the query and
closes editing; Escape clears and closes it. With an applied query, `/` reopens it without
inserting a slash, and Escape clears it. Adjacent legacy Escape sequences
(`Alt+Escape`, `Alt+/`) also cancel the old query, so rapid SSH/tmux input cannot
leave the editor stuck. Search results at root include each article once in
the preferred language, falling back to its original if untranslated, and show
their folder paths. Inside a folder, search keeps that folder's listing and
language fallback rules. Descriptions are shortened in the listing; article
titles wrap in the reader. The footer shows the article's available-language
web URL and scroll percentage. Resize
reflows the article while keeping approximately the same scroll percentage;
switching a translation does the same. The header shows the domain and a
right-aligned `中文` / `English` preference; article metadata shows only the
date. Footer hints occupy one line at every width, omitting lower-priority
keys when space is tight. All bindings remain active even when omitted.

## Content and architecture

`content.go` recursively reads only `.md` files, checks the YAML schema and
directory language, excludes drafts, rejects duplicate language/slug pairs,
and sorts newest first. Errors include the source filename and abort startup.
Folders come from source file paths; pairing and URLs use `translationSlug`,
including its folder segments. URLs use `https://blog.konakona.dev/zh/posts/`
and `/en/posts/`, with trailing slashes.

Each home lists its own language's root posts and folders merged from both
languages, matching the Blog's `src/pages/{zh,en}/index.astro`. Inside a folder, preferred
language content wins as a whole; untranslated siblings are not mixed into an
otherwise available structure. If the structure is empty, the TUI shows the
other language with a notice. The site's folder page currently displays a
notice/link instead of rendering that fallback structure; the reader displays
it directly so visitors can continue browsing. English article routes can fall
back to Chinese. `l` keeps the requested language globally and the same slug,
even for an English-only article viewed in Chinese. The footer links to the
available original, since the site has no Chinese route for English-only posts.
Empty intermediate folders without direct posts follow `folders.ts` and are
not exposed in the listing (the current corpus has none).

`markdown.go` preprocesses content outside fences, indented code, and code
spans. Raw HTML images become literal `[Image: alt]` captions plus absolute
URLs. Markdown inline/reference destinations resolve against the article's
web URL. Inline math becomes literal code preserving dollar delimiters and
TeX punctuation without padded code-span spaces. Non-breaking spaces inside
formulas and zero-cell discretionary boundaries around them keep a fitting
formula together, including next to CJK text; layout markers are removed after
wrapping. Display math becomes a fenced text block with its `$$` delimiters
removed.

`layout.go` parses the full Markdown tree with Goldmark, then uses Glamour's
public ANSI renderer. It reserves the style's document margin on both sides,
renders quoted blocks inside the width left after their `│ ` prefixes, and
wraps code before highlighting with space reserved for its indentation. This
avoids Glamour rewrapping completed quotes/code at the document margin and
losing their continuation prefixes. Cross-block reference links still resolve
because parsing happens once for the complete article. The final ANSI-aware
guard leaves fitting lines untouched; unexpected overflow keeps the line's
quote prefix or indentation on every continuation. Wide code and formulas
wrap rather than scroll horizontally. Soft continuations of code and display
math carry a dim `↪` gutter marker; actual source newlines have no marker.
Wrapping preserves spaces inside quoted arguments. For copying long commands,
use the linked web article rather than copying the rendered wrap markers.
Syntax-colored code and tables remain
styled. Dark/light code use the named Dracula/GitHub syntax palettes, avoiding
Glamour's shared first-registered custom Chroma palette. Each session has a bounded
16-entry render cache keyed by slug, actual content language, width, theme,
and client color profile.

`model.go` owns each session's navigation, filter, theme, cache, and Bubbles
viewport. All of that mutable state is created separately per session. The
catalog and post objects are shared read-only after startup; renderer ASTs and
style configuration copies are created per render. `server.go` wires that
model to Wish and restricts SSH requests to
interactive PTY shells. Exec, subsystems, agent forwarding, direct TCP
forwarding, and reverse forwarding are rejected. `limits.go` accounts for TCP
connections before the SSH handshake and open session channels, with bounded
IP bookkeeping. Session idle time counts incoming client input so cursor
repaints cannot keep a reader alive; absolute lifetime also bounds clients
that send keepalives. Handshakes time out after 10 seconds. SIGINT/SIGTERM stop
accepting connections and allow a 3-second grace period before closing active
connections. PTYs are limited to 1–512 columns and 1–256 rows; resize events
are clamped to those bounds. Client request payloads and request counts are
also bounded.

`session.go` snapshots accepted PTY metadata in the SSH request goroutine
before starting Wish, then delivers later sizes through the session's resize
channel. This avoids Charm SSH 0.4.3's unsynchronized `Pty.Window` reads/writes
when a client resizes immediately after requesting its shell. The snapshot
is independent for every session and preserves Wish's per-client terminal
configuration.

## Verification and captures

Tests run against `testdata/posts/`, a snapshot of the Blog's posts (taken at
Blog commit `365b1cd`), so they don't need a Blog checkout and don't break when
new posts are published. Some tests assert facts about that snapshot (post
counts, the seven CS50 lectures). To refresh it:

```sh
rsync -a --delete --include='*/' --include='*.md' --exclude='*' --prune-empty-dirs \
  ../Blog/src/content/posts/ testdata/posts/
go test ./...   # update the asserted counts if they changed
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

```sh
go vet ./...
go test ./...
go test -race ./...
# After reviewing intentional changes to the source corpus or presentation:
UPDATE_CAPTURES=1 go test -run TestReaderCaptures
```

The suite tests schema errors, draft exclusion, pairs, real-corpus language
listings and folders, fallback behavior, URLs, Markdown hazards, navigation,
filtering, translation toggles, background/profile messages, cache bounds,
resize, and tiny windows. Every one of the 31 published posts (10 Chinese,
21 English) renders at 40, 60, 80, and 120 columns in both themes: 248
combinations. Width uses ANSI-aware Unicode display cells, counting East Asian
wide characters as two. An additional 186 combinations (all posts at
60/80/120, both themes) assert that the final guard makes **no change** to the
complete rendered output, including prose, quotes, code, and tables. Regression
tests also check quote/code continuation prefixes, unpadded and atomic math,
display-delimiter removal, single-line hints at 40/60/80/120, and independent
session caches. The real-corpus count is intentionally asserted so
new content requires a review of the listing checks.

Search regressions cover all seven Lecture posts from root, folder paths,
description matching, preferred-language deduplication, folder scope, and the
Escape/apply/reopen lifecycle. Real SSH sessions at 40 and 90 columns send
adjacent Escape sequences followed by a new query and open the Qwen article.
Code/math tests check dim soft-wrap markers, unmarked source newlines, and
preservation of spaces inside quoted arguments.

`TestServeSSHCLI` builds the actual binary, runs `serve` on a free loopback
port with a temporary persisted host key, then connects using the real
`golang.org/x/crypto/ssh` client without authentication. It requests a PTY,
checks the home, filters/opens a real post, toggles its translation, resizes,
scrolls to the end, quits, verifies prohibited requests are rejected, checks
host key permissions, and exercises SIGTERM shutdown. Other SSH tests cover
Chinese session language, idle/duration timeouts, a blinking filter's idle
timeout, and session/connection caps. A real SSH test observes the OSC 11
query, injects a white-background reply, checks the resulting light palette,
and confirms a second session without a reply independently stays dark and
accepts input. Rate/global-cap accounting has unit
tests. `go test -short` skips only the binary CLI integration test.

The four ANSI-stripped captures in `testdata/captures/` include the
actual reader header, the **entire** rendered scrollable body, and footer at
the initial 0% position. They are expanded reader transcripts, not a single
screen or recordings of SSH cursor updates:

- `zh-ttt-80.txt`: Chinese TTT article, math and raw HTML images.
- `zh-ttt-60.txt`: The same Chinese article at 60 columns.
- `en-cs50-knowledge-80.txt`: English CS50 note, truth tables and code.
- `en-qwen38-80.txt`: English Qwen reproduction, long code, tables, diagrams.

## Limitations

Math is readable literal LaTeX, not typeset mathematics. Images are captions
and links, not terminal graphics; other arbitrary HTML is left to Glamour's
normal handling. Source-local images such as NITP's `imgs/nitp-overview.png`
resolve to absolute article-relative URLs; Astro may publish these under
hashed asset URLs instead. This module does not inspect a built site or
discover those asset mappings, and image reachability is not verified.

The preprocessor targets this corpus and common Markdown rather than the full
Markdown/HTML grammar; unusual nested list fences, exotic image attributes,
and ambiguous paired currency dollars can need additional handling. Wide
tables may wrap awkwardly and Glamour can shorten table headings. Code/formula
continuation lines are presentation wraps and should not be copied as
executable source; use the web article for original lines. At a one-column
window a two-cell glyph is clipped; very short windows necessarily hide some
chrome. There is no hot reload, full-body search, in-reader link navigation,
or horizontal code scrolling. The fixed-window rate limit is deliberately
small and can allow a burst at a minute boundary.

The automated SSH tests validate the background query/reply path using an
injected terminal response, rather than a physical terminal's automatic reply.
Color/font appearance and OSC handling across different SSH clients still need
human visual review. Quote layout handles quote blocks at the document level
and quotes nested within quotes; quotes embedded in lists still use Glamour's
native layout and may need further handling. Deployment (port 22, DNS, service management) is not set up yet.

## License

Code is MIT; the post snapshot and captures under `testdata/` are CC BY 4.0. See [LICENSE](LICENSE).
