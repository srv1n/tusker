#!/usr/bin/env sh
set -eu

DEFAULT_ARCHIVE_NAME="tusker-code-review.zip"

usage() {
	cat <<'EOF'
Usage:
  ./scripts/package-code-review.sh [archive-name.zip]
  ./scripts/package-code-review.sh --list

Creates a shareable zip at the repository root containing source, config,
markdown, docs, and other text artifacts. Generated output, dependencies,
media, binaries, and archives are excluded.

Options:
  --list    print the files that would be included without creating a zip
  --help    show this help
EOF
}

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		printf 'Missing required command: %s\n' "$1" >&2
		exit 1
	}
}

should_skip_path() {
	case "$1" in
		.git/*|.tools/*|node_modules/*|vendor/*)
			return 0
			;;
		dist/*|build/*|out/*|coverage/*)
			return 0
			;;
		tmp/*|temp/*|.tmp/*|.cache/*|.parcel-cache/*)
			return 0
			;;
		site/node_modules/*|site/dist/*|site/.astro/*|site/public/generated/*|site/src/generated/*)
			return 0
			;;
		tusker\ 2/*|tusker/work/*|tusker/epics/*|tusker/events/*|tusker/evidence/*|tusker/attempts/*|tusker/_generated/*|tusker/dashboards/*)
			return 0
			;;
		tusker/_system/project.yaml|tusker/_system/generated/*|tusker/_system/templates.zip)
			return 0
			;;
		.tusker-*|.tusker-*/*|*.log|*.log.*)
			return 0
			;;
		*.zip|*.tar|*.tar.gz|*.tgz)
			return 0
			;;
		*.mp4|*.mov|*.webm|*.png|*.jpg|*.jpeg|*.gif|*.webp|*.ico|*.pdf)
			return 0
			;;
		*.bin|*.exe|*.dmg|*.pkg|*.test|*.prof|*.out)
			return 0
			;;
	esac

	return 1
}

should_include_path() {
	case "$1" in
		.gitignore|*/.gitignore|.gitattributes|*/.gitattributes|.editorconfig|*/.editorconfig)
			return 0
			;;
		AGENTS.md|*/AGENTS.md|CLAUDE.md|*/CLAUDE.md|README|*/README|README.*|*/README.*|LICENSE|*/LICENSE|LICENSE.*|*/LICENSE.*|Makefile|*/Makefile)
			return 0
			;;
		*.go|*.mod|*.sum)
			return 0
			;;
		*.sh|*.bash|*.zsh|*.py|*.js|*.jsx|*.ts|*.tsx|*.mjs|*.cjs)
			return 0
			;;
		*.css|*.scss|*.html|*.astro)
			return 0
			;;
		*.md|*.mdx|*.txt|*.json|*.jsonl|*.yaml|*.yml|*.toml|*.base|*.lock|*.svg)
			return 0
			;;
	esac

	return 1
}

build_file_list() {
	candidates_path="$1"
	included_path="$2"

	git ls-files --cached --modified --others --exclude-standard \
		| LC_ALL=C sort -u > "$candidates_path"

	: > "$included_path"
	while IFS= read -r path; do
		[ -n "$path" ] || continue
		[ -f "$path" ] || continue
		if should_skip_path "$path"; then
			continue
		fi
		if should_include_path "$path"; then
			printf '%s\n' "$path" >> "$included_path"
		fi
	done < "$candidates_path"
}

list_only=0
archive_name="$DEFAULT_ARCHIVE_NAME"
archive_set=0

while [ "$#" -gt 0 ]; do
	case "$1" in
		--list)
			list_only=1
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		-*)
			printf 'Unknown flag: %s\n\n' "$1" >&2
			usage >&2
			exit 1
			;;
		*)
			if [ "$archive_set" -eq 1 ]; then
				printf 'Archive name was provided more than once.\n\n' >&2
				usage >&2
				exit 1
			fi
			archive_name="$1"
			archive_set=1
			shift
			;;
	esac
done

need_cmd git
need_cmd sort
need_cmd mktemp
need_cmd wc

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [ -z "$repo_root" ]; then
	printf 'This script must be run inside a git repository.\n' >&2
	exit 1
fi
cd "$repo_root"

tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-code-review)"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

candidates_path="${tmpdir}/candidates.txt"
included_path="${tmpdir}/included.txt"
manifest_path="${tmpdir}/TUSKER_CODE_REVIEW_MANIFEST.txt"

build_file_list "$candidates_path" "$included_path"
file_count="$(wc -l < "$included_path" | tr -d '[:space:]')"

if [ "$file_count" -eq 0 ]; then
	printf 'No files matched the code review archive rules.\n' >&2
	exit 1
fi

if [ "$list_only" -eq 1 ]; then
	cat "$included_path"
	printf '\n%s files would be included.\n' "$file_count" >&2
	exit 0
fi

need_cmd zip

case "$archive_name" in
	*.zip) ;;
	*) archive_name="${archive_name}.zip" ;;
esac

case "$archive_name" in
	/*)
		archive_path="$archive_name"
		;;
	*/*)
		archive_path="${repo_root}/${archive_name}"
		;;
	*)
		archive_path="${repo_root}/${archive_name}"
		;;
esac

archive_dir="$(dirname "$archive_path")"
archive_base="$(basename "$archive_path")"
mkdir -p "$archive_dir"
archive_abs="$(cd "$archive_dir" && pwd -P)/${archive_base}"

{
	printf 'Tusker code review archive\n'
	printf 'Generated: %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	printf 'Repository: repo-local checkout path intentionally omitted\n'
	printf 'Included files: %s\n\n' "$file_count"
	printf 'Rules:\n'
	printf '%s\n' '- Source, docs, markdown, config, manifests, and text assets are included.'
	printf '%s\n' '- Generated output, dependencies, media, binaries, and archives are excluded.'
	printf '%s\n\n' '- File discovery uses: git ls-files --cached --modified --others --exclude-standard'
	printf 'Included paths:\n'
	cat "$included_path"
} > "$manifest_path"

rm -f "$archive_abs"
zip -q "$archive_abs" -@ < "$included_path"
zip -q -j "$archive_abs" "$manifest_path"

printf 'Created %s\n' "$archive_abs"
printf 'Included %s files.\n' "$file_count"
