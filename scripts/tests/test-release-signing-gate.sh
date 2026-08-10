#!/usr/bin/env sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd -P)
root=$(mktemp -d "${TMPDIR:-/tmp}/tusker-signing-gate.XXXXXX")
trap 'rm -rf "$root"' EXIT
mkdir "$root/bin"
for name in git go gzip python3 stat awk grep sh; do
  path=$(command -v "$name")
  ln -s "$path" "$root/bin/$name"
done
printf '#!/bin/sh\nexit 0\n' >"$root/bin/minisign"
chmod 755 "$root/bin/minisign"
printf fixture >"$root/secret"
chmod 600 "$root/secret"
output="$root/output"
if (cd "$repo" && PATH="$root/bin" RELEASE_VERSION=v1.2.3 RELEASE_MINISIGN_SECRET_KEY_FILE="$root/secret" scripts/release-build.sh) >"$output" 2>&1; then
  printf '%s\n' 'FAIL: release proceeded without committed public key' >&2
  exit 1
fi
grep -q 'Release public key is not provisioned' "$output" || { cat "$output" >&2; exit 1; }
printf '%s\n' 'PASS: production release fails closed without the human-provisioned public key'
