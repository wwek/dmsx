#!/bin/sh
# Build dmsx with the official gh-ost binary from https://github.com/github/gh-ost
# embedded in the executable. dist/ also contains that same gh-ost file.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
version=${GHOST_VERSION:-v1.1.11}
os=$(go env GOOS)
arch=$(go env GOARCH)
asset="gh-ost-${os}-${arch}.tar.gz"

case "$os-$arch" in
linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64) ;;
*)
	echo "gh-ost publishes no binary for ${os}/${arch}" >&2
	exit 1
	;;
esac

cache="$root/dist/.cache"
mkdir -p "$cache"
work=$(mktemp -d)
trap 'rm -rf "$work"; rm -f "$root/internal/onlineddl/gh-ost"' EXIT

if [ ! -s "$cache/$asset" ] || [ ! -s "$cache/SHA256SUMS" ]; then
	echo "downloading gh-ost $version $asset"
	gh release download "$version" --repo github/gh-ost --pattern "$asset" --pattern SHA256SUMS --dir "$cache" --clobber
fi
cp "$cache/$asset" "$cache/SHA256SUMS" "$work/"
want=$(awk -v file="$asset" '$2 == file { print $1 }' "$work/SHA256SUMS")
got=$(shasum -a 256 "$work/$asset" | awk '{ print $1 }')
if [ -z "$want" ] || [ "$want" != "$got" ]; then
	echo "checksum mismatch for $asset" >&2
	exit 1
fi
tar -xzf "$work/$asset" -C "$work"
src=$(find "$work" -type f -name gh-ost | head -n 1)
if [ -z "$src" ]; then
	echo "gh-ost binary missing from $asset" >&2
	exit 1
fi

cp "$src" "$root/internal/onlineddl/gh-ost"
mkdir -p "$root/dist"
(
	cd "$root"
	release=$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)
	go build -tags ghostembed -trimpath -ldflags "-X dmsx/internal/version.Version=${release}" -o "$root/dist/dmsx" ./cmd/dmsx
)
cp "$src" "$root/dist/gh-ost"
chmod 755 "$root/dist/dmsx" "$root/dist/gh-ost"
echo "dist/dmsx includes gh-ost $version ($os/$arch)"
echo "dist/gh-ost is the same official binary"
