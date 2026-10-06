# blog-terminal

Read the [konakona blog](https://blog.konakona.dev) over SSH.

blog-terminal is a small Go server that serves the blog's Markdown posts as a
terminal UI. Visitors run `ssh`, browse the folders, and read posts in Chinese
or English. They don't need to install anything or create an account. It's
built on Charm's [Wish](https://github.com/charmbracelet/wish),
[Bubble Tea](https://github.com/charmbracelet/bubbletea) and
[Glamour](https://github.com/charmbracelet/glamour).

The posts come straight from the [Blog repo](https://github.com/samuelhe52/Blog).
No separate copy of the content is kept.

## Try it locally

You need Go 1.27 and a checkout of the Blog repo next to this one.

```sh
go build -o blog-terminal .
export BLOG_CONTENT_DIR=../Blog/src/content/posts

./blog-terminal local    # open the reader in this terminal
./blog-terminal serve    # or start an SSH server on 127.0.0.1:2222
```

With the server running, connect from another terminal:

```sh
ssh -p 2222 localhost
```

Any username works. The server doesn't ask for a password or key. On first
start it creates a host key at `.ssh/host_ed25519`. Keep that file to keep
the same host fingerprint.

Posts are loaded once at startup. If you edit a post, restart the server to
see the change.

## Keys

Navigation works like vim. Press `?` in the app to see this list.

| Keys | Action |
| --- | --- |
| `j` / `k` | Down / up |
| `l` / `h` | Open / go back |
| `gg` / `G` | Jump to top / bottom |
| `ctrl+d` / `ctrl+u` | Half page down / up |
| `ctrl+f` / `ctrl+b`, `space` | Page down / up |
| `5j`, `10G`, … | Repeat a motion or jump to line N |
| `/` | Search titles and descriptions |
| `ctrl+l` | Switch between 中文 and English |
| `t` | Toggle light / dark theme |
| `q` / `esc` | Go back (`q` quits from the top level) |
| `ctrl+c` | Quit |

Arrow keys, `enter`, `backspace`, `home`/`end` and `pgup`/`pgdn` work too.

## Options

`local` and `serve` take the same flags. The SSH flags only matter for
`serve`. Run `./blog-terminal serve --help` for the full list.

| Flag | Default | What it does |
| --- | --- | --- |
| `--content` | `$BLOG_CONTENT_DIR` | Posts directory, the one containing `zh/` and `en/` (required) |
| `--lang` | from the session | `zh` or `en` |
| `--theme` | `auto` | `auto`, `dark`, or `light` |
| `--listen` | `127.0.0.1:2222` | SSH listen address |
| `--host-key` | `.ssh/host_ed25519` | Ed25519 host key path |
| `--idle-timeout` | `5m` | Disconnect after this much inactivity |
| `--max-duration` | `1h` | Longest allowed session |
| `--max-sessions` | `32` | Open sessions across all clients |
| `--max-connections` | `64` | TCP connections across all clients |
| `--per-ip` | `4` | Concurrent connections per IP |
| `--rate` | `12` | New connections per IP per minute |

If `--lang` isn't set, the reader looks at `BLOG_TERMINAL_LANG`, then
`LC_ALL`, then `LANG`. Any `zh*` locale picks Chinese and everything else
picks English. To pass your locale over SSH, add `SendEnv LANG LC_ALL` to
your SSH config.

With `--theme auto`, the reader asks your terminal for its background color
and starts in dark mode until it gets an answer. Some terminals never reply.
Press `t` to switch manually.

## What to expect

Terminals have limits, so a few things look different from the website:

- Math shows as LaTeX source and isn't typeset.
- Images appear as a caption and a link.
- Long code lines wrap and get a `↪` marker instead of scrolling sideways.
  Copy commands from the web version, which has the original lines.
- Search covers titles and descriptions only, not the full post text.

The footer of each post links to the same post on the website.

## Development

```sh
go vet ./...
go test ./...
```

The tests run against a snapshot of the blog's posts in `testdata/posts/`,
so you don't need a Blog checkout to run them. [docs/testing.md](docs/testing.md)
explains how to refresh the snapshot and the reference captures.
[docs/architecture.md](docs/architecture.md) covers how the code is organized.

Deployment to a public host isn't set up yet.

## License

The code is MIT. The post snapshot and captures under `testdata/` are
CC BY 4.0. See [LICENSE](LICENSE).
