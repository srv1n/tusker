# Production readiness convergence — 2026-07-31

Scope: source-only distribution hardening from the GPT-5.6 Pro review. No
automation was enabled, no daemon was started, and no release, push, or ref
movement was performed.

## Finding coverage

The complete finding-to-code-and-test map is maintained in
`docs/specs/25-production-readiness-hardening.md` under **External-review
finding map**. It covers all twelve review findings and their deterministic
regression tests.

## Verification

| Command | Result | Evidence |
| --- | --- | --- |
| Changed Go files: `gofmt -d`, then conditional `gofmt -w` | PASS | No formatting delta; no rewrite required. |
| `git diff --check` | PASS | Clean at initial converged-tree check. |
| `go test ./cmd/tusker -run 'Serve.*Cancel|Project.*Cancel|VaultRootMigration|DiscoverVault' -count=1` | PASS | 5 tests passed. |
| `go test ./cmd/tusker -run 'Execution|Cancellation|Reclaim|ProcessIdentity|Graph|ChildAttention' -count=1` | FAIL, repaired pending rerun | 124 tests passed; stale `TestWorkSessionDeadExpiredHolderReclaims` expected legacy `tusker.yaml`. Config lane repaired the fixture; rerun remains required. |
| `go test ./cmd/tusker -run 'Config|RunnerProfile|ClosePolicy|InitDefaults|SetterReadback|Legacy' -count=1` | FAIL, repair pending | 78 tests passed, 5 failed: dispatch-scope legacy default, close-policy migration idempotence, runner-profile bootstrap preservation, local setter readback, and close-policy test expectation. |
| `go test ./cmd/tusker -count=1` | Pending | Held until focused config suite is green. |
| `go test ./... -count=1` | Pending | Held until package suite is green. |
| `tusker validate --json` | Pending | Held until code-test convergence. |
| `tusker skill doctor --strict --json` | Pending | Held until code-test convergence. |
| Source-only archive smoke | Pending | Held until release gate is green. |

## Current release decision

**Do not distribute yet.** The Serve/Vault lane is green, but the exact
converged-tree config-focused suite has five failures. Full validation and
archive smoke must be rerun after the config repair.
