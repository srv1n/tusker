#!/usr/bin/env sh
# Validate a release tag and resolve its artifact directory safely.
set -eu

usage() {
	printf '%s\n' "Usage: RELEASE_VERSION=v1.2.3 $0 [--root PATH] [--check-only] [--print-dir] [--prepare-dir]" >&2
}

root="dist/releases"
print_dir=0
prepare_dir=0
check_only=0
while [ "$#" -gt 0 ]; do
	case "$1" in
		--root)
			[ "$#" -ge 2 ] || { usage; exit 2; }
			root=$2
			shift 2
			;;
		--print-dir)
			print_dir=1
			shift
			;;
		--prepare-dir)
			prepare_dir=1
			shift
			;;
		--check-only)
			check_only=1
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			usage
			exit 2
			;;
	esac
done

version=${RELEASE_VERSION-}
case "$version" in
	'') printf '%s\n' 'RELEASE_VERSION is required (expected vMAJOR.MINOR.PATCH).' >&2; exit 1 ;;
	*[!A-Za-z0-9.+-]*) printf '%s\n' 'RELEASE_VERSION contains unsupported characters.' >&2; exit 1 ;;
esac

# Require a conventional v-prefixed SemVer tag, while permitting prerelease and
# build metadata. This excludes whitespace, slashes, shell metacharacters, and
# ambiguous leading-zero numeric components before any path or shell use.
if ! awk -v value="$version" 'BEGIN {
	pattern = "^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)"
	identifier = "(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
	pattern = pattern "(-" identifier "(\\." identifier ")*)?"
	pattern = pattern "(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"
	exit !(value ~ pattern)
}'; then
	printf '%s\n' 'RELEASE_VERSION must match vMAJOR.MINOR.PATCH[-prerelease][+build].' >&2
	exit 1
fi

[ "$check_only" -eq 1 ] && exit 0

[ -n "$root" ] || { printf '%s\n' 'release root must not be empty.' >&2; exit 1; }
mkdir -p "$root"
root_abs=$(cd -P "$root" && pwd -P)
candidate="$root_abs/$version"

# A pre-existing symlink is refused even when it currently points inside the
# root: recursive deletion must never operate through an attacker-controlled
# path. Existing directories are canonicalized and checked for containment.
if [ -L "$candidate" ]; then
	printf '%s\n' "refusing symlink release directory: $candidate" >&2
	exit 1
fi
if [ -e "$candidate" ]; then
	candidate_abs=$(cd -P "$candidate" && pwd -P)
	case "$candidate_abs/" in
		"$root_abs/"*) ;;
		*) printf '%s\n' "release directory escapes root: $candidate_abs" >&2; exit 1 ;;
	esac
fi

if [ "$prepare_dir" -eq 1 ]; then
	# The same validated process owns the destructive operation. The Makefile
	# never interpolates the version into an rm command and never accepts a
	# path returned by a second, independent validation step.
	rm -rf -- "$candidate"
	mkdir -p -- "$candidate"
fi

if [ "$print_dir" -eq 1 ]; then
	printf '%s\n' "$candidate"
fi
