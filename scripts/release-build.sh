#!/usr/bin/env sh
# Build one immutable, reproducible, signed release transaction.
set -eu

root=${RELEASE_ROOT:-dist/releases}
version=${RELEASE_VERSION:-}
matrix=${RELEASE_MATRIX:-"darwin/arm64 darwin/amd64 linux/arm64 linux/amd64"}
secret_key=${RELEASE_MINISIGN_SECRET_KEY_FILE:-}
public_key_file=${RELEASE_MINISIGN_PUBLIC_KEY_FILE:-scripts/release-minisign.pub}
trusted_tag_signer=${RELEASE_TRUSTED_TAG_SIGNER:-}
tag_signer_public_key=${RELEASE_TAG_SIGNER_PUBLIC_KEY_FILE:-scripts/release-tag-signer.asc}

RELEASE_VERSION=$version scripts/release-validate.sh --root "$root" --check-only
command -v git >/dev/null 2>&1 || { printf '%s\n' 'git is required.' >&2; exit 1; }
command -v go >/dev/null 2>&1 || { printf '%s\n' 'go is required.' >&2; exit 1; }
command -v gzip >/dev/null 2>&1 || { printf '%s\n' 'gzip is required.' >&2; exit 1; }
command -v minisign >/dev/null 2>&1 || { printf '%s\n' 'minisign is required.' >&2; exit 1; }
command -v python3 >/dev/null 2>&1 || { printf '%s\n' 'python3 is required for the release SBOM.' >&2; exit 1; }

[ -n "$secret_key" ] && [ -f "$secret_key" ] || { printf '%s\n' 'RELEASE_MINISIGN_SECRET_KEY_FILE must name a chmod-600 temporary key file.' >&2; exit 1; }
[ ! -L "$secret_key" ] || { printf '%s\n' 'Refusing a symlink signing key.' >&2; exit 1; }
key_mode=$(stat -f '%Lp' "$secret_key" 2>/dev/null || stat -c '%a' "$secret_key" 2>/dev/null || printf unknown)
[ "$key_mode" = 600 ] || { printf 'Signing key mode must be 600, got %s.\n' "$key_mode" >&2; exit 1; }
[ -f "$public_key_file" ] && [ ! -L "$public_key_file" ] || {
	printf 'Release public key is not provisioned at %s. A human must commit the real minisign public key before release.\n' "$public_key_file" >&2
	exit 1
}
public_key=$(awk '!/^untrusted comment:/ && NF { print; exit }' "$public_key_file")
[ -n "$public_key" ] || { printf '%s\n' 'Release public key file is malformed.' >&2; exit 1; }
grep -Fq "PINNED_MINISIGN_PUBLIC_KEY='$public_key'" scripts/install.sh || {
	printf '%s\n' 'Installer pinned key does not match scripts/release-minisign.pub.' >&2
	exit 1
}

head=$(git rev-parse HEAD)
tag_commit=$(git rev-parse "$version^{}" 2>/dev/null || true)
[ "$tag_commit" = "$head" ] || { printf 'Release tag %s does not point to HEAD %s.\n' "$version" "$head" >&2; exit 1; }
[ -n "$trusted_tag_signer" ] || { printf '%s\n' 'RELEASE_TRUSTED_TAG_SIGNER fingerprint is required.' >&2; exit 1; }
RELEASE_TRUSTED_TAG_SIGNER=$trusted_tag_signer \
	RELEASE_TAG_SIGNER_PUBLIC_KEY_FILE=$tag_signer_public_key \
	scripts/release-verify-tag.sh "$version" >/dev/null
[ -z "$(git status --porcelain --untracked-files=normal)" ] || { printf '%s\n' 'Release requires a clean worktree.' >&2; exit 1; }
epoch=$(git show -s --format=%ct "$tag_commit")
case "$epoch" in ''|*[!0-9]*) printf '%s\n' 'Unable to derive tag commit timestamp.' >&2; exit 1;; esac
if [ -n "${SOURCE_DATE_EPOCH:-}" ] && [ "$SOURCE_DATE_EPOCH" != "$epoch" ]; then
	printf 'SOURCE_DATE_EPOCH must equal tagged commit timestamp %s.\n' "$epoch" >&2; exit 1
fi
export SOURCE_DATE_EPOCH=$epoch

if command -v gtar >/dev/null 2>&1; then gnu_tar=gtar
elif tar --version 2>/dev/null | grep -q 'GNU tar'; then gnu_tar=tar
else printf '%s\n' 'Deterministic release archives require GNU tar (install gtar on macOS).' >&2; exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then sha256='sha256sum'
elif command -v shasum >/dev/null 2>&1; then sha256='shasum -a 256'
else printf '%s\n' 'sha256sum or shasum is required.' >&2; exit 1
fi

mkdir -p "$root"
root_abs=$(CDPATH= cd -P "$root" && pwd -P)
final="$root_abs/$version"
[ ! -e "$final" ] && [ ! -L "$final" ] || { printf 'Refusing to overwrite immutable release %s.\n' "$final" >&2; exit 1; }
lock="$root_abs/.release.lock"
mkdir "$lock" 2>/dev/null || { printf 'Release build already in progress: %s\n' "$lock" >&2; exit 1; }
stage="$root_abs/.${version}.staging.$$"
cleanup() { rm -rf "$lock" "${stage:-}"; }
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir "$stage"

python3 scripts/generate-release-sbom.py >"$stage/sbom.cdx.json"
[ -s "$stage/sbom.cdx.json" ] || { printf '%s\n' 'SBOM generator produced no output.' >&2; exit 1; }
python3 -m json.tool "$stage/sbom.cdx.json" >/dev/null || { printf '%s\n' 'SBOM is not valid JSON.' >&2; exit 1; }

python3 scripts/generate-release-provenance.py \
	"$version" "$head" "$epoch" "$(go version)" "$matrix" >"$stage/provenance.json"
python3 -m json.tool "$stage/provenance.json" >/dev/null || {
	printf '%s\n' 'Release provenance is not valid JSON.' >&2
	exit 1
}

for target in $matrix; do
	goos=${target%/*}; goarch=${target#*/}
	case "$goos/$goarch" in darwin/arm64|darwin/amd64|linux/arm64|linux/amd64|windows/amd64) ;; *) printf 'Unsupported release target: %s\n' "$target" >&2; exit 1;; esac
	stem="tusker_${version}_${goos}_${goarch}"; work="$stage/$stem"; mkdir "$work"; ext=''; [ "$goos" = windows ] && ext=.exe
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.buildVersion=$version" -o "$work/tusker$ext" ./cmd/tusker
	cp README.md LICENSE "$work/"
	archive="$stage/$stem.tar.gz"
	( cd "$stage" && "$gnu_tar" --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner --mode='u+rw,go+rX' -cf - "$stem" | gzip -n >"$archive" )
	rm -rf "$work"
done

( cd "$stage" && $sha256 *.tar.gz >checksums.txt )
( cd "$stage" && $sha256 checksums.txt provenance.json sbom.cdx.json *.tar.gz | LC_ALL=C sort -k2 >MANIFEST.sha256 )
[ "$(git rev-parse HEAD)" = "$head" ] && [ "$(git rev-parse "$version^{}")" = "$head" ] && [ -z "$(git status --porcelain --untracked-files=normal)" ] || {
	printf '%s\n' 'Repository changed during release build; refusing to sign.' >&2; exit 1;
}
minisign -Sm "$stage/MANIFEST.sha256" -s "$secret_key" -x "$stage/MANIFEST.sha256.minisig"
minisign -Vm "$stage/MANIFEST.sha256" -p "$public_key_file" -x "$stage/MANIFEST.sha256.minisig" >/dev/null
mv "$stage" "$final"
stage=''
printf 'release artifacts: %s\n' "$final"
