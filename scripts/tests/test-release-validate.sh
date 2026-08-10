#!/usr/bin/env sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd -P)
validator="$script_dir/../release-validate.sh"
tmp_root=$(mktemp -d "${TMPDIR:-/tmp}/tusker-release-test.XXXXXX")
trap 'rm -rf "$tmp_root"' EXIT INT TERM
tmp_root=$(CDPATH= cd -- "$tmp_root" && pwd -P)

pass=0
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
expect_reject() {
	value=$1
	if RELEASE_VERSION=$value "$validator" --root "$tmp_root/releases" --print-dir >/dev/null 2>&1; then
		fail "accepted invalid version: [$value]"
	fi
	pass=$((pass + 1))
}
expect_accept() {
	value=$1
	expected="$tmp_root/releases/$value"
	actual=$(RELEASE_VERSION=$value "$validator" --root "$tmp_root/releases" --print-dir)
	[ "$actual" = "$expected" ] || fail "wrong path for [$value]: [$actual]"
	[ -d "$tmp_root/releases" ] || fail 'validator did not create the requested root'
	pass=$((pass + 1))
}

expect_reject ''
expect_reject '   '
expect_reject '../v1.2.3'
expect_reject '/tmp/v1.2.3'
expect_reject 'v1.2.3`touch /tmp/pwned`'
expect_reject 'v1.2.3;echo-pwned'
expect_reject 'v1.2'
expect_reject '1.2.3'
expect_reject 'v01.2.3'
expect_reject 'v1.2.3-'
expect_reject 'v1.2.3-01'
if RELEASE_VERSION='v1.2.3;echo-pwned' "$validator" --check-only >/dev/null 2>&1; then
	fail 'check-only accepted shell metacharacters'
fi
pass=$((pass + 1))
expect_accept 'v0.0.0'
expect_accept 'v1.2.3'
expect_accept 'v1.2.3-rc.1'
expect_accept 'v1.2.3+build.42'
expect_accept 'v10.20.30-beta.2+linux.arm64'

mkdir -p "$tmp_root/outside"
ln -s "$tmp_root/outside" "$tmp_root/releases/v9.9.9"
if RELEASE_VERSION=v9.9.9 "$validator" --root "$tmp_root/releases" --print-dir >/dev/null 2>&1; then
	fail 'accepted a pre-existing symlink release directory'
fi
pass=$((pass + 1))

mkdir -p "$tmp_root/releases/v8.8.8/stale"
printf stale >"$tmp_root/releases/v8.8.8/stale/file"
prepared=$(RELEASE_VERSION=v8.8.8 "$validator" --root "$tmp_root/releases" --prepare-dir --print-dir)
[ "$prepared" = "$tmp_root/releases/v8.8.8" ] || fail 'prepare-dir returned the wrong path'
[ -d "$tmp_root/releases/v8.8.8" ] || fail 'prepare-dir did not recreate the directory'
[ ! -e "$tmp_root/releases/v8.8.8/stale/file" ] || fail 'prepare-dir retained stale contents'
pass=$((pass + 1))

printf 'PASS: %s release-version/path validation cases\n' "$pass"
