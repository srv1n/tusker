#!/usr/bin/env sh
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/../.." && pwd -P)
root=$(mktemp -d "${TMPDIR:-/tmp}/tusker-sbom.XXXXXX")
trap 'rm -rf "$root"' EXIT
(cd "$repo" && scripts/generate-release-sbom.py) >"$root/one.json"
(cd "$repo" && scripts/generate-release-sbom.py) >"$root/two.json"
cmp "$root/one.json" "$root/two.json"
python3 - "$root/one.json" <<'PY'
import json, sys
doc = json.load(open(sys.argv[1]))
assert doc["bomFormat"] == "CycloneDX"
assert doc["specVersion"] == "1.5"
assert len(doc["components"]) > 100
PY
printf '%s\n' 'PASS: deterministic non-empty CycloneDX release SBOM'
