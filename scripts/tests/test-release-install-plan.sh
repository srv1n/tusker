#!/usr/bin/env sh
set -eu

repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd -P)

darwin_plan=$(make -n -C "$repo" HOST_OS=Darwin install)
case "$darwin_plan" in
  *'install --bin --codex-user --claude-user'*'MAC_PREVIEW=1 '*'install-app.sh'*) ;;
  *) printf '%s\n' 'FAIL: Darwin install does not converge CLI, skills, app, and bundled daemon' >&2; exit 1 ;;
esac
darwin_counts=$(printf '%s\n' "$darwin_plan" | awk '/bun run build/{ui++} /go build -trimpath/{go++} /MAC_PREVIEW=1 .*install-app.sh/{app++} END{print ui+0, go+0, app+0}')
[ "$darwin_counts" = '1 1 1' ] || { printf 'FAIL: Darwin install repeats work (ui go app = %s)\n' "$darwin_counts" >&2; exit 1; }

preview_plan=$(make -n -C "$repo" HOST_OS=Darwin mac-preview)
case "$preview_plan" in
  *'install --bin --codex-user --claude-user'*'MAC_PREVIEW=1 '*'install-app.sh'*) ;;
  *) printf '%s\n' 'FAIL: mac-preview does not reuse the complete install path' >&2; exit 1 ;;
esac

portable_plan=$(make -n -C "$repo" HOST_OS=Linux install)
case "$portable_plan" in
  *'install-app.sh'*) printf '%s\n' 'FAIL: portable install unexpectedly includes TuskerBar' >&2; exit 1 ;;
esac

grep -q 'daemon service refresh --json' "$repo/apps/mac/TuskerBar/scripts/install-app.sh" || {
  printf '%s\n' 'FAIL: macOS preview install leaves the launchd daemon binary stale' >&2
  exit 1
}

printf '%s\n' 'PASS: install plans converge the supported local surfaces'
