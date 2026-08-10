#!/usr/bin/env sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
script="$repo/scripts/generate-release-provenance.py"
valid=$(python3 "$script" v1.2.3 0123456789012345678901234567890123456789 1700000000 'go version go1.25.0 darwin/arm64 "quoted"' 'darwin/arm64 linux/amd64')
python3 - "$valid" <<'PY'
import json
import sys

document = json.loads(sys.argv[1])
assert document["version"] == "v1.2.3"
assert document["commit"] == "0123456789012345678901234567890123456789"
assert document["source_date_epoch"] == 1700000000
assert document["targets"] == ["darwin/arm64", "linux/amd64"]
assert '"quoted"' in document["go"]
PY

if python3 "$script" v1.2.3 not-a-commit 1700000000 go 'linux/amd64' >/dev/null 2>&1; then
	printf '%s\n' 'FAIL: malformed commit was accepted' >&2
	exit 1
fi
if python3 "$script" v1.2.3 0123456789012345678901234567890123456789 1700000000 go 'plan/amd64' >/dev/null 2>&1; then
	printf '%s\n' 'FAIL: unsupported target was accepted' >&2
	exit 1
fi
printf '%s\n' 'PASS: release provenance validation'
