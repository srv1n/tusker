#!/bin/sh
set -eu

go test ./internal/runner/... -count=1
go test ./cmd/tusker -run '^Test(RunnerModuleBoundary|RunnerRecoveryConformance|HarnessConformanceReport|RunnerMigrationConformance)$' -count=1
go test ./cmd/tusker -run '^Test(ModelLevels|ServeModelLevels|AgentProfileMigration|AgentProfileTestIdentity)$' -count=1
(cd internal/serve/ui && bun test tests/runner-conformance.test.ts tests/profiles-interaction.test.ts && bun run typecheck && bun run build)
go run ./cmd/tusker runner conformance --harness codex_exec --preset read-only --json >/dev/null
grep -q 'Run live canary' internal/serve/ui/src/features/settings/app/ProfilesSection.tsx
grep -q 'tusker runner conformance' docs/system/runners-and-acp.md

if [ "${TUSKER_RUN_LIVE_TESTS:-0}" = 1 ]; then
  go test ./cmd/tusker -run '^TestInstalledAgentCanaryLifecycle$' -count=1
  for preset in read-only workspace-write-offline workspace-write-network; do
    go run ./cmd/tusker runner conformance --harness codex_exec --preset "$preset" --live --json >/dev/null
  done
  if codex --profile muse exec --help >/dev/null 2>&1; then
    for preset in read-only workspace-write-offline workspace-write-network; do
      go run ./cmd/tusker runner conformance --harness muse --preset "$preset" --live --json >/dev/null
    done
  fi
  if claude auth status --json 2>/dev/null | grep -q '"loggedIn": true'; then
    go run ./cmd/tusker runner conformance --harness claude-code --preset read-only --live --json >/dev/null
  fi
fi
