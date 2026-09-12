# Model-level routing acceptance

Candidate: current dirty checkout on 2026-09-09. No daemon, automation dispatch,
claim, provider turn, or task lifecycle mutation was performed.

| Acceptance | Measured result |
| --- | --- |
| Catalog truth | PASS. Focused tests cover live provenance, per-model reasoning values, and unavailable discovery. Configured profile values are returned as `configured_unverified`; availability remains a separate catalog observation. |
| Inheritance and precedence | PASS. Tests cover built-in, global, project override/reset, explicit task profile, authored work level, and legacy frontier routing. |
| Stable identity and fallback | PASS. Active-cycle retries retain recorded profile/model/effort. A disabled primary may use only its ordered configured fallback; an uncertain error stops selection. Fallback reason is stored on the run. |
| Profile lifecycle | PASS. Disable blocks future direct selection without touching snapshots. Remove refuses level/lane/default/routing/non-terminal-task references under a config lock and revision guard; historical runs are retained. |
| Agent CLI | PASS. `models show`, `catalog`, `profile-set`, `profile-disable`, `profile-enable`, `profile-remove`, `set`, and `reset` expose JSON and guarded writes. Existing `runner route` remains the single effective-route explanation command. |

Focused command:

```text
go test ./cmd/tusker -run 'Test(ModelLevels|ServeModelLevels)' -count=1
Go test: 8 passed in 1 package
```

The package-wide Go run was stopped after it produced no output for more than
eight minutes; it did not report a failure before cancellation. Focused model,
Serve, capability and schema checks passed after the concurrent demo edits
settled.

## Shared-checkout closeout compatibility gap

Documents task `WUX-T-0013` exposed an unresolved lifecycle boundary after its
implementation and browser proof were produced directly in the shared checkout:

```text
tusker finish WUX-T-0013 --request-review --json
No attempt exists

tusker attempt start WUX-T-0013 --json
canonical work-session claim created in a separate worktree

tusker finish WUX-T-0013 --request-review --json
No attempt exists
```

The unused separate-worktree claim was released without submission. Its existing
evidence remains attached to the task and must not be represented as proof from
the unrelated claimed workspace.

Required runtime behavior: Tusker needs one supported, explicit reconciliation
path that binds an authorized shared-checkout implementation and its
source-identified receipts to a canonical attempt before review. Until that
exists, `finish`, `attempt start`, and `work review` must return one consistent
repair command and must never imply that creating a new worktree imports earlier
work or evidence.
