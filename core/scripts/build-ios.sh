#!/usr/bin/env bash
# Builds libtether.a (the Go core as a C archive) for the SDK Xcode is building for. Run from an Xcode
# build phase (it reads PLATFORM_NAME, ARCHS, SDKROOT, IPHONEOS_DEPLOYMENT_TARGET) with the output
# directory as $1; or by hand: PLATFORM_NAME=iphonesimulator ARCHS=arm64 build-ios.sh out/
# Needs Go (Homebrew or /usr/local/go) and Xcode's clang (cgo).
set -euo pipefail
here=$(cd "$(dirname "$0")/.." && pwd)
out=${1:?output directory}
export PATH="/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:$HOME/go/bin:$HOME/sdk/go/bin:$PATH"
command -v go >/dev/null || { echo "error: Go is not installed (brew install go)" >&2; exit 1; }

platform=${PLATFORM_NAME:-iphonesimulator}
sdk=${SDKROOT:-$(xcrun --sdk "$platform" --show-sdk-path)}
min=${IPHONEOS_DEPLOYMENT_TARGET:-18.0}
case "$platform" in
	iphoneos) suffix="" ;;
	iphonesimulator) suffix="-simulator" ;;
	*) echo "error: unsupported platform $platform" >&2; exit 1 ;;
esac
clang=$(xcrun --sdk "$platform" -f clang)

cd "$here"
./scripts/tailscale-src.sh
tags=$(cat build-tags.txt)
ver=$(awk '$1=="tailscale.com" && $2 ~ /^v/ {print substr($2,2); exit}' go.mod)
ldflags="-s -w -X tailscale.com/version.longStamp=$ver-tether -X tailscale.com/version.shortStamp=$ver"

mkdir -p "$out"
libs=()
for arch in ${ARCHS:-arm64}; do
	case "$arch" in
		arm64) goarch=arm64 ;;
		x86_64) goarch=amd64 ;;
		*) echo "error: unsupported arch $arch" >&2; exit 1 ;;
	esac
	target="$arch-apple-ios$min$suffix"
	flags="-isysroot $sdk -target $target"
	echo "building libtether for $target"
	GOOS=ios GOARCH=$goarch CGO_ENABLED=1 CC="$clang" CXX="$clang" \
		CGO_CFLAGS="$flags" CGO_LDFLAGS="$flags" GOFLAGS=-buildvcs=false \
		go build -trimpath -buildmode=c-archive -tags "$tags" -ldflags "$ldflags" \
		-o "$out/$arch/libtether.a" ./cmd/libtether
	libs+=("$out/$arch/libtether.a")
done
if [ ${#libs[@]} -gt 1 ]; then
	lipo -create "${libs[@]}" -output "$out/libtether.a"
else
	cp "${libs[0]}" "$out/libtether.a"
fi
first=${ARCHS%% *}; first=${first:-arm64}
cp "$out/$first/libtether.h" "$out/libtether.h"
