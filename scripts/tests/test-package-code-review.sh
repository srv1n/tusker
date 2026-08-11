#!/usr/bin/env sh
set -eu

repo_script="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)/scripts/package-code-review.sh"
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-review-package-test)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT INT TERM

fixture="${tmpdir}/repo"
mkdir -p "${fixture}/scripts" "${fixture}/src" "${fixture}/build"
cp "$repo_script" "${fixture}/scripts/package-code-review.sh"
chmod 500 "${fixture}/scripts/package-code-review.sh"
printf '%s\n' 'package fixture' > "${fixture}/src/main.go"
printf '%s\n' '# Fixture' > "${fixture}/README.md"

git -C "$fixture" init -q
git -C "$fixture" config user.name 'Review Package Test'
git -C "$fixture" config user.email 'review-package@example.invalid'
mkdir "${tmpdir}/empty-hooks"
git -C "$fixture" config core.hooksPath "${tmpdir}/empty-hooks"
git -C "$fixture" add scripts/package-code-review.sh src/main.go README.md
git -C "$fixture" commit -qm 'fixture base'

clean_archive="${tmpdir}/clean.zip"
(cd "$fixture" && ./scripts/package-code-review.sh --base HEAD "$clean_archive") >"${tmpdir}/clean-package.out"
unzip -p "$clean_archive" TUSKER_CODE_REVIEW_MANIFEST.txt | grep -q '^Dirty mode: 0$'

printf '%s\n' 'package changed' > "${fixture}/src/main.go"
printf '%s\n' 'untracked review note' > "${fixture}/notes.md"
printf '%s\n' 'generated and excluded' > "${fixture}/build/generated.go"

if (cd "$fixture" && ./scripts/package-code-review.sh --base HEAD "${tmpdir}/must-not-exist.zip") >"${tmpdir}/clean.out" 2>"${tmpdir}/clean.err"; then
	printf '%s\n' 'dirty worktree unexpectedly passed clean-default packaging' >&2
	exit 1
fi
grep -q 'Worktree is dirty' "${tmpdir}/clean.err"

archive="${tmpdir}/review.zip"
(cd "$fixture" && ./scripts/package-code-review.sh --allow-dirty --base HEAD "$archive") >"${tmpdir}/dirty.out"
extract="${tmpdir}/extract"
mkdir "$extract"
unzip -q "$archive" -d "$extract"

manifest="${extract}/TUSKER_CODE_REVIEW_MANIFEST.txt"
test -f "$manifest"
test -x "${extract}/TUSKER_CODE_REVIEW_VERIFY.sh"
grep -Eq '^HEAD: [0-9a-f]{40}$' "$manifest"
grep -Eq '^Base: [0-9a-f]{40}$' "$manifest"
grep -Eq '^Merge-base: [0-9a-f]{40}$' "$manifest"
grep -q '^Dirty mode: 1$' "$manifest"
grep -q '^Porcelain-v2 sha256: [0-9a-f][0-9a-f]*$' "$manifest"
tab="$(printf '\t')"
grep -q "^FILE${tab}tracked_.M${tab}.*${tab}src/main.go$" "$manifest"
grep -q "^FILE${tab}untracked${tab}.*${tab}notes.md$" "$manifest"
test ! -e "${extract}/build/generated.go"
(cd "$extract" && sh ./TUSKER_CODE_REVIEW_VERIFY.sh) >"${tmpdir}/verify.out"
grep -q '^Verified ' "${tmpdir}/verify.out"

printf '%s\n' 'tampered' > "${extract}/src/main.go"
if (cd "$extract" && sh ./TUSKER_CODE_REVIEW_VERIFY.sh) >/dev/null 2>&1; then
	printf '%s\n' 'tampered review package unexpectedly verified' >&2
	exit 1
fi

rm -rf "$extract"
mkdir "$extract"
unzip -q "$archive" -d "$extract"
printf '%s\n' 'undeclared' > "${extract}/extra.txt"
if (cd "$extract" && sh ./TUSKER_CODE_REVIEW_VERIFY.sh) >/dev/null 2>&1; then
	printf '%s\n' 'review package with undeclared file unexpectedly verified' >&2
	exit 1
fi

printf '%s\n' 'package-code-review focused test: PASS'
