#!/usr/bin/env sh
# Verify one GPG-signed release tag against the repository-pinned public key and
# the independently configured trusted fingerprint.
set -eu

tag=${1:-}
trusted=${RELEASE_TRUSTED_TAG_SIGNER:-}
public_key_file=${RELEASE_TAG_SIGNER_PUBLIC_KEY_FILE:-scripts/release-tag-signer.asc}

[ -n "$tag" ] || { printf '%s\n' 'release tag argument is required.' >&2; exit 2; }
RELEASE_VERSION=$tag scripts/release-validate.sh --check-only
[ -n "$trusted" ] || { printf '%s\n' 'RELEASE_TRUSTED_TAG_SIGNER fingerprint is required.' >&2; exit 1; }
case "$trusted" in *[!0-9A-Fa-f]*) printf '%s\n' 'RELEASE_TRUSTED_TAG_SIGNER must be a hexadecimal fingerprint.' >&2; exit 1;; esac
[ -f "$public_key_file" ] && [ ! -L "$public_key_file" ] || {
	printf 'Trusted release-tag public key is not provisioned at %s.\n' "$public_key_file" >&2
	exit 1
}
command -v git >/dev/null 2>&1 || { printf '%s\n' 'git is required.' >&2; exit 1; }
command -v gpg >/dev/null 2>&1 || { printf '%s\n' 'gpg is required to verify release tags.' >&2; exit 1; }

gnupg_home=$(mktemp -d 2>/dev/null || mktemp -d -t tusker-release-gpg)
cleanup() { rm -rf "$gnupg_home"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
chmod 700 "$gnupg_home"
export GNUPGHOME=$gnupg_home
gpg --batch --quiet --import "$public_key_file" >/dev/null 2>&1 || {
	printf '%s\n' 'Unable to import the trusted release-tag public key.' >&2
	exit 1
}

trusted_upper=$(printf '%s' "$trusted" | tr '[:lower:]' '[:upper:]')
if ! gpg --batch --with-colons --fingerprint --fingerprint \
	| awk -F: '$1 == "fpr" { print toupper($10) }' \
	| grep -Fxq "$trusted_upper"; then
	printf '%s\n' 'Configured release fingerprint is absent from the pinned public key.' >&2
	exit 1
fi

verify_output=$(git verify-tag --raw "$tag" 2>&1) || {
	printf 'Release tag %s has no valid GPG signature.\n' "$tag" >&2
	exit 1
}
tag_signer=$(printf '%s\n' "$verify_output" | awk '$2 == "VALIDSIG" { print toupper($3); exit }')
[ "$tag_signer" = "$trusted_upper" ] || {
	printf 'Release tag signer %s does not match trusted fingerprint %s.\n' "${tag_signer:-unknown}" "$trusted_upper" >&2
	exit 1
}
printf 'verified release tag %s signer %s\n' "$tag" "$tag_signer"
