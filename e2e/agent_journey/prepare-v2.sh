#!/bin/sh
set -eu

# Run only from a copied fixture root after ./bin/tusker has been pinned to the
# release candidate. The context fingerprint comes from that candidate and
# repository state; it must never be typed by an agent.
candidate=./bin/tusker
vault=.tusker
: "${TUSKER_STATE_ROOT:=$PWD/$vault/runtime-state}"
export TUSKER_STATE_ROOT
mkdir -p "$TUSKER_STATE_ROOT"

"$candidate" init --vault "$vault" --yes --vault-only --no-mount
cat > "$vault/config.local.yaml" <<'EOF'
# This fixture runs one direct interactive task in the checked-out repository.
# Shared mode is safe here because the task has a declared owned path and no
# daemon or wave dispatch is enabled.
automation:
  workspace:
    strategy: shared
EOF
cp docs/specs/fresh-agent.md "$vault/specs/fresh-agent.md"
"$candidate" delivery plan --spec docs/specs/fresh-agent.md --out "$vault/scratch/context.yaml"
context=$(awk '/^context_fingerprint: / { print $2; exit }' "$vault/scratch/context.yaml")
if [ -z "$context" ]; then
  echo "candidate did not emit a V2 planning context fingerprint" >&2
  exit 1
fi
python3 -c '
import pathlib, sys
path = pathlib.Path(sys.argv[1])
raw = path.read_text()
needle = "sha256:REPLACE_WITH_PINNED_CONTEXT"
if raw.count(needle) != 1:
    raise SystemExit("delivery template has no single context placeholder")
path.write_text(raw.replace(needle, sys.argv[2]))
' delivery.yaml "$context"
"$candidate" delivery import --plan delivery.yaml --by agent:fixture
"$candidate" projects add --repo . --vault "./$vault"
