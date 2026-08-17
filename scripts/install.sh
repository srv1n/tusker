#!/usr/bin/env sh
set -eu

TUSKER_REPO="${TUSKER_REPO:-srv1n/tusker}"
TUSKER_VERSION="${TUSKER_VERSION:-}"
TUSKER_BIN_DIR="${TUSKER_BIN_DIR:-${HOME}/.local/bin}"
PINNED_MINISIGN_PUBLIC_KEY='TUSKER_RELEASE_PUBLIC_KEY_NOT_PROVISIONED'
TUSKER_INSTALL_TEST_MODE="${TUSKER_INSTALL_TEST_MODE:-0}"
TUSKER_TEST_MINISIGN_PUBLIC_KEY="${TUSKER_TEST_MINISIGN_PUBLIC_KEY:-}"
INSTALL_CODEX_USER=0
INSTALL_CLAUDE_USER=0
INSTALL_ALL_USER_SKILLS=0
INSTALL_REPO_PATH=""

usage() {
	cat <<'EOF'
Usage:
  curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh
  curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh -s -- --codex-user --claude-user
  curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh -s -- --version v0.1.0

Flags:
  --version <tag>     install a specific release tag (default: latest)
  --bin-dir <path>    install the binary into this directory (default: ~/.local/bin)
  --repo <path>       also install repo-local skills into <path>
  --codex-user        install ~/.agents/skills/tusker
  --claude-user       install ~/.claude/skills/tusker
  --all-user-skills   refresh all existing Tusker user skill installs
  --github-repo <r>   override GitHub repo slug (default: srv1n/tusker)
  --help              show this help

Notes:
  - This installer supports macOS and Linux.
  - It copies the release binary into BIN_DIR; it does not symlink to a local checkout.
  - Existing user skills are never refreshed unless an explicit user-skill flag is passed.
  - Release-manifest signatures are mandatory. Until the repository pins the real
    production public key, installation fails closed. Test trust roots require the
    explicit TUSKER_INSTALL_TEST_MODE=1 fixture boundary.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--version)
			TUSKER_VERSION="$2"
			shift 2
			;;
		--bin-dir)
			TUSKER_BIN_DIR="$2"
			shift 2
			;;
		--repo)
			INSTALL_REPO_PATH="$2"
			shift 2
			;;
		--codex-user)
			INSTALL_CODEX_USER=1
			shift
			;;
		--claude-user)
			INSTALL_CLAUDE_USER=1
			shift
			;;
		--all-user-skills)
			INSTALL_ALL_USER_SKILLS=1
			shift
			;;
		--github-repo)
			TUSKER_REPO="$2"
			shift 2
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			printf 'Unknown flag: %s\n\n' "$1" >&2
			usage >&2
			exit 1
			;;
	esac
done

need_cmd() {
	command -v "$1" >/dev/null 2>&1 || {
		printf 'Missing required command: %s\n' "$1" >&2
		exit 1
	}
}

is_sha256() {
	[ "${#1}" -eq 64 ] || return 1
	case "$1" in
		*[!0123456789abcdefABCDEF]*) return 1 ;;
	esac
	return 0
}

validate_archive() {
	archive=$1
	destination=$2
	expected_root="tusker_${TUSKER_VERSION}_${os}_${arch}"
	python3 - "$archive" "$expected_root" "$destination" <<'PY'
import os, pathlib, posixpath, shutil, sys, tarfile
archive, root, destination = sys.argv[1:]
seen = set()
expected = {root, root + "/tusker", root + "/README.md", root + "/LICENSE"}
with tarfile.open(archive, "r:gz") as tf:
    members = tf.getmembers()
    for member in members:
        name = member.name
        normalized = posixpath.normpath(name)
        if normalized in seen:
            raise SystemExit("duplicate archive member: " + name)
        seen.add(normalized)
        if name.startswith("/") or normalized == ".." or normalized.startswith("../"):
            raise SystemExit("unsafe archive member path: " + name)
        if normalized not in expected:
            raise SystemExit("unexpected archive member: " + name)
        if not (member.isdir() or member.isreg()):
            raise SystemExit("unsupported archive member type: " + name)
    missing = expected - seen
    if missing:
        raise SystemExit("archive is missing required members: " + ", ".join(sorted(missing)))
    base = pathlib.Path(destination)
    base.mkdir(mode=0o700, parents=True, exist_ok=False)
    for member in members:
        normalized = posixpath.normpath(member.name)
        target = base.joinpath(*normalized.split("/"))
        if member.isdir():
            target.mkdir(mode=0o755, parents=True, exist_ok=False)
            continue
        target.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
        source = tf.extractfile(member)
        if source is None:
            raise SystemExit("unable to read archive member: " + member.name)
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
        if hasattr(os, "O_NOFOLLOW"):
            flags |= os.O_NOFOLLOW
        fd = os.open(target, flags, 0o755 if normalized.endswith("/tusker") else 0o644)
        with source, os.fdopen(fd, "wb") as output:
            shutil.copyfileobj(source, output)
PY
}

need_cmd curl
need_cmd install
need_cmd awk
need_cmd python3
need_cmd minisign

case "$TUSKER_INSTALL_TEST_MODE" in 0|1) ;; *) printf 'TUSKER_INSTALL_TEST_MODE must be 0 or 1.\n' >&2; exit 1;; esac

if command -v shasum >/dev/null 2>&1; then
	checksum_tool="shasum"
elif command -v sha256sum >/dev/null 2>&1; then
	checksum_tool="sha256sum"
else
	printf 'Missing required checksum command: install shasum (macOS) or sha256sum (Linux).\n' >&2
	exit 1
fi

minisign_public_key=$PINNED_MINISIGN_PUBLIC_KEY
if [ "$TUSKER_INSTALL_TEST_MODE" -eq 1 ]; then minisign_public_key=$TUSKER_TEST_MINISIGN_PUBLIC_KEY; fi
case "$minisign_public_key" in ''|TUSKER_RELEASE_PUBLIC_KEY_NOT_PROVISIONED) printf '%s\n' 'Tusker release public key is not provisioned; installation is blocked.' >&2; exit 1;; esac

uname_s="$(uname -s)"
case "$uname_s" in
	Darwin) os="darwin" ;;
	Linux) os="linux" ;;
	*)
		printf 'Unsupported OS: %s\n' "$uname_s" >&2
		exit 1
		;;
esac

uname_m="$(uname -m)"
case "$uname_m" in
	x86_64|amd64) arch="amd64" ;;
	arm64|aarch64) arch="arm64" ;;
	*)
		printf 'Unsupported architecture: %s\n' "$uname_m" >&2
		exit 1
		;;
esac

api_url="${TUSKER_API_URL:-https://api.github.com}"
download_root="${TUSKER_DOWNLOAD_ROOT:-https://github.com/${TUSKER_REPO}/releases/download}"

if [ -z "$TUSKER_VERSION" ]; then
	TUSKER_VERSION="$(
		curl -fsSL "${api_url}/repos/${TUSKER_REPO}/releases/latest" \
			| python3 -c 'import json, sys; value=json.load(sys.stdin).get("tag_name", ""); print(value if isinstance(value, str) else "")'
	)"
fi

if [ -z "$TUSKER_VERSION" ]; then
	printf 'Failed to resolve the latest release tag for %s\n' "$TUSKER_REPO" >&2
	exit 1
fi
if ! python3 - "$TUSKER_VERSION" <<'PY'
import re, sys
identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
pattern = rf"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-{identifier}(?:\.{identifier})*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
raise SystemExit(0 if re.fullmatch(pattern, sys.argv[1]) else 1)
PY
then
	printf 'Invalid release version: %s\n' "$TUSKER_VERSION" >&2
	exit 1
fi

asset="tusker_${TUSKER_VERSION}_${os}_${arch}.tar.gz"
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-install)"
swapped=0
committed=0
had_previous=0
rollback_binary=''
final_binary=''
cleanup() {
	if [ "$swapped" -eq 1 ] && [ "$committed" -eq 0 ] && [ -n "$final_binary" ]; then
		if [ "$had_previous" -eq 1 ] && [ -n "$rollback_binary" ] && [ -f "$rollback_binary" ]; then
			restore="${final_binary}.restore.$$"
			cp -p "$rollback_binary" "$restore" && mv -f "$restore" "$final_binary" || true
		elif [ "$had_previous" -eq 0 ]; then
			rm -f "$final_binary"
		fi
	fi
	[ -z "${staged_binary:-}" ] || rm -f "$staged_binary"
	[ -z "$rollback_binary" ] || rm -f "$rollback_binary"
	rm -rf "$tmpdir"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

archive_path="${tmpdir}/${asset}"
manifest_path="${tmpdir}/MANIFEST.sha256"
signature_path="${tmpdir}/MANIFEST.sha256.minisig"
extract_dir="${tmpdir}/extract"

printf 'Downloading %s from %s...\n' "$asset" "$TUSKER_REPO"
curl -fsSL -o "$archive_path" "${download_root}/${TUSKER_VERSION}/${asset}"
curl -fsSL -o "$manifest_path" "${download_root}/${TUSKER_VERSION}/MANIFEST.sha256"
curl -fsSL -o "$signature_path" "${download_root}/${TUSKER_VERSION}/MANIFEST.sha256.minisig"
minisign -Vm "$manifest_path" -P "$minisign_public_key" -x "$signature_path" >/dev/null
for required in checksums.txt provenance.json sbom.cdx.json; do
	entry=$(awk -v name="$required" '$2 == name { count++; checksum=$1 } END { if (count == 1) print checksum }' "$manifest_path")
	is_sha256 "$entry" || { printf 'Signed release manifest is missing valid %s coverage.\n' "$required" >&2; exit 1; }
done

case "$checksum_tool" in
	shasum)
		if ! checksum_output="$(shasum -a 256 "$archive_path")"; then
			printf 'Failed to compute SHA-256 for %s using shasum.\n' "$asset" >&2
			exit 1
		fi
		;;
	sha256sum)
		if ! checksum_output="$(sha256sum "$archive_path")"; then
			printf 'Failed to compute SHA-256 for %s using sha256sum.\n' "$asset" >&2
			exit 1
		fi
		;;
esac

actual_checksum="$(printf '%s\n' "$checksum_output" | awk 'NR == 1 { print tolower($1); exit }')"
if [ -z "$actual_checksum" ]; then
	printf 'Failed to compute SHA-256 for %s using %s: command returned an empty checksum.\n' "$asset" "$checksum_tool" >&2
	exit 1
fi
if ! is_sha256 "$actual_checksum"; then
	printf 'Failed to compute SHA-256 for %s using %s: command returned a malformed checksum.\n' "$asset" "$checksum_tool" >&2
	exit 1
fi

checksum_entry="$(awk -v name="$asset" '$2 == name { count++; checksum=tolower($1) } END { if (count == 1) print "ok " checksum; else if (count > 1) print "duplicate"; else print "missing" }' "$manifest_path")"

case "$checksum_entry" in
	missing)
		printf 'Checksum entry missing for %s in signed manifest.\n' "$asset" >&2
		exit 1
		;;
	duplicate)
		printf 'Duplicate checksum entries for %s in signed manifest.\n' "$asset" >&2
		exit 1
		;;
	ok\ *)
		expected_checksum="${checksum_entry#ok }"
		;;
	*)
		printf 'Failed to parse checksum entry for %s.\n' "$asset" >&2
		exit 1
		;;
esac

if ! is_sha256 "$expected_checksum"; then
	printf 'Signed manifest entry for %s has a malformed SHA-256 value.\n' "$asset" >&2
	exit 1
fi

if [ "$actual_checksum" != "$expected_checksum" ]; then
	printf 'Checksum mismatch for %s\nexpected: %s\nactual:   %s\n' "$asset" "$expected_checksum" "$actual_checksum" >&2
	exit 1
fi

mkdir -p "$TUSKER_BIN_DIR"
validate_archive "$archive_path" "$extract_dir"
binary_path="${extract_dir}/tusker_${TUSKER_VERSION}_${os}_${arch}/tusker"

if [ ! -f "$binary_path" ] || [ -L "$binary_path" ]; then
	printf 'Release archive did not contain a regular tusker binary.\n' >&2
	exit 1
fi
if [ -L "$TUSKER_BIN_DIR" ]; then printf 'Refusing symlink binary directory: %s\n' "$TUSKER_BIN_DIR" >&2; exit 1; fi
bin_dir_abs=$(CDPATH= cd -P "$TUSKER_BIN_DIR" && pwd -P)
final_binary="${bin_dir_abs}/tusker"
staged_binary="${bin_dir_abs}/.tusker.install.$$"
rollback_binary="${bin_dir_abs}/.tusker.rollback.$$"
backup_binary="${bin_dir_abs}/tusker.previous"
for target in "$final_binary" "$backup_binary"; do
	[ ! -L "$target" ] || { printf 'Refusing symlink install path: %s\n' "$target" >&2; exit 1; }
done
install -m 0755 "$binary_path" "$staged_binary"
sync
if ! installed_version=$("$staged_binary" version --json 2>/dev/null | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])'); then
	rm -f "$staged_binary"
	printf 'Downloaded tusker binary failed health check.\n' >&2
	exit 1
fi
if [ "$installed_version" != "$TUSKER_VERSION" ]; then
	rm -f "$staged_binary"
	printf 'Downloaded binary version mismatch: expected %s, got %s.\n' "$TUSKER_VERSION" "$installed_version" >&2
	exit 1
fi
if [ -e "$final_binary" ]; then
	had_previous=1
	cp -p "$final_binary" "$rollback_binary"
fi
mv -f "$staged_binary" "$final_binary"
swapped=1
sync
post_version=$("$final_binary" version --json | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')
[ "$post_version" = "$TUSKER_VERSION" ] || { printf '%s\n' 'Installed binary failed post-swap version health check.' >&2; exit 1; }
printf 'Installed tusker to %s\n' "$final_binary"

path_ok=0
OLD_IFS=$IFS
IFS=:
for part in $PATH; do
	if [ "$part" = "$TUSKER_BIN_DIR" ]; then
		path_ok=1
		break
	fi
done
IFS=$OLD_IFS
if [ "$path_ok" -ne 1 ]; then
	printf 'Note: %s is not on your PATH.\n' "$TUSKER_BIN_DIR"
fi

existing_user_skills=0
for user_skill in \
		"${HOME}/.agents/skills/tusker" \
		"${HOME}/.codex/skills/tusker" \
		"${HOME}/.claude/skills/tusker"; do
	if [ -d "$user_skill" ]; then
		existing_user_skills=1
		printf 'Existing Tusker user skill install (not refreshed): %s\n' "$user_skill"
	fi
done
if [ "$existing_user_skills" -eq 1 ] && [ "$INSTALL_ALL_USER_SKILLS" -eq 0 ]; then
	printf 'Refresh explicitly with: %s install --all-user-skills --no-bin\n' "$final_binary"
fi
if [ "$INSTALL_ALL_USER_SKILLS" -eq 1 ]; then
	set -- install --no-bin --all-user-skills
	"$final_binary" "$@"
fi

if [ "$INSTALL_CODEX_USER" -eq 1 ] || [ "$INSTALL_CLAUDE_USER" -eq 1 ]; then
	set -- install --no-bin
	if [ "$INSTALL_CODEX_USER" -eq 1 ]; then
		set -- "$@" --codex-user
	fi
	if [ "$INSTALL_CLAUDE_USER" -eq 1 ]; then
		set -- "$@" --claude-user
	fi
	"$final_binary" "$@"
fi

if [ -n "$INSTALL_REPO_PATH" ]; then
	"$final_binary" install --repo "$INSTALL_REPO_PATH" --no-bin
fi

if [ -f "$rollback_binary" ]; then mv -f "$rollback_binary" "$backup_binary"; fi
committed=1
printf 'Tusker %s installed.\n' "$TUSKER_VERSION"
