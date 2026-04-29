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

need_cmd curl
need_cmd tar
need_cmd install

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

if command -v shasum >/dev/null 2>&1; then
	actual_checksum="$(shasum -a 256 "$archive_path" | awk '{print $1}')"
elif command -v sha256sum >/dev/null 2>&1; then
	actual_checksum="$(sha256sum "$archive_path" | awk '{print $1}')"
else
	actual_checksum=""
fi

expected_checksum="$(awk -v name="$asset" '$2 == name {print $1}' "$checksums_path" | head -n 1)"
if [ -n "$actual_checksum" ] && [ -n "$expected_checksum" ] && [ "$actual_checksum" != "$expected_checksum" ]; then
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
