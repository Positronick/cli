#!/bin/sh
# Builds the publishable npm packages out of goreleaser release archives
# (see packages/npm/README.md for the layout and publish order):
#   - four @positronick/cli-<os>-<arch> platform packages, each holding the
#     binary extracted from positronick_<os>_<arch>.tar.gz;
#   - the positronick meta-package, copied from packages/npm/positronick with
#     every 0.0.0-placeholder version rewritten to the released version.
#
# Usage: build-npm-packages.sh <version> <dist_dir> <out_dir>
#   version   bare semver (no leading v), e.g. 0.1.0
#   dist_dir  directory holding the positronick_<os>_<arch>.tar.gz archives
#   out_dir   created; packages land in <out_dir>/@positronick/* and <out_dir>/positronick
set -eu

usage() {
	echo "usage: $0 <version> <dist_dir> <out_dir>" >&2
	exit 1
}

[ $# -eq 3 ] || usage
version=$1
dist=$2
out=$3

# Reject anything that isn't a bare semver-ish version: the value is
# interpolated raw into package.json and a sed replacement, so stray
# characters ("&", "/", '"') would silently corrupt the output.
case $version in
	[0-9]*) ;;
	*)
		echo "error: version '$version' must be bare semver (strip the leading v)" >&2
		exit 1
		;;
esac
case $version in
	*[!0-9A-Za-z.+-]*)
		echo "error: version '$version' contains characters outside [0-9A-Za-z.+-]" >&2
		exit 1
		;;
esac
[ -d "$dist" ] || {
	echo "error: dist dir '$dist' does not exist" >&2
	exit 1
}

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
meta_src=$(dirname -- "$script_dir")/packages/npm/positronick

for os in darwin linux; do
	for arch in amd64 arm64; do
		archive="$dist/positronick_${os}_${arch}.tar.gz"
		pkg_dir="$out/@positronick/cli-$os-$arch"

		mkdir -p "$pkg_dir/bin"
		tar -xzf "$archive" -C "$pkg_dir/bin" positronick
		chmod 755 "$pkg_dir/bin/positronick"

		# npm os/cpu use node's values: process.arch is x64 where Go says amd64.
		case $arch in
			amd64) node_cpu=x64 ;;
			arm64) node_cpu=arm64 ;;
		esac

		cat >"$pkg_dir/package.json" <<EOF
{
	"name": "@positronick/cli-$os-$arch",
	"version": "$version",
	"description": "Positronick CLI binary for $os/$arch — installed via the positronick package, do not depend on this directly",
	"homepage": "https://positronick.com",
	"repository": {
		"type": "git",
		"url": "git+https://github.com/Positronick/cli.git"
	},
	"license": "Apache-2.0",
	"os": ["$os"],
	"cpu": ["$node_cpu"],
	"files": ["bin"],
	"publishConfig": {"access": "public"}
}
EOF
	done
done

mkdir -p "$out/positronick"
rm -rf "$out/positronick/bin" # cp -R would nest bin/bin if the out dir is reused
cp -R "$meta_src/bin" "$out/positronick/bin"
sed "s/0\\.0\\.0-placeholder/$version/g" "$meta_src/package.json" >"$out/positronick/package.json"

echo "built npm packages for $version in $out"
