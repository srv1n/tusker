#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../../../.." && pwd)
SOURCE="$ROOT/apps/mac/TuskerBar/.build/TuskerBar.app"
DESTINATION_ROOT=${MAC_APP_DIR:-"$HOME/Applications"}
DESTINATION="$DESTINATION_ROOT/TuskerBar.app"

if [ ! -d "$SOURCE" ]; then
  echo "TuskerBar.app is missing; run make mac-app first." >&2
  exit 1
fi

mkdir -p "$DESTINATION_ROOT"
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
rm -rf "$DESTINATION"
ditto "$SOURCE" "$DESTINATION"
codesign --verify --deep --strict "$DESTINATION"
open "$DESTINATION"
printf 'Installed and opened: %s\n' "$DESTINATION"
