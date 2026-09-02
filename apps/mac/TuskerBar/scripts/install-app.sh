#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
SOURCE="$ROOT/apps/mac/TuskerBar/.build/TuskerBar.app"
DESTINATION_ROOT=${MAC_APP_DIR:-"$HOME/Applications"}
DESTINATION="$DESTINATION_ROOT/TuskerBar.app"
STAGED="$DESTINATION_ROOT/.TuskerBar.app.install.$$"
BACKUP="$DESTINATION_ROOT/.TuskerBar.app.previous"
[ ! -e "$BACKUP" ] || BACKUP="$DESTINATION_ROOT/.TuskerBar.app.previous.$$"
SWAP_HELPER="$DESTINATION_ROOT/.tusker-atomic-swap.$$"
SWAPPED=0
COMMITTED=0
HAD_DESTINATION=0
cleanup() {
  if [ "$SWAPPED" -eq 1 ] && [ "$COMMITTED" -eq 0 ]; then
    if [ "$HAD_DESTINATION" -eq 1 ] && [ -e "$STAGED" ] && [ -e "$DESTINATION" ]; then
      "$SWAP_HELPER" "$STAGED" "$DESTINATION" || true
    elif [ "$HAD_DESTINATION" -eq 0 ] && [ -e "$DESTINATION" ]; then
      mv "$DESTINATION" "$STAGED" 2>/dev/null || true
    fi
  fi
  rm -rf "$STAGED" "$SWAP_HELPER"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if [ ! -d "$SOURCE" ]; then
  echo "TuskerBar.app is missing; run make mac-app first." >&2
  exit 1
fi
[ ! -L "$SOURCE" ] || { printf 'Refusing symlink app source: %s\n' "$SOURCE" >&2; exit 1; }
[ ! -L "$DESTINATION_ROOT" ] || { printf 'Refusing symlink app destination root: %s\n' "$DESTINATION_ROOT" >&2; exit 1; }
[ ! -L "$DESTINATION" ] || { printf 'Refusing symlink app destination: %s\n' "$DESTINATION" >&2; exit 1; }
[ ! -e "$DESTINATION" ] || HAD_DESTINATION=1
codesign --verify --deep --strict "$SOURCE"
"$SOURCE/Contents/Resources/tusker" version --json >/dev/null

mkdir -p "$DESTINATION_ROOT"
mkdir -p "$HOME/Library/Application Support/tusker/logs"
chmod 700 "$HOME/Library/Application Support/tusker/logs"
for log in "$HOME/Library/Application Support/tusker/logs"/app-daemon.log*; do
  [ -f "$log" ] && chmod 600 "$log"
done
if [ "${MAC_PREVIEW:-}" = 1 ]; then
  # Preview must serve the just-built bundle, not attach to an older launchd daemon.
  "$SOURCE/Contents/Resources/tusker" daemon service stop --json >/dev/null 2>&1 || \
    "$SOURCE/Contents/Resources/tusker" daemon stop --json >/dev/null
fi
if /usr/bin/pgrep -x TuskerBar >/dev/null 2>&1; then
  /usr/bin/osascript -e 'tell application "TuskerBar" to quit' >/dev/null 2>&1 || /usr/bin/pkill -x TuskerBar || true
  sleep 1
fi
if /usr/bin/pgrep -f '[T]uskerBar.app/Contents/Resources/tusker daemon run' >/dev/null 2>&1; then
  "$SOURCE/Contents/Resources/tusker" daemon stop --json >/dev/null 2>&1 || true
  attempts=0
  while /usr/bin/pgrep -f '[T]uskerBar.app/Contents/Resources/tusker daemon run' >/dev/null 2>&1 && [ "$attempts" -lt 20 ]; do
    sleep 0.25
    attempts=$((attempts + 1))
  done
fi
rm -rf "$STAGED"
xcrun clang -Wall -Wextra -Werror -o "$SWAP_HELPER" "$ROOT/apps/mac/TuskerBar/scripts/atomic-swap.c"
ditto "$SOURCE" "$STAGED"
codesign --verify --deep --strict "$STAGED"
sync
"$SWAP_HELPER" "$STAGED" "$DESTINATION"
SWAPPED=1
sync
codesign --verify --deep --strict "$DESTINATION"
"$DESTINATION/Contents/Resources/tusker" version --json >/dev/null
if [ "${MAC_PREVIEW:-}" = 1 ]; then
  "$DESTINATION/Contents/Resources/tusker" daemon service refresh --json >/dev/null
  cmp "$DESTINATION/Contents/Resources/tusker" "$HOME/Library/Application Support/tusker/bin/tusker-daemon"
fi
if ! open "$DESTINATION"; then
  printf '%s\n' 'Opening installed TuskerBar failed; rolling back.' >&2
  exit 1
fi
if [ -e "$STAGED" ]; then
  mv "$STAGED" "$BACKUP"
fi
COMMITTED=1
printf 'Installed and opened: %s\n' "$DESTINATION"
