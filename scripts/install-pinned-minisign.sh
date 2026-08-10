#!/usr/bin/env sh
# Install the exact reviewed Minisign tool used by protected Linux CI jobs.
set -eu

destination=${1:-}
[ -n "$destination" ] || { printf '%s\n' 'destination directory is required.' >&2; exit 2; }
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || {
	printf '%s\n' 'The pinned CI Minisign installer supports Linux x86_64 only.' >&2
	exit 1
}
command -v curl >/dev/null 2>&1 || { printf '%s\n' 'curl is required.' >&2; exit 1; }
command -v sha256sum >/dev/null 2>&1 || { printf '%s\n' 'sha256sum is required.' >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { printf '%s\n' 'tar is required.' >&2; exit 1; }

archive_url='https://github.com/jedisct1/minisign/releases/download/0.12/minisign-0.12-linux.tar.gz'
archive_sha256='9a599b48ba6eb7b1e80f12f36b94ceca7c00b7a5173c95c3efc88d9822957e73'
work=$(mktemp -d 2>/dev/null || mktemp -d -t tusker-minisign)
cleanup() { rm -rf "$work"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
	-o "$work/minisign.tar.gz" "$archive_url"
printf '%s  %s\n' "$archive_sha256" "$work/minisign.tar.gz" | sha256sum -c -
tar -xzf "$work/minisign.tar.gz" -C "$work" minisign-linux/x86_64/minisign
mkdir -p "$destination"
install -m 0755 "$work/minisign-linux/x86_64/minisign" "$destination/minisign"
[ -x "$destination/minisign" ] || { printf '%s\n' 'Minisign install failed.' >&2; exit 1; }
