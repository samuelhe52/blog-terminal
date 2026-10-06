# blog-terminal

Serve a directory of Markdown blog posts as a terminal UI over SSH.

Point blog-terminal at your posts and start the server. Visitors connect with
a plain `ssh` command and can browse folders, search, and read posts in
Chinese or English without installing anything or authenticating. It is built
on Charm's [Wish](https://github.com/charmbracelet/wish),
[Bubble Tea](https://github.com/charmbracelet/bubbletea), and
[Glamour](https://github.com/charmbracelet/glamour).

## Try it

```sh
ssh -p 2222 ssh-blog.konakona.dev
```

This is [konakona](https://blog.konakona.dev), the author's blog, served by
blog-terminal. Any username works, with no password or key. The host key
fingerprint is `SHA256:mlKN5LOJgyAMaqz2g+t7cqcUH1QOUled3/bWr1Fe3fM`.

## Serving your own posts

Requires Go 1.27.1 or later. Build it, then try it on the sample posts in
`testdata/fixture/`:

```sh
git clone https://github.com/samuelhe52/blog-terminal.git
cd blog-terminal
go build -o blog-terminal .

./blog-terminal local --content testdata/fixture
```

`local` runs the reader in the current terminal. To serve your own posts over
SSH, use `serve` and pass the directory, your site's URL, and your blog's
name:

```sh
./blog-terminal serve --content /path/to/posts \
    --site-url https://example.com --title "My Blog"
```

Without `--site-url` and `--title`, article links point to the konakona blog.
The posts must follow the layout in [Content format](#content-format).

With the server running, connect from another terminal:

```sh
ssh -p 2222 localhost
```

The server listens on `127.0.0.1:2222` by default. Use `--listen :2222` to
accept connections from other machines. It accepts any username without a
password or key. On first start it generates a host key at
`.ssh/host_ed25519`, relative to the working directory. Keep this file to
preserve the host fingerprint across restarts.

Posts are loaded once at startup, so the server must be restarted to pick up
edits. Example systemd units for a public server are in [`deploy/`](deploy/).

## Content format

The content directory holds one subdirectory per language. Both `zh/` and
`en/` must exist, though either can be empty. Subdirectories inside them
become folders in the reader. A folder is listed only if it directly contains
posts.

```text
posts/
├── en/
│   ├── hello.md
│   └── course-notes/
│       └── lecture-1.md
└── zh/
    └── hello.md
```

Every `.md` file starts with YAML frontmatter:

```yaml
---
title: "Hello"
description: "A short summary shown in lists and searched by /."
date: 2026-01-10
lang: "en"
translationSlug: "hello"
---
```

| Field | Required | Description |
| --- | --- | --- |
| `title` | Yes | Post title |
| `date` | Yes | Publication date (`YYYY-MM-DD`); lists are sorted newest first |
| `lang` | Yes | `en` for posts in `en/`, `zh-CN` for posts in `zh/` |
| `translationSlug` | Yes | Links translations of the same post, and forms the post's URL. May contain `/`. Must be unique within a language |
| `description` | No | Summary shown in lists and matched by search |
| `author` | No | Accepted but not shown |
| `draft` | No | `true` hides the post |

Any other frontmatter field is an error. An invalid post stops the reader from
starting, and the error names the file.

A post with no translation is shown in its original language, with a note.

Each post's footer links to `<site-url>/<lang>/posts/<translationSlug>/`, with
`<lang>` being `zh` or `en`. Relative links and images in posts resolve
against that URL. Your website needs to use the same URL layout for these
links to work.

## Keys

Navigation follows vim conventions. Press `?` in the app to show the key list.

| Keys | Action |
| --- | --- |
| `j` / `k` | Down / up |
| `l` / `h` | Open / back |
| `gg` / `G` | First / last item or line |
| `ctrl+d` / `ctrl+u` | Half page down / up |
| `ctrl+f` / `ctrl+b`, `space` | Page down / up |
| `5j`, `10G`, … | Repeat a motion, or go to line N |
| `/` | Filter by title and description; `↓`/`↑` move through matches |
| `y` | Copy the post's web link (via OSC 52, so it works over SSH) |
| `v` / `V` | Select lines; motions extend the selection, `y` copies it, `esc` leaves |
| `c` | Copy the code block at the top of the screen |
| `[` / `]` | Previous / next code block |
| `ctrl+l` | Switch between 中文 and English |
| `t` | Choose a theme; `j`/`k` preview, `enter` apply, `esc` cancel |
| `q` / `esc` | Leave the selection, or go back; `q` quits from the top level |
| `ctrl+c` | Quit |

Arrow keys, `enter`, `backspace`, `home`/`end`, and `pgup`/`pgdn` also work,
as do `ctrl+n`/`ctrl+p` and `ctrl+e`/`ctrl+y` for down and up.

## Options

`local` and `serve` accept the same flags; the SSH-related ones only apply to
`serve`. See `./blog-terminal serve --help` for details.

| Flag | Default | Description |
| --- | --- | --- |
| `--content` | `$BLOG_CONTENT_DIR`, then `./content` | Posts directory containing `zh/` and `en/` (required) |
| `--site-url` | `https://blog.konakona.dev` | Public site that article links point to (`BLOG_SITE_URL`) |
| `--title` | `konakona` | Blog name in the header (`BLOG_TITLE`) |
| `--lang` | from session | `zh` (or `zh-CN`) or `en`; overrides each visitor's locale (`BLOG_TERMINAL_LANG`) |
| `--theme` | `auto` | `auto`, `rose-pine`, `rose-pine-dawn`, `dracula`, `nord`, `gruvbox`, or `paper` |
| `--listen` | `127.0.0.1:2222` | SSH listen address |
| `--host-key` | `.ssh/host_ed25519` | Ed25519 host key path |
| `--idle-timeout` | `5m` | Disconnect after this long without input |
| `--max-duration` | `1h` | Maximum session length |
| `--max-sessions` | `32` | Maximum open sessions overall |
| `--max-connections` | `64` | Maximum TCP connections overall |
| `--per-ip` | `4` | Maximum concurrent connections per IP |
| `--rate` | `12` | Maximum new connections per IP per minute |

Without `--lang` or `BLOG_TERMINAL_LANG`, each session starts in the language
of the visitor's `LC_ALL` or `LANG`, in that order. `zh*` locales select
Chinese; anything else selects English. To forward your locale over SSH, add
`SendEnv LANG LC_ALL` to your SSH client config. The server ignores all other
client variables except `COLORTERM`, `NO_COLOR`, `CLICOLOR`, and
`CLICOLOR_FORCE`, which control color output.

Themes only recolor text; the terminal's own background always shows through.
With `auto`, the reader queries the terminal's background color and uses Rosé
Pine until a reply arrives, or Rosé Pine Dawn if the background is light. Not
every terminal replies; `t` opens a picker that previews each theme across the
whole screen. The choice lasts for the session.

## Differences from the website

- Math is shown as LaTeX source rather than typeset.
- Images are shown as captions with links.
- Long code lines wrap with a `↪` marker instead of scrolling horizontally.
  Copying with `c`, or by selecting lines with `v` and `y`, gives the
  original unwrapped lines.
- Search matches titles and descriptions, not post content.

Each post's footer links to its page on the website.

## Development

```sh
go vet ./...
go test ./...
```

Tests run against a small made-up blog in `testdata/fixture/`, so no real
posts are needed.

When `--content` and `BLOG_CONTENT_DIR` are not set, the reader uses
`./content` if it exists. This path is git-ignored, so you can link your posts
there once:

```sh
ln -s ../Blog/src/content/posts content
```

With the link in place (or `BLOG_CONTENT_DIR` set), the tests also check
rendering of those posts. See [docs/testing.md](docs/testing.md) for details,
and [docs/architecture.md](docs/architecture.md) for an overview of the code.

## License

MIT. See [LICENSE](LICENSE).
