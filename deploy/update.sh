#!/bin/sh
# Build and test the latest main when it differs from the installed revision.
# Runs unprivileged; blog-terminal-update.service installs the result.
set -eu
src="$STATE_DIRECTORY/src"
[ -d "$src/.git" ] || git clone -q https://github.com/samuelhe52/blog-terminal.git "$src"
cd "$src"
git fetch -q origin main
rev=$(git rev-parse FETCH_HEAD)
[ "$rev" = "$(cat "$STATE_DIRECTORY/installed" 2>/dev/null)" ] && exit 0
git reset -q --hard "$rev"
git clean -qfdx
go test ./...
go build -trimpath -ldflags="-s -w" -o "$STATE_DIRECTORY/pending.tmp" .
echo "$rev" >"$STATE_DIRECTORY/pending.rev"
mv "$STATE_DIRECTORY/pending.tmp" "$STATE_DIRECTORY/pending"
