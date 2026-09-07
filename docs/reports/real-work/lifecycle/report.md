# Real-work lifecycle: consistent execution and closeout across CLI and UI

Date: 2026-09-07.
Packet: `.tusker/specs/real-work-lifecycle-cli.md`.
Candidate: base commit `f9d2b2b1` plus dirty material in
`cmd/tusker/real_work_lifecycle.go` (new),
`cmd/tusker/real_work_lifecycle_test.go` (new),
`cmd/tusker/commands_v7.go`, `cmd/tusker/cli.go`,
`cmd/tusker/capabilities_cmd.go` (narrow edits).
No demo, UI, fixture, or schema files touched.

## 1. Reproduction (disposable task, not WUX-T-0013)

The WUX-T-0013 incident traced to two different "attempt" notions:

- `attempt start` on a workflow vault claimed a **runtime work session**
  (`workSessionStartCmd` → `RuntimeStore` run lease in a separate worktree).
- `finish` / `attempt handoff` resolved only **file attempt notes**
  (`latestV7AttemptID` over `.tusker/attempts/`), so the same command chain
  returned `No attempt exists`, and `work review` then demanded task status
  `review` which the chain could never reach.

Before: `TestRealWorkLifecycleAliasConsistency` reproduces this shape — start
via `attempt start`, then `finish` fails without ever recognizing the live
session. After: `attempt start` links a file attempt note bound to the
runtime session (`runtime_attempt_id`, `reconciled_from_runtime: true`), and
`handoff`/`finish` materialize that bound note when a live session exists but
no note does. The disposable-task run either advances to proof validation or
completes the handoff; it never reports `No attempt exists` for a session it
was told to create. The single shared error text lives in
`v7MissingAttemptError` (`cmd/tusker/real_work_lifecycle.go`), used by both
the file lookup and the runtime bridge.

Active-command trace (shared services, used identically by UI Serve actions
where noted):

- `work start` / `attempt start` → `claimWorkSession` →
  `runOwnershipService.claimWithAuthorization` (CAS lease + `RunIdentity` +
  `RunAttempt` + `RunAuthorization` in one transaction).
- `work submit` → `runsLifecycleCmd(submit)` →
  `captureRunEndStateForMaterialScope` (branch/head/material fingerprint) →
  `finishWithEndStateAtRevision` → task moves to `review` via `statusCmd`.
- `work review` → `reviewImplementation` / `reviewImplementationParent`
  (binds implementer, source SHA, material scope + fingerprint).
- `review submit` → `reviewSubmitCmd` (task-rev, source-sha, proof/gate and
  material fingerprint checks, reviewer-actor authorization, independence
  check). Serve review actions project the same run/attempt/review rows.
- `close` → `closeV7Cmd` → `v7ClosePreflight` (review + proof + gates +
  acceptor policy). Serve closeout renders the same preflight.

## 2. Exact CLI journey (canonical path; legacy aliases in parentheses)

```sh
tusker work readiness APP-T-0001 --json
tusker work start APP-T-0001 --by agent:luna
# implement in the bound workspace
tusker work reconcile APP-T-0001 --by agent:luna --json
tusker work submit APP-T-0001 --by agent:luna --deliverable "..." \
  --verification "..." --gate-verdicts A1=pass
tusker work review APP-T-0001 --by reviewer:agent
tusker review submit APP-T-0001 --attempt <review-attempt> --by reviewer:agent \
  --task-rev <rev> --source-sha <sha> --work-rev <n> \
  --proof-fingerprint <fp> --gate-fingerprint <fp> \
  --material-fingerprint <fp> --verdict pass --covers A1 --summary "..."
tusker close APP-T-0001 --by reviewer:agent --reason "..."
# legacy: attempt start (= work start + link), attempt handoff / finish,
# verify add, accept; progress/observability below
tusker work progress APP-T-0001 --json
tusker work wait APP-T-0001 --timeout 60 --json
tusker work profile APP-T-0001 --lane execute --json
tusker work cancel APP-T-0001 --by <owner> --reason "..."
tusker work retry APP-T-0001 --by <agent>
```

Shared-checkout route: run `work start` **from the checkout holding the
implementation** (set `workspace.strategy: shared` for a shared checkout;
the default claims an isolated worktree), verify with read-only
`work reconcile`, then `work submit`. An empty-worktree claim is never
treated as an import: submit fingerprints the bound workspace, review
refuses changed scope/material, and `handoff` refuses a file note bound to
neither the live session nor its workspace (`WORKSPACE_MISMATCH`).

## 3. Acceptance matrix

| ID | Result | Evidence |
|---|---|---|
| A1 | Pass. Disposable start/finish mismatch fixed; one consistent replacement path (`work start` canonical, `attempt start` linked). | `TestRealWorkLifecycleAliasConsistency` |
| A2 | Pass. Shared-checkout binding verified read-only before review; unrelated worktree notes and stale revisions refused; no invented history. | `TestRealWorkLifecycleSharedCheckoutReconciliation`, `TestRealWorkLifecycleWrongWorkspaceRefusal` |
| A3 | Pass. Start/progress/submit/review/close operate through supported CLI without GUI or hand-edited state; submit→review and review→done verified. | `TestRealWorkLifecycleCompletionState` (+ progress assertions in alias test) |
| A4 | Pass. Two independent sessions overlap with sufficient capacity; dependent readiness blocked; wave arm requires a human; arming never auto-starts. | `TestRealWorkLifecycleAllowedParallelWaves` |
| A5 | Pass. Cancel releases ownership; late submit refused (CAS); retry opens a truthful second attempt with history preserved. | `TestRealWorkLifecycleCancellationLateResult` |
| A6 | Pass. New commands call the same validators/services as UI paths; machine JSON emitted on success and refusal; failures exit nonzero with codes + hints. | All seven tests assert error codes/non-nil errors and parse JSON output |
| A7 | Pass. Runner/profile/model/harness/fallback recorded on the run and exposed via `work progress`/`work profile`; human gates stay human (agent wave arm refused; review actor authorization unchanged). | Parallel-waves + stale-review tests |
| A8 | Pass (this report). Contracts below; sibling owners connect to these commands/JSON/events. | Sections 4–5 |

## 4. Commands, JSON, and events (handoff)

- New CLI: `work readiness|progress|wait|cancel|retry|reconcile|profile`
  (registered in `cmd/tusker/cli.go`, help in `printWorkSessionHelp`,
  inventory in `installedCapabilityCommands`). Read-only:
  readiness/progress/wait/reconcile/profile. Mutating via existing services:
  cancel→release CAS, retry→start CAS.
- Stable identities in every JSON packet: `task_id`, run
  (`project_id/record_id/active_attempt_id/lease_generation/work_revision`),
  `workspace`, `effective_profile`, `head`, `freshness`, `stage`
  (`unclaimed|in_progress|submitted|in_review|completed|failed|cancelled|…`),
  `blockers` (same `ReadinessBlocker` validators as `work start`), `next`
  (exact follow-up command).
- Failure contract: nonzero exit, `TuskerError` code
  (`WORK_SESSION_*`, `WORKSPACE_MISMATCH`, `WORK_SESSION_STALE`,
  `WAIT_TIMEOUT`, `CAS_CONFLICT`, `NOT_FOUND`, …) with `hint` naming the
  replacement command. `work reconcile` and `work wait` also emit their JSON
  envelope before the nonzero return so unattended callers can parse it.
- Events: no new SSE stream. Reuses existing `attempt_started`,
  `attempt_handoff`, task status/review/close events plus the run
  lease/heartbeat/submit rows the UI already polls (`FindRun`,
  `ListAttemptsForRun`, `LatestRunAuthorization`, `RunIdentity`,
  review results). `ensureFileAttemptForWorkSession` emits
  `attempt_started` with `runtime_attempt_id` for the linked note.
- Effective profile: `work profile` reports the configured lane profile
  from the same workflow source the UI reads plus the run-recorded
  runner/harness/model/effort/fallback. No silent fallback is invented: a
  missing lane profile is a `CONFIG_INVALID` precondition error.

## 5. Verification results

New tests (`cmd/tusker/real_work_lifecycle_test.go`, 7 tests, all pass):

```sh
go test ./cmd/tusker -run '^TestRealWorkLifecycle' -count=1 -v
```

Direct-package execution note: at verification time the package did not
compile in place because the fixture owner's in-progress `demo_*.go` files
(e.g. `demo_realwork.go`: unused imports, bad operator, missing return)
are mid-write. Their files were not touched. Verification ran in
`cmd/tuskverify-local`, an overlay symlinking every current worktree source
byte-identical except the sibling `demo_*.go` set (replaced by stubs for the
7 `demo*Cmd` entry points + `printDemoHelp`, reachable only via
`tusker demo ...`, which no test here exercises), then deleted. Result:
7/7 `TestRealWorkLifecycle*` pass (12.9s). The required command above is
the exact command to re-run once the fixture owner lands compiling demo
files; no test change is needed.

Existing suites for changed paths (same overlay): all `TestWorkSession*`
(except pre-existing env failure below), `TestRunsRelease*`,
`TestV7*Proof/Verify/Attempt/Finish/Handoff*`, `TestAccept*`,
`TestCapabilities*`, `TestReviewResult*/TestReviewer*/TestReview*` — green,
including `TestV7FinishWithoutAttemptPrintsRecoveryCommand` and
`TestV7ProtectedActiveStatusExplainsAttemptFlowAndCapsuleRuntime`, whose
hint substrings the new guidance preserves deliberately.

Pre-existing failures, reproduced identical with and without this change
(base overlay using HEAD versions of the three edited files): all
`TestArmedWave*` / `TestWaveArm*` / `TestWaveDisarm` /
`TestWaveAuthorization*` ("delivery plan is operationally unsafe", from
prior worktree validation mods, untouched by this packet) and
`TestWorkSessionNotificationIsExactRunHintAndDoesNotSpawn` (unix socket
bind not permitted in this sandbox). Not regressions; not caused here.

## 6. Known limitations

- Paid/live harness checks happen only through the user-authorized installed
  runtime; nothing here executes a model. Missing auth/executable/capacity
  surfaces as a precondition error, never as demo success.
- `tusker demo ...` was stubbed out of the verification overlay only; the
  shipped `cmd/tusker` tree still routes demo commands to the fixture
  owner's files.
- Full accepted completion through `review submit --verdict pass` + `close`
  on the runtime path additionally requires satisfied proof/gates per risk
  policy; the completion test proves the ceremony through the file path and
  the submit→review half through the runtime path, plus refusal of early
  close on both.
- `work wait` polls with 1s interval and a 600s cap; it is a convenience
  over the same run row, not a subscription.
