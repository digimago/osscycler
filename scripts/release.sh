#!/usr/bin/env bash
# Release builds with the ANT+ network key built in, so riders needn't
# fetch it. The key comes from $ANT_PLUS_NETWORK_KEY (the CI secret) or
# $ANT_KEY_FILE; it is never echoed and never touches a tracked file.
#
# Into $DIST:
#   linux   amd64, arm64   .tar.gz, .deb, .rpm (binaries, udev rule)
#   macOS   universal      .tar.gz (Intel and Apple Silicon in one binary)
# and SHA256SUMS over all of them. Linux first, macOS second; no Windows.
set -euo pipefail

VERSION=${VERSION:?set VERSION}
DIST=${DIST:-_dist}
ANT_KEY_FILE=${ANT_KEY_FILE:-_secrets/ant-network-key}
# Build tools, pinned and run through the Go toolchain: nothing to install,
# and the same on a Mac and on the Linux CI runner.
LIPO=(go run github.com/konoui/lipo@v0.10.0)
NFPM=(go run github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.47.0)

die() { echo "release: $*" >&2; exit 1; }

key=${ANT_PLUS_NETWORK_KEY:-$(cat "$ANT_KEY_FILE" 2>/dev/null || true)}
key=$(printf '%s' "$key" | sed 's/0[xX]//g' | tr -cd '0-9a-fA-F')
[[ ${#key} -eq 16 ]] || die "need the ANT+ network key (16 hex digits) in ANT_PLUS_NETWORK_KEY or $ANT_KEY_FILE"
ldflags="-s -w -X main.builtinNetworkKey=$key -X main.releaseVersion=$VERSION"
ANT_PLUS_NETWORK_KEY= go run -ldflags "$ldflags" ./cmd/osscycler-core -key-check | grep -q "built in" ||
	die "the key didn't make it into the binary"

# Packages want a version that starts with a digit, ordered as dpkg and
# rpm see it: v0.1.0 → 0.1.0; a build between tags (git describe
# v0.1.0-3-gabc1234) → 0.1.0+3.gabc1234, after 0.1.0; a pre-release
# v0.2.0-rc1 → 0.2.0~rc1, before 0.2.0; anything else → 0.0.0~<it>.
pkg_version=${VERSION#v}
if [[ $pkg_version =~ ^([0-9].*)-([0-9]+)-(g[0-9a-f]+)(-dirty)?$ ]]; then
	base=${BASH_REMATCH[1]//-/\~}
	pkg_version="$base+${BASH_REMATCH[2]}.${BASH_REMATCH[3]}${BASH_REMATCH[4]:+.dirty}"
elif [[ $pkg_version =~ ^[0-9] ]]; then
	pkg_version=${pkg_version//-/\~}
else
	pkg_version="0.0.0~$(printf '%s' "$VERSION" | tr -c '0-9A-Za-z.~\n' '.')"
fi

rm -rf "$DIST"
mkdir -p "$DIST"
# No Apple metadata (._ files, extended attributes) in archives made on a Mac.
export COPYFILE_DISABLE=1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# build GOOS GOARCH DIR: both binaries into DIR.
build() {
	mkdir -p "$3"
	GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$3/" ./cmd/osscycler-core ./cmd/osscycler-tui
}

# Linux: archive and packages per architecture.
for arch in amd64 arm64; do
	name=osscycler_${VERSION}_linux_${arch}
	build linux "$arch" "$work/$name"
	cp README.md deploy/udev/99-ant-usb.rules deploy/udev/install-udev.sh deploy/systemd/osscycler@.service "$work/$name/"
	tar --no-xattrs -C "$work" -czf "$DIST/$name.tar.gz" "$name"
	# nfpm doesn't expand the environment in file paths: fill in the config.
	sed -e "s|\${ARCH}|$arch|" -e "s|\${PKG_VERSION}|$pkg_version|" -e "s|\${BUILD_DIR}|$work/$name|" \
		deploy/packaging/nfpm.yaml > "$work/nfpm-$arch.yaml"
	for packager in deb rpm; do
		"${NFPM[@]}" package --config "$work/nfpm-$arch.yaml" --packager "$packager" --target "$DIST/" >/dev/null
	done
done

# macOS: one universal binary per program.
name=osscycler_${VERSION}_darwin_universal
build darwin amd64 "$work/darwin_amd64"
build darwin arm64 "$work/darwin_arm64"
mkdir -p "$work/$name"
for bin in osscycler-core osscycler-tui; do
	"${LIPO[@]}" -output "$work/$name/$bin" -create "$work/darwin_amd64/$bin" "$work/darwin_arm64/$bin"
	chmod +x "$work/$name/$bin"
done
cp README.md "$work/$name/"
tar --no-xattrs -C "$work" -czf "$DIST/$name.tar.gz" "$name"

(cd "$DIST" && shasum -a 256 -- *.tar.gz *.deb *.rpm > SHA256SUMS)
ls -1 "$DIST"
