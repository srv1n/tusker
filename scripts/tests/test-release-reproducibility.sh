#!/usr/bin/env sh
set -eu
if command -v gtar >/dev/null 2>&1; then tar_cmd=gtar
elif tar --version 2>/dev/null | grep -q 'GNU tar'; then tar_cmd=tar
else printf '%s\n' 'SKIP: GNU tar/gtar is required for release reproducibility test'; exit 0
fi
if command -v sha256sum >/dev/null 2>&1; then hash_cmd=sha256sum; else hash_cmd='shasum -a 256'; fi
root=$(mktemp -d "${TMPDIR:-/tmp}/tusker-repro.XXXXXX")
trap 'rm -rf "$root"' EXIT HUP INT TERM
mkdir -p "$root/a/fixture" "$root/b/fixture"
printf 'binary\n' >"$root/a/fixture/tusker"
printf 'readme\n' >"$root/a/fixture/README.md"
cp -R "$root/a/fixture/." "$root/b/fixture/"
touch -t 202001010101 "$root/a/fixture/tusker"
touch -t 202512312359 "$root/b/fixture/tusker"
for side in a b; do
 ( cd "$root/$side" && "$tar_cmd" --sort=name --mtime='@1700000000' --owner=0 --group=0 --numeric-owner --mode='u+rw,go+rX' -cf - fixture | gzip -n >"$root/$side.tar.gz" )
done
left=$($hash_cmd "$root/a.tar.gz" | awk '{print $1}')
right=$($hash_cmd "$root/b.tar.gz" | awk '{print $1}')
[ "$left" = "$right" ] || { printf 'FAIL: reproducible archive hashes differ: %s %s\n' "$left" "$right" >&2; exit 1; }
printf 'PASS: reproducible archive hash %s\n' "$left"
