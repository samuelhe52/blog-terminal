# blog-terminal

An SSH reader for the [konakona blog](https://blog.konakona.dev).

blog-terminal serves the blog's Markdown posts as a terminal UI over SSH.
Visitors connect with a plain `ssh` command and can browse folders, search,
and read posts in Chinese or English without installing anything or
authenticating. It is built on Charm's
[Wish](https://github.com/charmbracelet/wish),
[Bubble Tea](https://github.com/charmbracelet/bubbletea), and
[Glamour](https://github.com/charmbracelet/glamour), and reads posts directly
from a checkout of the [Blog repo](https://github.com/samuelhe52/Blog).

## Running locally

Requires Go 1.27 and a directory of posts laid out like the Blog repo's
`src/content/posts` (`zh/` and `en/` subdirectories of Markdown files with the
Blog's frontmatter). The reader holds no content of its own; any checkout,
export, or synced copy of that directory works.

Link the Blog's posts into the repo once. The link is git-ignored, always
reflects the Blog checkout, and is used whenever `--content` and
`BLOG_CONTENT_DIR` are not set:

```sh
ln -s ../Blog/src/content/posts content
```

```sh
go build -o blog-terminal .

./blog-terminal local    # run the reader in the current terminal
./blog-terminal serve    # run an SSH server on 127.0.0.1:2222
```

With the server running, connect from another terminal:

```sh
ssh -p 2222 localhost
```

The server accepts any username without a password or key. On first start it
generates a host key at `.ssh/host_ed25519`; keep this file to preserve the
host fingerprint across restarts.

Posts are loaded once at startup, so the server must be restarted to pick up
edits.

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
| `ctrl+l` | Switch between 中文 and English |
| `t` | Choose a theme; `j`/`k` preview, `enter` apply, `esc` cancel |
| `q` / `esc` | Back; `q` quits from the top level |
| `ctrl+c` | Quit |

Arrow keys, `enter`, `backspace`, `home`/`end`, and `pgup`/`pgdn` also work.

## Options

`local` and `serve` accept the same flags; the SSH-related ones only apply to
`serve`. See `./blog-terminal serve --help` for details.

| Flag | Default | Description |
| --- | --- | --- |
| `--content` | `$BLOG_CONTENT_DIR`, then `./content` | Posts directory containing `zh/` and `en/` (required) |
| `--site-url` | `https://blog.konakona.dev` | Public site that article links point to (`BLOG_SITE_URL`) |
| `--title` | `konakona` | Blog name in the header (`BLOG_TITLE`) |
| `--lang` | from session | `zh` or `en` |
| `--theme` | `auto` | `auto`, `rose-pine`, `rose-pine-dawn`, `dracula`, `nord`, `gruvbox`, or `paper` |
| `--listen` | `127.0.0.1:2222` | SSH listen address |
| `--host-key` | `.ssh/host_ed25519` | Ed25519 host key path |
| `--idle-timeout` | `5m` | Disconnect after this long without input |
| `--max-duration` | `1h` | Maximum session length |
| `--max-sessions` | `32` | Maximum open sessions overall |
| `--max-connections` | `64` | Maximum TCP connections overall |
| `--per-ip` | `4` | Maximum concurrent connections per IP |
| `--rate` | `12` | Maximum new connections per IP per minute |

Without `--lang`, the language is taken from `BLOG_TERMINAL_LANG`, `LC_ALL`,
or `LANG`, in that order. `zh*` locales select Chinese; anything else selects
English. To forward your locale over SSH, add `SendEnv LANG LC_ALL` to your
SSH client config.

Themes only recolor text; the terminal's own background always shows through.
With `auto`, the reader queries the terminal's background color and uses Rosé
Pine until a reply arrives, or Rosé Pine Dawn if the background is light. Not
every terminal replies; `t` opens a picker that previews each theme across the
whole screen. The choice lasts for the session.

## Differences from the website

- Math is shown as LaTeX source rather than typeset.
- Images are shown as captions with links.
- Long code lines wrap with a `↪` marker instead of scrolling horizontally.
  Copy commands from the website rather than from the terminal.
- Search matches titles and descriptions, not post content.

Each post's footer links to its page on the website.

## Development

```sh
go vet ./...
go test ./...
```

Tests run against a small made-up blog in `testdata/fixture/`, so no real
posts are needed. With the `content` link in place (or `BLOG_CONTENT_DIR`
set), they also check rendering of the live posts. See
[docs/testing.md](docs/testing.md) for details, and
[docs/architecture.md](docs/architecture.md) for an overview of the code.

Example systemd units, including a timer that rebuilds from `main`, are in
[`deploy/`](deploy/).

## License

MIT. See [LICENSE](LICENSE).
