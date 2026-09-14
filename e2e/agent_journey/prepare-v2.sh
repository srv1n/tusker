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
"$candidate" wave create --file wave-authoring.yaml --request-key fresh-agent --by agent:fixture
"$candidate" projects add --repo . --vault "./$vault"
