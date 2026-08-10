---
capsule:
  what: "Implementation contract for the 2026-08-09 production-readiness hardening waves."
  use_when:
    - "Reviewing which safety findings were assigned to each hardening lane."
  skip_when:
    - "You need current pass/fail release evidence; read the production hardening implementation report."
---

# Production readiness hardening after GPT-5.6 Pro review

## Outcome

Make the current Tusker working tree safe and truthful enough for day-to-day
distribution. Address every actionable finding in the source-only GPT-5.6 Pro
review while preserving the existing uncommitted config-location migration.

## Constraints

- Interactive agents implement the work; do not enable automation, run a daemon,
  dispatch Tusker workers, move refs, release, or push.
- Preserve unrelated user changes and the current dirty checkout.
- Destructive control must fail closed when immutable execution or process
  identity cannot be proven.
- Configuration effective values and provenance must come from one
  presence-aware layered resolver.
- Historical evidence, current state, project scope, and graph completeness must
  remain distinct in operator projections.
- Run focused tests per lane, then the repository validation gate and full Go
  suite once after convergence.

## External-review finding map

| Finding | Implementation | Deterministic regression coverage |
| --- | --- | --- |
| 1 Historical cancel can hit retry | Immutable `(project, attempt, generation)` control lookup; no task fallback | `TestExecutionControlHistoricalAttemptCannotTargetRetry` |
| 2 Manual reclaim can orphan live work | Exact snapshot CAS and liveness fencing | `TestManualReclaimRequiresExactDeadSnapshot` |
| 3 Presence-insensitive config merge | Raw presence-aware full-schema layer merge and shared provenance | `TestConfigResolverPreservesExplicitZeroFalseAndEmptyCollections` |
| 4 Managed config masks legacy safety | Full resolver used by authority reads | `TestManagedConfigAugmentsLegacyClosePolicyWithoutMaskingIt` |
| 5 Local setter reports failure after write | Exact-vault staged readback and no-op rollback | `TestProjectSetterUsesExactLegacyVaultAndRollsBackNoop` |
| 6 Process-start probe fails open | Unverifiable process identity denies control | `TestProcessIdentityProbeFailureFailsClosed`, `TestExecutionCancellationManagedPIDFence` |
| 7 Cancellation duplicate hides failed outcome | Durable terminal outcome replay | `TestExecutionCancellationEvidenceIsIdempotentAndProviderSafe` |
| 8 Serve cancel lacks project scope | Registered-project resolution and exact match | `TestServeExecutionCancelRequiresRegisteredProjectMatch` |
| 9 Graph page hides ancestry | Separate `topology_partial` disclosure | `TestExecutionGraphProjection` |
| 10 Recovered child stays in attention | Latest-observation current-state derivation | `TestExecutionLifecycleChildAttentionClearsOnRecovery` |
| 11 Vault move can strand/partially mutate | Discoverable destination preflight, rollback, postcondition | `TestVaultRootMigrationRejectsUndiscoverableDestinationBeforeMutation`, `TestVaultRootMigrationValidatesBeforeMoving`, `TestVaultRootMigrationRollsBackWriteFailure`, `TestVaultRootMigrationPostconditionDiscoversDestination` |
| 12 Migration tests assert obsolete writes | Managed write assertions plus legacy-read coverage | `TestInitDefaultsToDotTuskerVault`, `TestInitPreservesLegacyRootConfigAsReadOnlyCompatibilityInput`, `TestMigrateVaultRootMovesLegacyVaultAndUpdatesPointers` |

## Lane 1: execution-control-fencing

Fix findings 1, 2, 6, 7, 9, and 10.

Acceptance:

- Cancelling historical attempt A1 cannot signal or mutate current retry A2.
- Manual reclaim requires exact owner, attempt, generation, revision, and
  process identity; live or unverifiable holders are refused.
- Process-start probing failure is identity-unverifiable, never a match.
- Cancellation idempotency replays the stored terminal outcome.
- Graph pagination discloses missing topology and recovered child attention
  clears while history remains.

Verification:

- `command: go test ./cmd/tusker -run 'Execution|Cancellation|Reclaim|ProcessIdentity|Graph|ChildAttention' -count=1`

Owned paths:

- `cmd/tusker/execution_graph.go`
- `cmd/tusker/execution_graph_test.go`
- `cmd/tusker/execution_lifecycle.go`
- `cmd/tusker/execution_lifecycle_test.go`
- `cmd/tusker/execution_commands.go`
- `cmd/tusker/run_ownership.go`
- `cmd/tusker/run_ownership_test.go`
- `cmd/tusker/runtime_store.go`
- `cmd/tusker/daemon.go`
- focused execution/reclaim/process-identity test files under `cmd/tusker/`

## Lane 2: config-authority-integrity

Fix findings 3, 4, 5, and 12 while completing the existing config-location
migration.

Acceptance:

- Explicit false, zero, empty list, and empty map values override lower layers.
- Validation commands and every authority-sensitive field use the same layered
  resolver and matching provenance.
- Coexisting legacy and managed files cannot silently weaken close, branch,
  mutation, landing, routing, or validation policy.
- Local setters resolve the exact supplied vault and cannot report failure after
  persisting an authority change.
- Fresh-write tests target managed paths; separate tests prove legacy reads.

Verification:

- `command: go test ./cmd/tusker -run 'Config|RunnerProfile|ClosePolicy|InitDefaults|SetterReadback|Legacy' -count=1`

Owned paths:

- `cmd/tusker/runner_profiles.go`
- `cmd/tusker/runner_profiles_test.go`
- `cmd/tusker/v7_control_cmd.go`
- `cmd/tusker/commands_v7.go`
- `cmd/tusker/v7_validation.go`
- `cmd/tusker/v7_land_cmd.go`
- `cmd/tusker/workflow.go`
- `cmd/tusker/install_test.go`
- `internal/v7schema/schema.go`
- `internal/v7policy/close.go`
- existing dirty config-migration files not owned by another lane

## Lane 3: serve-and-vault-safety

Fix findings 8 and 11.

Acceptance:

- Serve cancellation requires a registered project context and exact execution
  project match.
- Vault migration rejects undiscoverable destinations, validates before
  mutation, rolls back partial failures, and proves post-migration discovery.

Verification:

- `command: go test ./cmd/tusker -run 'Serve.*Cancel|Project.*Cancel|VaultRootMigration|DiscoverVault' -count=1`

Owned paths:

- `cmd/tusker/serve_execution_graph.go`
- `cmd/tusker/serve_execution_graph_test.go`
- `cmd/tusker/vault_root_migration.go`
- `cmd/tusker/vault_root_migration_test.go`

## Lane 4: distribution-convergence

Depends on all three implementation lanes.

Acceptance:

- Every external-review finding is mapped to code and a deterministic regression
  test.
- `git diff --check`, formatting, focused lane tests, full Go tests, Tusker
  validation, and skill doctor pass on the converged working tree.
- Independent review finds no P0/P1 correctness or authority issue.
- Distribution/package smoke succeeds without enabling automation or releasing.

Verification:

- `command: gofmt -w cmd internal && git diff --check && go test ./cmd/tusker -count=1 && go test ./... -count=1 && tusker validate --json && tusker skill doctor --strict --json`

<!-- tusker:delivery-import:0eade35aedff8eee:begin -->

## Work streams

- `[[PRH-T-0002]]` implements delivery source `config-authority-integrity`.
- `[[PRH-T-0004]]` implements delivery source `distribution-convergence`.
- `[[PRH-T-0001]]` implements delivery source `execution-control-fencing`.
- `[[PRH-T-0003]]` implements delivery source `serve-and-vault-safety`.

- `[[W-0010]]` is the imported delivery wave.

<!-- tusker:delivery-import:0eade35aedff8eee:end -->
