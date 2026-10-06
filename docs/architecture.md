# Architecture

Built with Go 1.27 and the Charm v2 stack: Wish 2.0.5, Bubble Tea 2.0.10,
Bubbles 2.2.1, Glamour 2.0.1, and Lip Gloss 2.0.6 (`charm.land/.../v2` import
paths). Versions are pinned in `go.mod`/`go.sum`.

| File | Responsibility |
| --- | --- |
| `main.go` | CLI modes (`local`, `serve`) and flags |
| `content.go` | Loading, validating, and pairing posts; folder structure |
| `markdown.go` | Preprocessing Markdown for the terminal |
| `layout.go` | Rendering Markdown to width-safe ANSI |
| `model.go` | Per-session Bubble Tea model: navigation, filter, theme, cache |
| `server.go` | Wish server and SSH request restrictions |
| `limits.go` | Connection, session, and rate limits |
| `session.go` | PTY snapshotting around a Charm SSH race |

## Content loading

`content.go` recursively reads only `.md` files, checks the YAML schema and
directory language, excludes drafts, rejects duplicate language/slug pairs,
and sorts newest first. Errors include the source filename and abort startup.
Folders come from source file paths; pairing and URLs use `translationSlug`,
including its folder segments. URLs use `https://blog.konakona.dev/zh/posts/`
and `/en/posts/`, with trailing slashes.

## Language fallback

Each home lists its own language's root posts and folders merged from both
languages, matching the Blog's `src/pages/{zh,en}/index.astro`. Inside a
folder, preferred-language content wins as a whole; untranslated siblings are
not mixed into an otherwise available structure. If the structure is empty,
the TUI shows the other language with a notice. The site's folder page
currently displays a notice/link instead of rendering that fallback
structure; the reader displays it directly so visitors can continue browsing.

English article routes can fall back to Chinese. `ctrl+l` keeps the requested
language globally and the same slug, even for an English-only article viewed
in Chinese. The footer links to the available original, since the site has no
Chinese route for English-only posts. Empty intermediate folders without
direct posts follow `folders.ts` and are not exposed in the listing (the
current corpus has none).

## Navigation and search

While editing a filter, every printable key (`h`, `l`, `t`, `q`, `?`, digits)
is text. Queries match titles and descriptions case-insensitively. Enter
applies the query and closes editing; Escape clears and closes it. With an
applied query, `/` reopens it without inserting a slash, and Escape clears it.
Adjacent legacy Escape sequences (`Alt+Escape`, `Alt+/`) also cancel the old
query, so rapid SSH/tmux input cannot leave the editor stuck.

Search results at root include each article once in the preferred language,
falling back to its original if untranslated, and show their folder paths.
Inside a folder, search keeps that folder's listing and language fallback
rules.

`q` / `esc` go back in order: close the article, clear an applied filter, then
go to the parent folder. At the top level `q` quits and `esc` does nothing.
A count prefix or pending `g` is cancelled by `esc`.

## Screen layout

Descriptions are shortened in the listing; article titles wrap in the reader.
The footer shows the article's available-language web URL and scroll
percentage. Resize reflows the article while keeping approximately the same
scroll percentage; switching a translation does the same. The header shows
the domain and a right-aligned `中文` / `English` preference; article metadata
shows only the date. Footer hints occupy one line at every width, omitting
lower-priority keys when space is tight. All bindings remain active even when
omitted.

## Markdown preprocessing

`markdown.go` preprocesses content outside fences, indented code, and code
spans. Raw HTML images become literal `[Image: alt]` captions plus absolute
URLs. Markdown inline/reference destinations resolve against the article's
web URL. Inline math becomes literal code preserving dollar delimiters and
TeX punctuation without padded code-span spaces. Non-breaking spaces inside
formulas and zero-cell discretionary boundaries around them keep a fitting
formula together, including next to CJK text; layout markers are removed
after wrapping. Display math becomes a fenced text block with its `$$`
delimiters removed.

## Rendering

`layout.go` parses the full Markdown tree with Goldmark, then uses Glamour's
public ANSI renderer. It reserves the style's document margin on both sides,
renders quoted blocks inside the width left after their `│ ` prefixes, and
wraps code before highlighting with space reserved for its indentation. This
avoids Glamour rewrapping completed quotes/code at the document margin and
losing their continuation prefixes. Cross-block reference links still resolve
because parsing happens once for the complete article.

The final ANSI-aware guard leaves fitting lines untouched; unexpected overflow
keeps the line's quote prefix or indentation on every continuation. Wide code
and formulas wrap rather than scroll horizontally. Soft continuations of code
and display math carry a dim `↪` gutter marker; actual source newlines have no
marker. Wrapping preserves spaces inside quoted arguments.

Syntax-colored code and tables remain styled. Dark/light code use the named
Dracula/GitHub syntax palettes, avoiding Glamour's shared first-registered
custom Chroma palette. Each session has a bounded 16-entry render cache keyed
by slug, actual content language, width, theme, and client color profile.

## Theme detection

Styles use the session's PTY `TERM`, environment, and Bubble Tea
color-profile messages, never the server terminal's global renderer.
Rendering is pure and colors are explicitly adapted per client.

In `auto`, Bubble Tea writes an OSC 11 query (`ESC ] 11 ; ? ST`) to the SSH
session output and parses the terminal's response from that session's input.
The model starts dark immediately and switches according to the reply's
luminance. With no reply it stays dark; it never waits for a reply to render.
A manual toggle or forced theme prevents later replies from changing it.

## Sessions and the SSH server

`model.go` owns each session's navigation, filter, theme, cache, and Bubbles
viewport. All of that mutable state is created separately per session. The
catalog and post objects are shared read-only after startup; renderer ASTs
and style configuration copies are created per render.

`server.go` wires that model to Wish and restricts SSH requests to
interactive PTY shells. Exec, subsystems, agent forwarding, direct TCP
forwarding, and reverse forwarding are rejected. If a client can't infer an
interactive terminal, force PTY allocation with `ssh -tt`.

`limits.go` accounts for TCP connections before the SSH handshake and open
session channels (including channels awaiting a shell), with bounded IP
bookkeeping. The per-IP rate limit uses a fixed one-minute window. Session
idle time counts incoming client input so cursor repaints cannot keep a
reader alive; absolute lifetime also bounds clients that send keepalives.
Handshakes time out after 10 seconds. SIGINT/SIGTERM stop accepting
connections and allow a 3-second grace period before closing active
connections. PTYs are limited to 1–512 columns and 1–256 rows; resize events
are clamped to those bounds. Client request payloads and request counts are
also bounded.

The host private key is generated once with restrictive permissions; `.ssh/`
and the built binary are git-ignored.

`session.go` snapshots accepted PTY metadata in the SSH request goroutine
before starting Wish, then delivers later sizes through the session's resize
channel. This avoids Charm SSH 0.4.3's unsynchronized `Pty.Window`
reads/writes when a client resizes immediately after requesting its shell.
The snapshot is independent for every session and preserves Wish's
per-client terminal configuration.

## Known limitations

Math is readable literal LaTeX, not typeset mathematics. Images are captions
and links, not terminal graphics; other arbitrary HTML is left to Glamour's
normal handling. Source-local images such as NITP's `imgs/nitp-overview.png`
resolve to absolute article-relative URLs; Astro may publish these under
hashed asset URLs instead. This module does not inspect a built site or
discover those asset mappings, and image reachability is not verified.

The preprocessor targets this corpus and common Markdown rather than the full
Markdown/HTML grammar; unusual nested list fences, exotic image attributes,
and ambiguous paired currency dollars can need additional handling. Wide
tables may wrap awkwardly and Glamour can shorten table headings.
Code/formula continuation lines are presentation wraps and should not be
copied as executable source. At a one-column window a two-cell glyph is
clipped; very short windows necessarily hide some chrome. There is no hot
reload, full-body search, in-reader link navigation, or horizontal code
scrolling. The fixed-window rate limit is deliberately small and can allow a
burst at a minute boundary.

Quote layout handles quote blocks at the document level and quotes nested
within quotes; quotes embedded in lists still use Glamour's native layout and
may need further handling. Color/font appearance and OSC handling across
different SSH clients still need human visual review.
