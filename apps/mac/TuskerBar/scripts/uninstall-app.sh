#!/bin/sh
set -eu

DESTINATION_ROOT=${MAC_APP_DIR:-"$HOME/Applications"}
DESTINATION="$DESTINATION_ROOT/TuskerBar.app"
if /usr/bin/pgrep -x TuskerBar >/dev/null 2>&1; then
  /usr/bin/osascript -e 'tell application "TuskerBar" to quit' >/dev/null 2>&1 || /usr/bin/pkill -x TuskerBar || true
fi
if [ -x "$DESTINATION/Contents/Resources/tusker" ] && /usr/bin/pgrep -f '[T]uskerBar.app/Contents/Resources/tusker daemon run' >/dev/null 2>&1; then
  "$DESTINATION/Contents/Resources/tusker" daemon stop --json >/dev/null 2>&1 || true
fi
rm -rf "$DESTINATION"
printf 'Removed: %s\n' "$DESTINATION"
