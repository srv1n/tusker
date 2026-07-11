#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
REPO_ROOT=$(CDPATH= cd -- "$ROOT/../../.." && pwd)
TUSKER_CLI=${TUSKER_CLI:-"$REPO_ROOT/dist/tusker"}
cd "$ROOT"

if [ ! -x "$TUSKER_CLI" ]; then
  echo "Tusker CLI is missing at $TUSKER_CLI; run make build first." >&2
  exit 1
fi
swift build -c release

BUNDLE="$ROOT/.build/TuskerBar.app"
rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/Contents/MacOS" "$BUNDLE/Contents/Resources"
cp .build/release/TuskerBar "$BUNDLE/Contents/MacOS/TuskerBar"
cp "$TUSKER_CLI" "$BUNDLE/Contents/Resources/tusker"
chmod 755 "$BUNDLE/Contents/Resources/tusker"
cp Info.plist "$BUNDLE/Contents/Info.plist"

IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null | awk '/Developer ID Application:/{print $2; exit}')
if [ -n "${IDENTITY:-}" ]; then
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$BUNDLE/Contents/Resources/tusker"
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$BUNDLE"
else
  codesign --force --sign - "$BUNDLE/Contents/Resources/tusker"
  codesign --force --sign - "$BUNDLE"
fi

codesign --verify --deep --strict "$BUNDLE"
printf 'Built signed app: %s\n' "$BUNDLE"
