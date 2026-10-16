#!/bin/sh
# Installs msb at exactly the version orcasm pins (internal/msb/version.go),
# for CI. install.microsandbox.dev always installs the latest release, so
# this does what it does for one tagged release: download the bundle,
# verify its checksum, and lay it out under $MSB_HOME (~/.microsandbox).
#
#   .github/scripts/install-msb.sh            # the pinned version
#   .github/scripts/install-msb.sh 0.7.8
set -eu

version=${1:-$(sed -n 's/^const PinnedVersion = "\(.*\)"$/\1/p' "$(dirname "$0")/../../internal/msb/version.go")}
[ -n "$version" ] || { echo "install-msb: no version" >&2; exit 1; }
case "$(uname -s)-$(uname -m)" in
Linux-x86_64) target=linux-x86_64 ;;
Linux-aarch64 | Linux-arm64) target=linux-aarch64 ;;
*) echo "install-msb: unsupported platform $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

home=${MSB_HOME:-$HOME/.microsandbox}
base=https://github.com/superradcompany/microsandbox/releases/download/v$version
bundle=microsandbox-$target.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$bundle" "$base/$bundle"
curl -fsSL -o "$tmp/checksums.sha256" "$base/checksums.sha256"
(cd "$tmp" && grep -F " $bundle" checksums.sha256 | sha256sum -c --quiet -)
tar -xzf "$tmp/$bundle" -C "$tmp"

set -- "$tmp"/libkrunfw.so.*.*.*
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "install-msb: want exactly one libkrunfw in $bundle" >&2; exit 1; }
lib=$(basename "$1")
abi=${lib#libkrunfw.so.}
abi=${abi%%.*}

mkdir -p "$home/bin" "$home/lib"
install -m 755 "$tmp/msb" "$home/bin/msb"
ln -sf msb "$home/bin/microsandbox"
install -m 644 "$tmp/$lib" "$home/lib/$lib"
ln -sf "$lib" "$home/lib/libkrunfw.so.$abi"
ln -sf "libkrunfw.so.$abi" "$home/lib/libkrunfw.so"

if [ -n "${GITHUB_PATH:-}" ]; then
	echo "$home/bin" >>"$GITHUB_PATH"
fi
"$home/bin/msb" --version
