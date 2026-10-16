#!/bin/sh
# Installs msb at exactly one release: the one orcasm pins
# (internal/msb/version.go) by default. install.microsandbox.dev always
# installs the latest release, so this does what it does for one tagged
# release: download the bundle, verify its checksum, and lay it out under
# HOME_DIR (default $MSB_HOME, else ~/.microsandbox) as bin/msb and lib/.
# `orcasm self-update` runs it (embedded) to bring the host msb to the pin
# without overshooting it; CI runs it from the repository.
#
#   internal/msb/install-msb.sh                    # the pinned version
#   internal/msb/install-msb.sh 0.7.8 [HOME_DIR]
set -eu

version=${1:-}
if [ -z "$version" ] && [ -f "$(dirname "$0")/version.go" ]; then
	version=$(sed -n 's/^const PinnedVersion = "\(.*\)"$/\1/p' "$(dirname "$0")/version.go")
fi
[ -n "$version" ] || { echo "install-msb: no version" >&2; exit 1; }
case "$(uname -s)-$(uname -m)" in
Linux-x86_64) target=linux-x86_64 os=linux ;;
Linux-aarch64 | Linux-arm64) target=linux-aarch64 os=linux ;;
Darwin-arm64 | Darwin-aarch64) target=darwin-aarch64 os=darwin ;;
*) echo "install-msb: unsupported platform $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

home=${2:-${MSB_HOME:-$HOME/.microsandbox}}
base=https://github.com/superradcompany/microsandbox/releases/download/v$version
bundle=microsandbox-$target.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

curl -fsSL -o "$tmp/$bundle" "$base/$bundle"
curl -fsSL -o "$tmp/checksums.sha256" "$base/checksums.sha256"
want=$(grep -F " $bundle" "$tmp/checksums.sha256" | awk '{print $1}')
if command -v sha256sum >/dev/null 2>&1; then
	have=$(sha256sum "$tmp/$bundle" | awk '{print $1}')
else
	have=$(shasum -a 256 "$tmp/$bundle" | awk '{print $1}')
fi
[ -n "$want" ] && [ "$want" = "$have" ] || { echo "install-msb: checksum mismatch for $bundle" >&2; exit 1; }
tar -xzf "$tmp/$bundle" -C "$tmp"

if [ "$os" = linux ]; then
	set -- "$tmp"/libkrunfw.so.*.*.*
else
	set -- "$tmp"/libkrunfw.*.dylib
fi
[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "install-msb: want exactly one libkrunfw in $bundle" >&2; exit 1; }
lib=$(basename "$1")

# Fresh inodes, as msb's own installer does: a running msb (or library)
# keeps the old file, and macOS caches code signatures per vnode.
mkdir -p "$home/bin" "$home/lib"
cp "$tmp/msb" "$home/bin/.msb.new"
chmod 755 "$home/bin/.msb.new"
mv -f "$home/bin/.msb.new" "$home/bin/msb"
ln -sf msb "$home/bin/microsandbox"
cp "$tmp/$lib" "$home/lib/.$lib.new"
chmod 644 "$home/lib/.$lib.new"
mv -f "$home/lib/.$lib.new" "$home/lib/$lib"
if [ "$os" = linux ]; then
	abi=${lib#libkrunfw.so.}
	abi=${abi%%.*}
	ln -sf "$lib" "$home/lib/libkrunfw.so.$abi"
	ln -sf "libkrunfw.so.$abi" "$home/lib/libkrunfw.so"
else
	ln -sf "$lib" "$home/lib/libkrunfw.dylib"
fi

if [ -n "${GITHUB_PATH:-}" ]; then
	echo "$home/bin" >>"$GITHUB_PATH"
fi
"$home/bin/msb" --version
