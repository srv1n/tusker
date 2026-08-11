#!/usr/bin/env sh
set -eu

DEFAULT_ARCHIVE_NAME="tusker-code-review.zip"
MANIFEST_NAME="TUSKER_CODE_REVIEW_MANIFEST.txt"
VERIFY_NAME="TUSKER_CODE_REVIEW_VERIFY.sh"

usage() {
	cat <<'EOF'
Usage:
  ./scripts/package-code-review.sh [--base REF] [--allow-dirty] [archive-name.zip]
  ./scripts/package-code-review.sh [--base REF] [--allow-dirty] --list

Creates a source-only review archive with commit/worktree provenance, per-file
SHA-256 receipts, and an embedded completeness verifier. The default refuses a
dirty worktree; --allow-dirty explicitly packages and records local changes.

Options:
  --base REF     record the exact base commit and merge-base (default: upstream,
                 then HEAD^, then HEAD)
  --allow-dirty  include modified and untracked reviewable files and record the
                 porcelain-v2 worktree state
  --list         print included files without creating a zip
  --help         show this help
EOF
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		printf 'Missing required command: %s\n' "$1" >&2
		exit 1
	}
}

sha256_file() {
	case "$sha256_tool" in
		sha256sum) sha256sum "$1" | sed 's/[[:space:]].*$//' ;;
		shasum) shasum -a 256 "$1" | sed 's/[[:space:]].*$//' ;;
	esac
}

should_skip_path() {
	case "$1" in
		.git/*|.tools/*|node_modules/*|vendor/*|dist/*|build/*|out/*|coverage/*)
			return 0 ;;
		tmp/*|temp/*|.tmp/*|.cache/*|.parcel-cache/*)
			return 0 ;;
		site/node_modules/*|site/dist/*|site/.astro/*|site/public/generated/*|site/src/generated/*)
			return 0 ;;
		tusker\ 2/*|tusker/work/*|tusker/epics/*|tusker/events/*|tusker/evidence/*|tusker/attempts/*|tusker/_generated/*|tusker/dashboards/*)
			return 0 ;;
		tusker/_system/project.yaml|tusker/_system/generated/*|tusker/_system/templates.zip)
			return 0 ;;
		.tusker-*|.tusker-*/*|*.log|*.log.*|*.zip|*.tar|*.tar.gz|*.tgz)
			return 0 ;;
		*.mp4|*.mov|*.webm|*.png|*.jpg|*.jpeg|*.gif|*.webp|*.ico|*.pdf)
			return 0 ;;
		*.bin|*.exe|*.dmg|*.pkg|*.test|*.prof|*.out)
			return 0 ;;
	esac
	return 1
}

should_include_path() {
	case "$1" in
		.gitignore|*/.gitignore|.gitattributes|*/.gitattributes|.editorconfig|*/.editorconfig)
			return 0 ;;
		AGENTS.md|*/AGENTS.md|CLAUDE.md|*/CLAUDE.md|README|*/README|README.*|*/README.*|LICENSE|*/LICENSE|LICENSE.*|*/LICENSE.*|Makefile|*/Makefile)
			return 0 ;;
		*.go|*.mod|*.sum|*.sh|*.bash|*.zsh|*.py|*.js|*.jsx|*.ts|*.tsx|*.mjs|*.cjs)
			return 0 ;;
		*.css|*.scss|*.html|*.astro|*.md|*.mdx|*.txt|*.json|*.jsonl|*.yaml|*.yml|*.toml|*.base|*.lock|*.svg)
			return 0 ;;
	esac
	return 1
}

classify_path() {
	status_line="$(git status --porcelain=v2 --untracked-files=all -- "$1" | sed -n '1p')"
	case "$status_line" in
		"") printf '%s\n' tracked_clean ;;
		"? "*) printf '%s\n' untracked ;;
		"1 "*) printf 'tracked_%s\n' "$(printf '%s\n' "$status_line" | cut -d' ' -f2)" ;;
		"2 "*) printf 'tracked_renamed_%s\n' "$(printf '%s\n' "$status_line" | cut -d' ' -f2)" ;;
		"u "*) printf '%s\n' tracked_unmerged ;;
		*) printf '%s\n' unknown_dirty ;;
	esac
}

build_file_list() {
	candidates_path="$1"
	included_path="$2"
	git ls-files --cached --modified --others --exclude-standard | LC_ALL=C sort -u > "$candidates_path"
	: > "$included_path"
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		case "$path" in
			\"*)
				printf 'Review package refuses a Git-quoted path (control, newline, backslash, or non-portable bytes): %s\n' "$path" >&2
				exit 1 ;;
		esac
		[ -f "$path" ] || continue
		[ ! -L "$path" ] || { printf 'Review package refuses a symlink: %s\n' "$path" >&2; exit 1; }
		case "$path" in
			*"$(printf '\t')"*)
				printf 'Review package refuses a path containing a tab: %s\n' "$path" >&2
				exit 1 ;;
		esac
		should_skip_path "$path" && continue
		should_include_path "$path" && printf '%s\n' "$path" >> "$included_path"
	done < "$candidates_path"
}

list_only=0
allow_dirty=0
archive_name="$DEFAULT_ARCHIVE_NAME"
archive_set=0
base_ref=""

while [ "$#" -gt 0 ]; do
	case "$1" in
		--list) list_only=1; shift ;;
		--allow-dirty) allow_dirty=1; shift ;;
		--base)
			[ "$#" -ge 2 ] || { printf '%s\n' '--base requires a ref.' >&2; exit 1; }
			base_ref="$2"
			shift 2 ;;
		--help|-h) usage; exit 0 ;;
		-*) printf 'Unknown flag: %s\n\n' "$1" >&2; usage >&2; exit 1 ;;
		*)
			[ "$archive_set" -eq 0 ] || { printf '%s\n\n' 'Archive name was provided more than once.' >&2; usage >&2; exit 1; }
			archive_name="$1"
			archive_set=1
			shift ;;
	esac
done

for command in git sort mktemp wc tr sed cut cmp find; do need_cmd "$command"; done
if command -v sha256sum >/dev/null 2>&1; then
	sha256_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	sha256_tool=shasum
else
	printf '%s\n' 'Missing required command: sha256sum or shasum' >&2
	exit 1
fi

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
[ -n "$repo_root" ] || { printf '%s\n' 'This script must be run inside a git repository.' >&2; exit 1; }
cd "$repo_root"

tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-code-review)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT INT TERM

status_path="${tmpdir}/porcelain-v2.txt"
status_changes_path="${tmpdir}/porcelain-v2-changes.txt"
git status --porcelain=v2 --branch --untracked-files=all > "$status_path"
git status --porcelain=v2 --untracked-files=all > "$status_changes_path"
if [ -s "$status_changes_path" ] && [ "$allow_dirty" -ne 1 ]; then
	printf '%s\n' 'Worktree is dirty; commit/stash it or rerun with --allow-dirty to record local changes.' >&2
	exit 1
fi

head_commit="$(git rev-parse --verify HEAD^{commit})"
if [ -z "$base_ref" ]; then
	base_ref="$(git rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
	if [ -z "$base_ref" ]; then
		if git rev-parse --verify HEAD^ >/dev/null 2>&1; then base_ref=HEAD^; else base_ref=HEAD; fi
	fi
fi
base_commit="$(git rev-parse --verify --end-of-options "${base_ref}^{commit}" 2>/dev/null || true)"
[ -n "$base_commit" ] || { printf 'Base ref does not resolve to a commit: %s\n' "$base_ref" >&2; exit 1; }
merge_base="$(git merge-base "$head_commit" "$base_commit" 2>/dev/null || true)"
[ -n "$merge_base" ] || { printf '%s\n' 'HEAD and base have no merge-base.' >&2; exit 1; }

candidates_path="${tmpdir}/candidates.txt"
included_path="${tmpdir}/included.txt"
manifest_path="${tmpdir}/${MANIFEST_NAME}"
verify_path="${tmpdir}/${VERIFY_NAME}"
build_file_list "$candidates_path" "$included_path"
file_count="$(wc -l < "$included_path" | tr -d '[:space:]')"
[ "$file_count" -gt 0 ] || { printf '%s\n' 'No files matched the code review archive rules.' >&2; exit 1; }

if [ "$list_only" -eq 1 ]; then
	cat "$included_path"
	printf '\n%s files would be included.\n' "$file_count" >&2
	exit 0
fi

need_cmd zip
need_cmd unzip

cat > "$verify_path" <<'EOF'
#!/usr/bin/env sh
set -eu
cd "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)"
manifest="TUSKER_CODE_REVIEW_MANIFEST.txt"
tab="$(printf '\t')"
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-review-verify)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT INT TERM
if command -v sha256sum >/dev/null 2>&1; then
	hash_file() { sha256sum "$1" | sed 's/[[:space:]].*$//'; }
elif command -v shasum >/dev/null 2>&1; then
	hash_file() { shasum -a 256 "$1" | sed 's/[[:space:]].*$//'; }
else
	printf '%s\n' 'Need sha256sum or shasum.' >&2
	exit 1
fi
[ -f "$manifest" ] || { printf '%s\n' 'Missing review manifest.' >&2; exit 1; }
expected="${tmpdir}/expected.txt"
actual="${tmpdir}/actual.txt"
: > "$expected"
count=0
while IFS="$tab" read -r tag classification digest path; do
	[ "$tag" = FILE ] || continue
	[ -n "$classification" ] && [ -n "$digest" ] && [ -n "$path" ] || { printf '%s\n' 'Malformed FILE receipt.' >&2; exit 1; }
	[ -f "$path" ] || { printf 'Missing packaged file: %s\n' "$path" >&2; exit 1; }
	[ "$(hash_file "$path")" = "$digest" ] || { printf 'Hash mismatch: %s\n' "$path" >&2; exit 1; }
	printf '%s\n' "$path" >> "$expected"
	count=$((count + 1))
done < "$manifest"
declared_count="$(sed -n 's/^Target file count: //p' "$manifest")"
[ "$count" = "$declared_count" ] || { printf '%s\n' 'Manifest target count mismatch.' >&2; exit 1; }
find . -type f | sed 's#^\./##' | while IFS= read -r path; do
	case "$path" in
		"$manifest"|TUSKER_CODE_REVIEW_VERIFY.sh) ;;
		*) printf '%s\n' "$path" ;;
	esac
done | LC_ALL=C sort > "$actual"
LC_ALL=C sort -o "$expected" "$expected"
cmp -s "$expected" "$actual" || { printf '%s\n' 'Archive target set is incomplete or contains undeclared files.' >&2; exit 1; }
verify_digest="$(sed -n 's/^Verifier sha256: //p' "$manifest")"
[ "$(hash_file TUSKER_CODE_REVIEW_VERIFY.sh)" = "$verify_digest" ] || { printf '%s\n' 'Verifier hash mismatch.' >&2; exit 1; }
printf 'Verified %s review files.\n' "$count"
EOF
chmod 500 "$verify_path"
verify_digest="$(sha256_file "$verify_path")"
status_digest="$(sha256_file "$status_path")"

{
	printf '%s\n' 'Tusker code review archive v2'
	printf 'Generated UTC: %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	printf 'HEAD: %s\n' "$head_commit"
	printf 'Base: %s\n' "$base_commit"
	printf 'Merge-base: %s\n' "$merge_base"
	printf 'Dirty mode: %s\n' "$allow_dirty"
	printf 'Porcelain-v2 sha256: %s\n' "$status_digest"
	printf 'Target file count: %s\n' "$file_count"
	printf 'Verifier sha256: %s\n' "$verify_digest"
	printf '%s\n' 'PORCELAIN_V2_BEGIN'
	cat "$status_path"
	printf '%s\n' 'PORCELAIN_V2_END'
	while IFS= read -r path; do
		printf 'FILE\t%s\t%s\t%s\n' "$(classify_path "$path")" "$(sha256_file "$path")" "$path"
	done < "$included_path"
} > "$manifest_path"

case "$archive_name" in *.zip) ;; *) archive_name="${archive_name}.zip" ;; esac
case "$archive_name" in /*) archive_path="$archive_name" ;; *) archive_path="${repo_root}/${archive_name}" ;; esac
archive_dir="$(dirname "$archive_path")"
archive_base="$(basename "$archive_path")"
mkdir -p "$archive_dir"
archive_abs="$(cd "$archive_dir" && pwd -P)/${archive_base}"

rm -f "$archive_abs"
zip -q "$archive_abs" -@ < "$included_path"
zip -q -j "$archive_abs" "$manifest_path" "$verify_path"
verify_dir="${tmpdir}/verify"
mkdir "$verify_dir"
unzip -q "$archive_abs" -d "$verify_dir"
(cd "$verify_dir" && sh "./${VERIFY_NAME}") >/dev/null

printf 'Created %s\n' "$archive_abs"
printf 'Included %s files; embedded provenance verifier passed.\n' "$file_count"
