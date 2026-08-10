#!/usr/bin/env sh
set -eu
if [ "$(uname -s)" != Darwin ]; then printf '%s\n' 'SKIP: macOS atomic swap test'; exit 0; fi
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd -P)
root=$(mktemp -d "${TMPDIR:-/tmp}/tusker-swap.XXXXXX")
trap 'rm -rf "$root"' EXIT
xcrun clang -Wall -Wextra -Werror -o "$root/swap" "$repo/apps/mac/TuskerBar/scripts/atomic-swap.c"
mkdir "$root/current" "$root/staged"
printf old >"$root/current/value"
printf new >"$root/staged/value"
"$root/swap" "$root/staged" "$root/current"
[ "$(cat "$root/current/value")" = new ] || { printf '%s\n' 'FAIL: new value not installed' >&2; exit 1; }
[ "$(cat "$root/staged/value")" = old ] || { printf '%s\n' 'FAIL: prior value not preserved' >&2; exit 1; }
"$root/swap" "$root/staged" "$root/current"
[ "$(cat "$root/current/value")" = old ] || { printf '%s\n' 'FAIL: rollback did not restore old value' >&2; exit 1; }
mkdir "$root/fresh-stage"
printf fresh >"$root/fresh-stage/value"
"$root/swap" "$root/fresh-stage" "$root/fresh"
[ "$(cat "$root/fresh/value")" = fresh ] || { printf '%s\n' 'FAIL: fresh install did not publish staged value' >&2; exit 1; }
[ ! -e "$root/fresh-stage" ] || { printf '%s\n' 'FAIL: fresh install left staged path behind' >&2; exit 1; }
printf '%s\n' 'PASS: macOS atomic directory swap and rollback'
