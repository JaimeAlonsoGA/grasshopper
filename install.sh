#!/bin/sh
# grasshopper — install and set up in one command.
#
#   curl -fsSL https://hopcli.dev/install.sh | sh
#
# The scheme is written out on purpose. .dev is HSTS-preloaded, but curl does not
# consult the preload list: without https:// the first request goes out in
# plaintext and the redirect it follows is attacker-controllable. For a URL whose
# body is piped into a shell, that is the entire threat model.
#
# Downloads the binary for this machine, puts it on PATH, and registers it with
# every agent it can find. No Go, no Homebrew, no tap. POSIX sh on purpose: this
# runs before anything of ours exists, so it may assume nothing.
set -eu

OWNER="${GRASSHOPPER_OWNER:-JaimeAlonsoGA}"
REPO="grasshopper"
PREFIX="${PREFIX:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'grasshopper: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) die "$(uname -s) is not supported yet — build from source: go build ./cmd/hop" ;;
esac
case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64)  arch=amd64 ;;
  *) die "$(uname -m) is not supported yet — build from source: go build ./cmd/hop" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is needed to download the binary"
command -v tar  >/dev/null 2>&1 || die "tar is needed to unpack it"

# The latest release, whatever it is. Asking GitHub rather than pinning a version
# means this script does not go stale.
api="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
tag=$(curl -fsSL "$api" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$tag" ] || die "could not find a release at $api"

url="https://github.com/$OWNER/$REPO/releases/download/$tag/grasshopper-$tag-$os-$arch.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading grasshopper $tag for $os/$arch"
curl -fsSL "$url" -o "$tmp/hop.tar.gz" || die "could not download $url"

# Every release publishes checksums. This script is piped into a shell, so the one
# thing worth spending ten lines on is checking that what arrived is what was
# published. A missing checksums file is a warning rather than a wall: an old
# release should still install.
sums="https://github.com/$OWNER/$REPO/releases/download/$tag/checksums.txt"
if curl -fsSL "$sums" -o "$tmp/checksums.txt" 2>/dev/null; then
  name="grasshopper-$tag-$os-$arch.tar.gz"
  want=$(awk -v n="$name" '$2 == n || $2 == "*"n { print $1 }' "$tmp/checksums.txt" | head -1)
  if [ -n "$want" ]; then
    if command -v shasum >/dev/null 2>&1; then
      got=$(shasum -a 256 "$tmp/hop.tar.gz" | cut -d" " -f1)
    elif command -v sha256sum >/dev/null 2>&1; then
      got=$(sha256sum "$tmp/hop.tar.gz" | cut -d" " -f1)
    fi
    if [ -n "${got:-}" ] && [ "$got" != "$want" ]; then
      die "checksum mismatch for $name — refusing to install
  published $want
  received  $got"
    fi
    [ -n "${got:-}" ] && say "checksum verified"
  fi
else
  say "note: no checksums published for $tag — installing unverified"
fi

tar -xzf "$tmp/hop.tar.gz" -C "$tmp"
[ -f "$tmp/hop" ] || die "the archive did not contain hop"

mkdir -p "$PREFIX"
install -m 0755 "$tmp/hop" "$PREFIX/hop"
say "installed $PREFIX/hop"

# Registering is part of installing: a binary on PATH that no agent knows about
# carries nothing, and the gap between those two states is where people conclude
# the tool does not work.
"$PREFIX/hop" setup || true

case ":$PATH:" in
  *":$PREFIX:"*) ;;
  *)
    say ""
    say "$PREFIX is not on your PATH. Add this to your shell profile:"
    say "    export PATH=\"$PREFIX:\$PATH\""
    ;;
esac
