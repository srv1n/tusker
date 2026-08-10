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
[ ! -L "$TUSKER_CLI" ] || { printf 'Refusing symlink Tusker CLI: %s\n' "$TUSKER_CLI" >&2; exit 1; }
"$TUSKER_CLI" version --json >/dev/null
swift build -c release

BUNDLE="$ROOT/.build/TuskerBar.app"
STAGED_BUNDLE="$ROOT/.build/.TuskerBar.app.build.$$"
SWAP_HELPER="$ROOT/.build/.tusker-atomic-swap.$$"
SWAPPED=0
HAD_BUNDLE=0
cleanup() {
  if [ "$SWAPPED" -eq 1 ]; then
    if [ "$HAD_BUNDLE" -eq 1 ] && [ -e "$STAGED_BUNDLE" ] && [ -e "$BUNDLE" ]; then
      "$SWAP_HELPER" "$STAGED_BUNDLE" "$BUNDLE" || true
    elif [ "$HAD_BUNDLE" -eq 0 ] && [ -e "$BUNDLE" ]; then
      mv "$BUNDLE" "$STAGED_BUNDLE" 2>/dev/null || true
    fi
  fi
  rm -rf "$STAGED_BUNDLE" "$SWAP_HELPER"
}
rm -rf "$STAGED_BUNDLE"
[ ! -e "$BUNDLE" ] || HAD_BUNDLE=1
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
xcrun clang -Wall -Wextra -Werror -o "$SWAP_HELPER" scripts/atomic-swap.c
mkdir -p "$STAGED_BUNDLE/Contents/MacOS" "$STAGED_BUNDLE/Contents/Resources"
cp .build/release/TuskerBar "$STAGED_BUNDLE/Contents/MacOS/TuskerBar"
cp "$TUSKER_CLI" "$STAGED_BUNDLE/Contents/Resources/tusker"
chmod 755 "$STAGED_BUNDLE/Contents/Resources/tusker"
cmp "$TUSKER_CLI" "$STAGED_BUNDLE/Contents/Resources/tusker"
"$STAGED_BUNDLE/Contents/Resources/tusker" version --json >/dev/null
cp Info.plist "$STAGED_BUNDLE/Contents/Info.plist"

IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null | awk '/Developer ID Application:/{print $2; exit}')
if [ -n "${IDENTITY:-}" ]; then
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$STAGED_BUNDLE/Contents/Resources/tusker"
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$STAGED_BUNDLE"
else
  codesign --force --sign - "$STAGED_BUNDLE/Contents/Resources/tusker"
  codesign --force --sign - "$STAGED_BUNDLE"
fi

codesign --verify --deep --strict "$STAGED_BUNDLE"
sync
"$SWAP_HELPER" "$STAGED_BUNDLE" "$BUNDLE"
SWAPPED=1
codesign --verify --deep --strict "$BUNDLE"
SWAPPED=0
rm -rf "$STAGED_BUNDLE"
printf 'Built signed app: %s\n' "$BUNDLE"
