#!/bin/sh
# Install orcasm from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/Betree/Orcasm/main/install.sh | sh
#
# Environment:
#   ORCASM_VERSION      release tag to install (e.g. v0.1.0); default: latest
#   ORCASM_INSTALL_DIR  where to put the binary; default: /usr/local/bin if
#                       writable, else $HOME/.local/bin
set -eu

REPO="Betree/Orcasm"

fail() {
	echo "orcasm install: $*" >&2
	exit 1
}

need() {
	command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

need curl
need tar
need uname

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "unsupported OS: $(uname -s) (orcasm supports Linux and macOS)" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture: $(uname -m)" ;;
esac

version="${ORCASM_VERSION:-latest}"
if [ "$version" = "latest" ]; then
	base="https://github.com/$REPO/releases/latest/download"
else
	base="https://github.com/$REPO/releases/download/$version"
fi

archive="orcasm_${os}_${arch}.tar.gz"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "Downloading $archive ($version)..."
curl -fsSL "$base/$archive" -o "$tmp/$archive" || fail "download failed: $base/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $base/checksums.txt"

expected="$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")"
[ -n "$expected" ] || fail "no checksum listed for $archive"
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmp/$archive" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')"
else
	fail "need sha256sum or shasum to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" orcasm

dir="${ORCASM_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/orcasm" "$dir/orcasm"

echo "Installed $("$dir/orcasm" --version) to $dir/orcasm"
case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Note: $dir is not on your PATH. Add it to use orcasm." ;;
esac
