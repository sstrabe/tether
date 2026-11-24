#!/usr/bin/env bash
# Puts the patched Tailscale source in third_party/tailscale (gitignored), which go.mod's replace points at.
# The version is the one go.mod requires; patches/*.patch are applied in order. Re-run after changing either.
set -euo pipefail
cd "$(dirname "$0")/.."
ver=$(awk '$1=="tailscale.com" && $2 ~ /^v/ {print $2; exit}' go.mod)
[ -n "$ver" ] || { echo "no tailscale.com version in go.mod" >&2; exit 1; }
stamp="$ver $(cat patches/*.patch | shasum -a 256 2>/dev/null || cat patches/*.patch | sha256sum)"
dst=third_party/tailscale
if [ -f "$dst/.tether-stamp" ] && [ "$(cat "$dst/.tether-stamp")" = "$stamp" ]; then
	exit 0
fi
dir=$(GOFLAGS=-mod=mod go mod download -json "tailscale.com@$ver" | sed -n 's/.*"Dir": "\(.*\)".*/\1/p')
[ -d "$dir" ] || { echo "download of tailscale.com@$ver failed" >&2; exit 1; }
rm -rf "$dst"
mkdir -p third_party
cp -R "$dir" "$dst"
chmod -R u+w "$dst"
for p in patches/*.patch; do
	patch -s -p1 -d "$dst" < "$p"
done
echo "$stamp" > "$dst/.tether-stamp"
echo "tailscale.com $ver patched in $dst"
