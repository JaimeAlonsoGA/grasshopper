#!/bin/sh
# grasshopper — install and set up in one command.
#
#   curl -fsSL https://raw.githubusercontent.com/JaimeAlonsoGA/grasshopper/main/install.sh | sh
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
