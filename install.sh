#!/bin/sh
# Positronick CLI installer — https://positronick.com
#
#   curl -fsSL https://positronick.com/install.sh | sh
#
# This is the raw.githubusercontent.com fallback for the script served at
# positronick.com/install.sh (mi-dika/positronick src/lib/installScript.ts) —
# keep the two behaviorally identical.
#
# Options (env vars):
#   POSITRONICK_VERSION      pin a release, e.g. "0.1.0" (default: latest)
#   POSITRONICK_INSTALL_DIR  install dir (default: $HOME/.local/bin — no root needed)
set -eu

REPO="Positronick/cli"
VERSION="${POSITRONICK_VERSION:-latest}"
# Tags are v-prefixed (v0.1.0) but the pin is the bare version — tolerate both.
VERSION="${VERSION#v}"
INSTALL_DIR="${POSITRONICK_INSTALL_DIR:-$HOME/.local/bin}"

err() {
	printf 'install.sh: %s\n' "$1" >&2
	exit 1
}

OS=$(uname -s)
case "$OS" in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) err "unsupported OS: $OS (linux and darwin only — see https://github.com/$REPO/releases)" ;;
esac

ARCH=$(uname -m)
case "$ARCH" in
	x86_64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) err "unsupported architecture: $ARCH (amd64 and arm64 only)" ;;
esac

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v tar >/dev/null 2>&1 || err "tar is required"

ASSET="positronick_${OS}_${ARCH}.tar.gz"
# Resolve "latest" to a concrete tag up front: the tarball and checksums.txt must come from
# the SAME release (a release published between the two fetches would otherwise look like a
# checksum mismatch), and the receipt should record a real version.
if [ "$VERSION" = "latest" ]; then
	LATEST_URL=$(curl --proto '=https' --tlsv1.2 -fsSLI -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest") \
		|| err "could not resolve the latest release of $REPO"
	TAG="${LATEST_URL##*/}"
	# With no releases published, GitHub redirects to /releases and TAG is "releases" —
	# accept only a v-prefixed tag.
	case "$TAG" in
	v[0-9]*) VERSION="${TAG#v}" ;;
	*) err "could not resolve the latest release of $REPO (got: $LATEST_URL)" ;;
	esac
fi
BASE="https://github.com/$REPO/releases/download/v$VERSION"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

fetch() {
	curl --proto '=https' --tlsv1.2 -fsSL "$1" -o "$2" || err "download failed: $1"
}

printf 'Downloading %s (%s)...\n' "$ASSET" "$VERSION"
fetch "$BASE/$ASSET" "$TMP/$ASSET"
fetch "$BASE/checksums.txt" "$TMP/checksums.txt"

cd "$TMP"
# Exact-name match (grep would treat the dots in the asset name as wildcards), tolerating
# both sha256sum output modes: "<hash>  <file>" (text) and "<hash> *<file>" (binary).
EXPECTED=$(awk -v asset="$ASSET" '{ name = $2; sub(/^\*/, "", name) } name == asset { print $1; exit }' checksums.txt)
[ -n "$EXPECTED" ] || err "no checksum for $ASSET in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	ACTUAL=$(sha256sum "$ASSET" | cut -d' ' -f1)
else
	ACTUAL=$(shasum -a 256 "$ASSET" | cut -d' ' -f1)
fi
[ "$ACTUAL" = "$EXPECTED" ] || err "checksum mismatch for $ASSET (expected $EXPECTED, got $ACTUAL)"

tar -xzf "$ASSET"
[ -f positronick ] || err "archive did not contain the positronick binary"
mkdir -p "$INSTALL_DIR"
install -m 755 positronick "$INSTALL_DIR/positronick"
ln -sf "$INSTALL_DIR/positronick" "$INSTALL_DIR/pck"
printf '{"method":"installer","version":"%s"}\n' "$VERSION" \
	> "$INSTALL_DIR/positronick-install-receipt.json"

printf 'Installed positronick (%s) to %s (alias: pck)\n' "$VERSION" "$INSTALL_DIR"
case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*)
		printf 'NOTE: %s is not on your PATH. Add this to your shell profile:\n' "$INSTALL_DIR"
		# $PATH must stay literal in the printed hint.
		# shellcheck disable=SC2016
		printf '  export PATH="%s:$PATH"\n' "$INSTALL_DIR"
		;;
esac
printf 'Get started: positronick soul search <query> — or see %s\n' "https://positronick.com"
