#!/usr/bin/env sh
set -eu

TUSKER_REPO="${TUSKER_REPO:-srv1n/tusker}"
TUSKER_VERSION="${TUSKER_VERSION:-}"
TUSKER_BIN_DIR="${TUSKER_BIN_DIR:-${HOME}/.local/bin}"
INSTALL_CODEX_USER=0
INSTALL_CLAUDE_USER=0
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
  --github-repo <r>   override GitHub repo slug (default: srv1n/tusker)
  --help              show this help

Notes:
  - This installer supports macOS and Linux.
  - It copies the release binary into BIN_DIR; it does not symlink to a local checkout.
  - Binary installs refresh existing root user skills in ~/.agents, ~/.codex, and ~/.claude.
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

need_cmd curl
need_cmd tar
need_cmd install
need_cmd awk

if command -v shasum >/dev/null 2>&1; then
	checksum_tool="shasum"
elif command -v sha256sum >/dev/null 2>&1; then
	checksum_tool="sha256sum"
else
	printf 'Missing required checksum command: install shasum (macOS) or sha256sum (Linux).\n' >&2
	exit 1
fi

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
			| sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
			| head -n 1
	)"
fi

if [ -z "$TUSKER_VERSION" ]; then
	printf 'Failed to resolve the latest release tag for %s\n' "$TUSKER_REPO" >&2
	exit 1
fi

asset="tusker_${TUSKER_VERSION}_${os}_${arch}.tar.gz"
tmpdir="$(mktemp -d 2>/dev/null || mktemp -d -t tusker-install)"
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

archive_path="${tmpdir}/${asset}"
checksums_path="${tmpdir}/checksums.txt"
extract_dir="${tmpdir}/extract"

printf 'Downloading %s from %s...\n' "$asset" "$TUSKER_REPO"
curl -fsSL -o "$archive_path" "${download_root}/${TUSKER_VERSION}/${asset}"
curl -fsSL -o "$checksums_path" "${download_root}/${TUSKER_VERSION}/checksums.txt"

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

checksum_entry="$(
	awk -v name="$asset" '
		{
			line = $0
			sub(/\r$/, "", line)
			checksum = ""
			filename = line
			if (line ~ /^[^[:space:]]+[[:space:]]+/) {
				checksum = line
				sub(/[[:space:]].*$/, "", checksum)
				sub(/^[^[:space:]]+[[:space:]]+/, "", filename)
			} else {
				sub(/^[[:space:]]+/, "", filename)
			}
			if (substr(filename, 1, 1) == "*") {
				filename = substr(filename, 2)
			}
			if (filename == name) {
				matches++
				expected = tolower(checksum)
			}
		}
		END {
			if (matches == 0) {
				print "missing"
			} else if (matches > 1) {
				print "duplicate"
			} else if (expected == "") {
				print "empty"
			} else {
				print "ok " expected
			}
		}
	' "$checksums_path"
)"

case "$checksum_entry" in
	missing)
		printf 'Checksum entry missing for %s in checksums.txt.\n' "$asset" >&2
		exit 1
		;;
	duplicate)
		printf 'Duplicate checksum entries for %s in checksums.txt.\n' "$asset" >&2
		exit 1
		;;
	empty)
		printf 'Checksum entry for %s has an empty SHA-256 value.\n' "$asset" >&2
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
	printf 'Checksum entry for %s has a malformed SHA-256 value.\n' "$asset" >&2
	exit 1
fi

if [ "$actual_checksum" != "$expected_checksum" ]; then
	printf 'Checksum mismatch for %s\nexpected: %s\nactual:   %s\n' "$asset" "$expected_checksum" "$actual_checksum" >&2
	exit 1
fi

mkdir -p "$extract_dir" "$TUSKER_BIN_DIR"
tar -xzf "$archive_path" -C "$extract_dir"
binary_path="${extract_dir}/tusker_${TUSKER_VERSION}_${os}_${arch}/tusker"

install -m 0755 "$binary_path" "${TUSKER_BIN_DIR}/tusker"
printf 'Installed tusker to %s/tusker\n' "$TUSKER_BIN_DIR"

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

if [ -d "${HOME}/.agents/skills/tusker" ] || \
	[ -d "${HOME}/.codex/skills/tusker" ] || \
	[ -d "${HOME}/.claude/skills/tusker" ] || \
	[ -d "${HOME}/.agents/skills/obsidian-vault-tracker" ] || \
	[ -d "${HOME}/.codex/skills/obsidian-vault-tracker" ] || \
	[ -d "${HOME}/.claude/skills/obsidian-vault-tracker" ]; then
	if "${TUSKER_BIN_DIR}/tusker" install --help 2>/dev/null | grep -q -- '--refresh-existing-user-skills'; then
		"${TUSKER_BIN_DIR}/tusker" install --no-bin --refresh-existing-user-skills
	else
		printf 'Existing user skills found, but Tusker %s does not support skill refresh during binary install.\n' "$TUSKER_VERSION"
	fi
fi

if [ "$INSTALL_CODEX_USER" -eq 1 ] || [ "$INSTALL_CLAUDE_USER" -eq 1 ]; then
	set -- install --no-bin
	if [ "$INSTALL_CODEX_USER" -eq 1 ]; then
		set -- "$@" --codex-user
	fi
	if [ "$INSTALL_CLAUDE_USER" -eq 1 ]; then
		set -- "$@" --claude-user
	fi
	"${TUSKER_BIN_DIR}/tusker" "$@"
fi

if [ -n "$INSTALL_REPO_PATH" ]; then
	"${TUSKER_BIN_DIR}/tusker" install --repo "$INSTALL_REPO_PATH" --no-bin
fi

printf 'Tusker %s installed.\n' "$TUSKER_VERSION"
