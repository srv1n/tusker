#!/bin/sh
set -eu

mode=${1:---offline}
case "$mode" in
  --offline)
    go test ./cmd/tusker ./internal/runner -run 'TestAgentContacts|TestAgentMessages|TestAgentTransport|TestAgentCoordinationE2E|TestClarificationWorkflow|TestArchitectContinuation|TestServeDurableMutations' -count=1
    (cd internal/serve/ui && bun test test/agent-coordination.test.ts && bun run typecheck)
    ;;
  --live)
    go run ./cmd/tusker runner conformance --harness codex_exec --preset read-only --live --exercise print --json
    go run ./cmd/tusker runner conformance --harness muse --preset read-only --live --exercise print --json
    ;;
  *) echo "usage: $0 --offline|--live" >&2; exit 2 ;;
esac
