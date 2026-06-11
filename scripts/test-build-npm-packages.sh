#!/bin/sh
# Smoke test for scripts/build-npm-packages.sh against fake release archives.
# Guards the invariants the npm optionalDependencies pattern depends on:
# every version field pinned in lockstep (a drifted pin makes `npm i` fail),
# node-style os/cpu fields (wrong mapping installs zero or two binaries),
# and an executable binary at the path the bin/positronick shim resolves.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(dirname -- "$script_dir")

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "FAIL: $1" >&2
	exit 1
}

# --- fixture: fake goreleaser archives with the contract name template
mkdir -p "$tmp/dist"
for os in darwin linux; do
	for arch in amd64 arm64; do
		work="$tmp/payload-$os-$arch"
		mkdir -p "$work"
		printf 'fake-binary %s/%s\n' "$os" "$arch" >"$work/positronick"
		chmod 755 "$work/positronick"
		printf 'license\n' >"$work/LICENSE"
		tar -czf "$tmp/dist/positronick_${os}_${arch}.tar.gz" -C "$work" positronick LICENSE
	done
done

# --- run
"$script_dir/build-npm-packages.sh" 1.2.3 "$tmp/dist" "$tmp/out"

# --- platform packages
for os in darwin linux; do
	for arch in amd64 arm64; do
		pkg="$tmp/out/@positronick/cli-$os-$arch"
		bin="$pkg/bin/positronick"
		manifest="$pkg/package.json"

		[ -x "$bin" ] || fail "$bin missing or not executable"
		grep -q "fake-binary $os/$arch" "$bin" || fail "$bin holds the wrong platform's binary"
		[ -e "$pkg/LICENSE" ] && fail "$pkg should contain only the binary, found LICENSE"

		grep -q "\"name\": \"@positronick/cli-$os-$arch\"" "$manifest" || fail "$manifest: wrong name"
		grep -q '"version": "1.2.3"' "$manifest" || fail "$manifest: version not pinned to 1.2.3"
		grep -q "\"os\": \[\"$os\"\]" "$manifest" || fail "$manifest: os field wrong"
		case $arch in
			amd64) node_cpu=x64 ;;
			arm64) node_cpu=arm64 ;;
		esac
		grep -q "\"cpu\": \[\"$node_cpu\"\]" "$manifest" || fail "$manifest: cpu must be node-style ($node_cpu)"
	done
done

# --- meta package: committed scaffold copied with every placeholder rewritten
meta="$tmp/out/positronick"
[ -x "$meta/bin/positronick" ] || fail "meta bin/positronick shim missing or not executable"
[ -e "$meta/bin/pck" ] || fail "meta bin/pck shim missing"
grep -q '"version": "1.2.3"' "$meta/package.json" || fail "meta version not rewritten"
grep -q '"@positronick/cli-darwin-arm64": "1.2.3"' "$meta/package.json" || fail "optionalDependencies not pinned in lockstep"
if grep -rq '0\.0\.0-placeholder' "$tmp/out"; then
	fail "placeholder version survived the rewrite"
fi

# --- bad input is rejected loudly
if "$script_dir/build-npm-packages.sh" v1.2.3 "$tmp/dist" "$tmp/out2" 2>/dev/null; then
	fail "a v-prefixed version must be rejected (npm versions are bare semver)"
fi
if "$script_dir/build-npm-packages.sh" 1.2.3 "$repo_root/does-not-exist" "$tmp/out3" 2>/dev/null; then
	fail "a missing dist dir must be rejected"
fi

echo "ok: build-npm-packages.sh"
