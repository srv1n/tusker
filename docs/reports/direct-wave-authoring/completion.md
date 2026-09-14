# Direct wave authoring — delivery-plan removal completion

Status: code cutover complete in this working tree. Independent review is
still pending; this report does not claim all tickets are closed.

Proof classes are labeled honestly: `source` = static/source assertions,
`fixture local` = hermetic Go/UI tests and local demo runs, `built` = static
bundle artifacts, `browser` / `installed` / `provider-live` = not executed
here. No fixture result proves provider runtime behavior.

## Deletion inventory

### Real-vault plan artifacts (frozen evidence: `real-vault-migration.json`,
all `delete_after_conversion`)

- `.tusker/specs/agent-access-editor.plan.yaml`
- `.tusker/specs/agent-access.plan.yaml`
- `.tusker/specs/agent-coordination.plan.yaml`
- `.tusker/specs/compact-project-navigation.plan.yaml`
- `.tusker/specs/direct-wave-authoring.bootstrap.plan.json`
- `.tusker/specs/full-height-workspace.plan.yaml`
- `.tusker/specs/planning-handoff-completion.plan.json`
- `.tusker/specs/project-registration-and-visibility.plan.yaml`
- `.tusker/specs/remaining-product-work.plan.yaml`
- `.tusker/specs/skills-docs.plan.yaml`

### Top-level-schema scratch/templates/fixtures

- `.tusker/scratch/completion-doc-update.plan.yaml`
- `.tusker/scratch/full-height-plan-template.yaml`
- `.tusker/scratch/runner-execution/fingerprint.yaml`
- `.tusker/scratch/runner-execution/plan.yaml`
- `.tusker/scratch/wave-pilot/dag.template.yaml`
- `.tusker/scratch/wave-pilot/repo/.tusker/delivery/single.yaml`
- `.tusker/scratch/wave-pilot/single.template.yaml`
- `docs/delivery/*.yaml` (13 files)
- `docs/plans/24-execution-observability-v2.yaml`
- `docs/plans/25-contract-convergence-and-skill-disclosure-v2.yaml`
- `e2e/agent_journey/fixture/delivery.yaml`

### Go implementation and tests

- `cmd/tusker/delivery_amendment.go` + tests (2 files)
- `cmd/tusker/delivery_bind.go` + test
- `cmd/tusker/delivery_capabilities.go`, `delivery_required_capabilities_test.go`
- `cmd/tusker/delivery_cmd.go`, `delivery_cmd_test.go` (generic transaction
  helpers moved to `v7_document_transaction.go` first; direct-authoring tests
  moved to `direct_authoring_test.go`)
- `cmd/tusker/delivery_context_cmd.go` + test
- `cmd/tusker/delivery_cross_scope.go` + import/review tests (canonical
  `dependency_contracts` projection moved to `dependency_contracts.go`)
- `cmd/tusker/delivery_doctor.go` + test
- `cmd/tusker/delivery_phase_readiness.go` + test
- `cmd/tusker/delivery_review_cmd.go` + test
- `cmd/tusker/delivery_rollout.go` + test
- `cmd/tusker/delivery_start_cmd.go` + test
- `cmd/tusker/delivery_v2.go` + test (plan schema/parser)
- `cmd/tusker/delivery_verification_contract.go` + test (strict-proof semantics
  moved to `strict_proof_contract.go`)
- `cmd/tusker/delivery_wave_bugfix_test.go`
- `cmd/tusker/cross_scope_preflight_gate_test.go`
- `cmd/tusker/factory_intake_contract.go` + test
- `cmd/tusker/serve_delivery.go`, `serve_delivery_snapshot_portable.go`,
  `serve_delivery_snapshot_unix.go` + tests
- `cmd/tusker/v7_wave_refingerprint_test.go`
- `cmd/tusker/wave_execute.go` + test (direct route-check/directive continuity
  helpers moved to `direct_run_authority.go`)
- `cmd/tusker/direct_wave_migration.go` + test (one-time migration complete;
  evidence retained in this reports directory)
- `cmd/tusker/testdata/delivery_review/terminal.golden`

### UI

- `internal/serve/ui/src/features/delivery/DeliveryReview.tsx` (canonical
  direct components moved to `features/workbench/integration/WaveAuthority.tsx`)
- `internal/serve/ui/src/features/v2/{DeliveryScreens,OperationsScreens,TaskScreens,TodayScreens,shared}.tsx`
- `internal/serve/ui/tests/factory-intake.test.ts`,
  `tests/cross-scope-dependencies.test.ts` (plan-based lane)
- `api.ts`/`queries.ts`/`domain.ts`: `DeliveryPlan*`, `DeliveryReview`,
  `DeliveryStartResult`, `DeliveryErrorPayload`, `DeliveryCrossScopeDependency`,
  `deliveryRequest`, `useDelivery*`, `runTask`/`useRunTask`,
  `waveExecute`/`useWaveExecute`, `WaveExecuteResult`, `WaveExecutionReceipt`,
  and the `/delivery/*`, `/tasks/:id/run`, `/waves/:id/execute` endpoints.
- Obsolete hashed `dist/assets` chunks removed by the build output refresh.

### Docs and skill assets

- `docs/system/factory-intake.md`
- `skills/tusker/assets/factory-intake-contract.yaml`

## Preservation mapping (from frozen `real-vault-migration.json`; not
re-derived here)

- `docs/delivery/cfx-immutable-context.yaml` → `W-0001` / `CFX-T-0001`.
- `docs/delivery/model-level-configuration.yaml` → `W-0014`.
- `docs/plans/24-execution-observability-v2.yaml` → durable `ORC-T-0066`–`ORC-T-0076` family.
- `docs/plans/25-contract-convergence-and-skill-disclosure-v2.yaml` → `ORC-T-0083`–`ORC-T-0088`.
- Receipted `.tusker/specs/*.plan.*` files → per-receipt targets recorded in
  `real-vault-migration.json` (`partial_apply`, `explicit_plan_mapping`,
  `final_apply` sections).
- Explicit one-time mapping: `planning-handoff-completion.plan.json`
  (sha256:57c8c220…) → `W-0023`, targets `FLW-T-0037`–`FLW-T-0041`.
- Preserved unresolved dependencies (canonical `dependency_contracts` rows,
  targets absent by design; nothing was created or authorized):
  - `ORC-T-0055 → ORC-T-0041` hard sha256:21eb60a4…
  - `ORC-T-0066 → ORC-T-0041` hard sha256:21eb60a4…
  - `ORC-T-0079 → ORC-T-0041` hard sha256:21eb60a4…
  - `ORC-T-0081 → ORC-T-0048` hard sha256:47352771…
- Git history preserves tracked historical plan sources; durable records
  preserve the current authored work.

## Q1–Q14 offline matrix

Harness: `python3 scripts/test-task-authoring-journey.py --mode offline`.
Matched case count: **59** (Q1–Q13 Go tests + Q14 UI assertions). All PASS.

| Case | Subject | Exact command | Proof class | Result |
|---|---|---|---|---|
| Q1 | authoring request creates durable graph | `go test ./cmd/tusker -run '^(TestDirectWaveAuthoringBatchCreatesDurableGraph)$' -count=1 -v` | fixture local | PASS (1) |
| Q2 | rollback, idempotence, request-key conflict | `go test ./cmd/tusker -run '^(TestDirectWaveAuthoringRollbackIdempotencyAndConflict)$' -count=1 -v` | fixture local | PASS (1) |
| Q3 | task create + CAS update | `go test ./cmd/tusker -run '^(TestDirectWaveAuthoringTaskUpdateCAS)$' -count=1 -v` | fixture local | PASS (1) |
| Q4 | removal scan + no-plan fixture journey | `go test ./cmd/tusker -run '^(TestDirectWaveRemoval.*)$' -count=1 -v` | source + fixture local | PASS (3) |
| Q5 | review reads only durable material | `go test ./cmd/tusker -run '^(TestDirectWaveAuthorityReviewUsesOnlyDurableMaterial)$' -count=1 -v` | fixture local | PASS (1) |
| Q6 | wave start queues eligible roots | `go test ./cmd/tusker -run '^(TestDirectWaveAuthorityWaveStartQueuesEligibleRoots)$' -count=1 -v` | fixture local | PASS (1) |
| Q7 | pause/resume authority + continuity | `go test ./cmd/tusker -run '^(TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity)$' -count=1 -v` | fixture local | PASS (1) |
| Q8 | task start scope and blockers | `go test ./cmd/tusker -run '^(TestDirectStartAuthorityBackgroundScopeAndBlockers)$' -count=1 -v` | fixture local | PASS (1) |
| Q9 | autonomous frontier advance | `go test ./cmd/tusker -run '^(TestDirectWaveAutonomousFrontierAdvancesAfterCompletion)$' -count=1 -v` | fixture local | PASS (1) |
| Q10 | packets preserve wave context + bodies | `go test ./cmd/tusker -run '^(TestDirectWavePacketWaveContextAndScopedBodies)$' -count=1 -v` | fixture local | PASS (1) |
| Q11 | Serve review + Start/Pause/Resume | `go test ./cmd/tusker -run '^(TestDirectWaveServe.*)$' -count=1 -v` | fixture local | PASS (5) |
| Q12 | routing lane honesty | `go test ./cmd/tusker -run '^(TestExternalArchitectRouting.*)$' -count=1 -v` | fixture local | PASS (9) |
| Q13 | proof/dependency/work-session contracts | `go test ./cmd/tusker -run '^(TestV7Proof.*\|TestV7Dependencies.*\|TestWorkSessionLifecycleCASAndExactOnce)$' -count=1 -v` | fixture local | PASS (23) |
| Q14 | UI review detail + authority controls | `cd internal/serve/ui && bun test test/direct-wave-authority.test.ts` | source + fixture local | PASS (15 assertions) |

Browser lane: `python3 scripts/test-task-authoring-journey.py --mode browser`
→ **NOT RUN** (`TUSKER_REALWORK_BASE_URL`/`TUSKER_REALWORK_PROJECT` unset).
Installed/live lane → **NOT RUN** (no `TUSKER_LIVE_*` receipt). Provider-live:
not exercised anywhere.

## Additional verification evidence

- Focused gate `go test ./cmd/tusker -run '^(TestDirectWaveRemoval|TestDirectWaveAuthoring|TestDirectWaveAuthority|TestDirectStartAuthority|TestDirectWaveAutonomous|TestDirectWavePacket|TestDirectWaveServe|TestExternalArchitectRouting)' -count=1 -v`: **67/67 PASS** (`ok 45.7s`).
- `go test ./cmd/tusker -run '^(TestWorkSession|TestV7Proof|TestV7Dependencies|TestWaveAuthorizationFingerprint|TestTaskAuthoring)' -count=1`: PASS (`ok 145.8s`) after removing redundant `model_levels` re-setters that the shared `setDirectEmergencyProfileForAutomationTest` fixture already applies.
- Demo fixture lanes `TestDemoRepeatableE2E`, `TestDemoFailureRetryE2E`,
  `TestDemoGuardsE2E`, `TestRealWorkFixture*`, `TestDispatchConsultsPlanBeforeExecute`,
  `TestDaemonPollDispatchesReleasedReviewHandoffInsideArmedWave`: PASS
  (`ok 85.4s`) after seeding wave integration branches
  (`integration/<WAVE-ID>`) at baseline commit, emitting Acceptance Proof
  columns, passing `--if-revision` on cross-scope dependency updates, and
  mapping `light`/`demanding` demo levels to the configured fixture profiles.
- `go build ./cmd/tusker`: ok. `gofmt`: clean on changed files.
- UI: `bun run typecheck` clean; `bun test` on
  `direct-wave-authority`, `task-authoring-experience`, `wux-inspector`,
  `wux-integration`: **31 pass / 0 fail**; `bun run build` succeeds (built).
- `tusker docs check`: **45 documents pass** (removed the dangling
  `factory-intake.md` managed link from the removal spec's `updates:`).
- `git diff --check`: clean.

## Known unrelated dirty-tree / environment failures

`go test ./cmd/tusker -count=1` did not complete within the default 10-minute
package timeout under load (demo tests build a candidate binary inline). The
named failures below reproduce at HEAD in `/private/tmp/tusker-head-verify`
and are not caused by this cutover:

- `TestSetupCodexACPPackagesAndMakesMachineLocalPrimary`,
  `TestDaemonAutoAdvanceExternalApply{Failure,Success}*`,
  `TestCrashLoopPreRunFailuresLeaveSixthReplacementServingReads`,
  `TestDispatchConsultsPlanBeforeExecuteContinuation`,
  `TestStaleLeaseReleaseForNonDispatchableTaskStates`: the user-global config
  `~/.config/tusker/config.yaml` defines `automation.model_levels` (only the
  `light` level) and a `devin`-harness profile. At HEAD these fail during
  config resolution (`harness "devin" unsupported`); in this tree they resolve
  and then fail on the globally-pinned partial `model_levels` mapping or later
  dispatch assertions. Environment-dependent, pre-existing.
- `TestSharedProjectLoaderAllEntryPoints`: fails identically at HEAD —
  `agent_coordination.go` (pre-existing code) calls `store.ListProjects`
  outside the test's allowlist.

## Residual-reference allowlist

`TestDirectWaveRemovalScanFindsNoLegacySurfaces` enforces the boundary; the
only remaining legacy references are:

- Historical `.tusker/work` task bodies/events and `.tusker/specs` decision
  records — durable history, not executable.
- `.tusker/specs/direct-wave-authoring.md` and `docs/reports/direct-wave-authoring/*` —
  the canonical removal spec and its evidence reports.
- `.tusker/scratch/wave-pilot/*.json|log` historical run receipts.
- `e2e/crashrecovery` / fake-runner "delivery" naming for generic
  deliver/review/landing terminology (the `delivery` word alone is not plan
  authority).
- `e2e/contractconvergence` "preflight" usages unrelated to wave arm/plan
  gating (fixture process reaping, editor discard preflight).
- Git history (not a filesystem scan target).
