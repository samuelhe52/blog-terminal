# Deploying with systemd

These units run blog-terminal as a sandboxed systemd service. They are the
setup used for `ssh-blog.konakona.dev`; adjust the paths and flags for your
own server.

| File | Purpose |
| --- | --- |
| `blog-terminal.service` | Runs `blog-terminal serve` on port 2222 |
| `blog-terminal-reload.path` | Watches `/srv/blog-terminal/deployed` and restarts the reader when it changes |
| `blog-terminal-reload.service` | The restart triggered by the path unit |
| `blog-terminal-update.timer` | Runs the update service 5 minutes after boot and every 15 minutes after that |
| `blog-terminal-update.service` | Builds and tests the latest `main`, and installs the binary if it changed |
| `update.sh` | The build script the update service runs |

## Paths

| Path | Contents |
| --- | --- |
| `/usr/local/bin/blog-terminal` | The binary |
| `/usr/local/libexec/blog-terminal-update` | `update.sh` |
| `/srv/blog-terminal/posts` | The content directory, with `zh/` and `en/` |
| `/srv/blog-terminal/deployed` | Any file; touch it after updating the posts |
| `/var/lib/blog-terminal/host_ed25519` | The host key, created on first start |

The update service also expects Go in `/usr/local/go/bin`. Both services use
`DynamicUser`, so no user account needs to be created.

## Installing

Build and install the binary and the update script, then copy the units and
enable them:

```sh
go build -trimpath -o blog-terminal .
sudo install -m 0755 blog-terminal /usr/local/bin/blog-terminal
sudo install -D -m 0755 deploy/update.sh /usr/local/libexec/blog-terminal-update

sudo install -d /srv/blog-terminal/posts
sudo cp -R /path/to/posts/. /srv/blog-terminal/posts/

sudo cp deploy/*.service deploy/*.path deploy/*.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now blog-terminal.service blog-terminal-reload.path
sudo systemctl enable --now blog-terminal-update.timer   # optional
```

`blog-terminal.service` passes only `--content`, `--listen`, and
`--host-key`. Add `--site-url` and `--title` to its `ExecStart` for your
blog. Open port 2222 in your firewall.

## Updating posts

Posts are read once at startup. After copying new posts into
`/srv/blog-terminal/posts`, touch the trigger file and the path unit restarts
the reader:

```sh
sudo touch /srv/blog-terminal/deployed
```

The konakona blog does this from its own deploy workflow.

## Automatic updates

The update timer fetches `main` from
`https://github.com/samuelhe52/blog-terminal.git` every 15 minutes. If the
revision changed and `go test ./...` passes, it installs the new binary as
root and restarts the reader. Anything merged into `main` therefore reaches
the server within about 15 minutes. If you deploy your own instance, point
`update.sh` at a repository you control, or leave the timer disabled.
