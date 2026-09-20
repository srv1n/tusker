# W-0028 walkthrough qualification

Date: 2026-09-16. Scope: the shared source checkout only. This report does not
change tracker state, start a daemon, install a build, call a provider, or
touch `/private/tmp/tusker-walkthrough-20260916`.

## Verdict

**PASS at the deterministic current-checkout boundary.** The selected-project
settings, authoritative status projection, Start/lifecycle/review/delivery,
pause/proof failure handling, and inspector attempt/proof rendering have
focused executable coverage. This is not a browser, installed-product, or
provider qualification.

## Original walkthrough failures retained

The 2026-09-16 human walkthrough reported: a background-work toggle whose
display did not match the persisted setting; Start buried in stacked content;
contradictory overview/detail/task status; unclear prerequisites and next
action; duplicate stacked execution/dependency content; stale/unhelpful proof
text; and unclear worker versus independent-review progress. The source and
deterministic checks below specifically reject those stale, mismatched, and
failed-attempt representations. They do not replace the original human
observation or establish that a running app has changed.

## Build identity and working material

| Field | Observed |
| --- | --- |
| Source HEAD | `94dfd615d0c3bdd8dc513e178c407cd4ebc3e76f` |
| Checkout state | dirty; 184 porcelain entries before this report (shared concurrent work, not cleaned) |
| UI runtime | Bun `1.3.14` |
| Installed build identity | NOT RUN / unknown |
| Browser URL and registered project | NOT supplied |

## Executed evidence

| Acceptance | Command | Result |
| --- | --- | --- |
| A1, A2, A3, A4 | `bun test --cwd internal/serve/ui test/walkthrough-status.test.ts test/walkthrough-layout.test.ts test/walkthrough-inspector.test.ts` | PASS twice consecutively without clearing test state: 21 tests, 100 assertions each pass. |
| A2 | `bun test --cwd internal/serve/ui tests/projects-settings.test.ts tests/walkthrough-project-automation.test.ts` | PASS: 7 tests, 71 assertions. |
| A1, A2, A3 | `TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w28-validation-locks go test ./cmd/tusker -run '^(TestWalkthroughWaveStatus|TestWalkthroughProjectAutomation|TestServeRunDetail)' -count=1 -timeout=6m -v` | PASS: 12 focused tests. |
| A3 | `TUSKER_VALIDATION_LOCK_DIR=/private/tmp/tusker-w28-validation-locks go test ./cmd/tusker -run '^(TestArmedWaveFrontier|TestArmedWaveReviewChecksCrossWaveDependencyOnItsOwnIntegrationBranch|TestDirectWaveAutonomousFrontierAdvancesAfterCompletion|TestDirectWaveAutonomousPausePreservesAuthorityAndContinuity|TestDirectWaveAutonomousTaskStartInsidePausedWave|TestDirectWaveAutonomousServePauseResume)$' -count=1 -timeout=6m -v` | PASS: 6 focused tests. |
| A1, A3 | `bun test --cwd internal/serve/ui test/direct-wave-authority.test.ts test/human-approval-continuation.test.ts` | PASS: 20 tests, 110 assertions. |
| UI integration | `bun run --cwd internal/serve/ui typecheck` | PASS. |
| Source hygiene | `git diff --check` | PASS. |

The first Go invocation without `TUSKER_VALIDATION_LOCK_DIR` failed before the
tests ran because this sandbox cannot create
`.git/tusker-validation.lock`. The supported disposable lock path above was
used for the passing rerun; it is a test-environment restriction, not a
product failure.

## Acceptance map

| ID | Deterministic evidence | Status |
| --- | --- | --- |
| A1 | `walkthrough-status` checks authoritative ready/executing/awaiting-review/reviewing/failed/completed projection, refresh-error precedence over cached completion, overview grouping, task-row/graph phases, human action and blocker display. Its one-observation regression feeds stale task/run data plus authoritative `proof_blocked` and `failed` review members through header, overview, task row, graph, and drawer; it fails if any surface reverts to stale execution. `walkthrough-layout` checks mutually exclusive Tasks/Dependencies/Results views. | PASS |
| A2 | `TestWalkthroughProjectAutomation*` checks selected checkout runtime state, refusal rollback, and concurrent write preservation; UI project settings/readback tests cover the visible project isolation. `TestWalkthroughWaveStatusStaleProofAndPausedPartialFailure` rejects missing, failed and stale proof from acceptance while allowing pending proof before execution. | PASS |
| A3 | The lifecycle test verifies pause projection, active work remains visible, failed setup is explicit, proof-blocked work has no Start, and record-identity occupancy prevents redispatch. The frontier suite verifies independent branch progression, cross-wave integration-branch resolution, pause/resume continuity, and no duplicate admission. The status/UI regression suite verifies worker → review → delivery and discrete review attempts. | PASS at deterministic fixture boundary |
| A4 | This report separates original observations, exact local commands, source identity, and unrun boundaries. No screenshots or transition request/responses exist because no browser transition failed or was executed. | PASS for reporting boundary |

## Inherited-edit audit

Reviewed the named inherited seams: `direct_wave_authority.go`,
`serve_command.go`, toggle control, inspector, review integration, overview
grouping, wave navigation, and authority controls.

- One review projection is used for overview status, wave header, task list,
  graph member state, and authority controls. A review fetch error makes a
  cached terminal summary unavailable rather than falsely completed.
- Wave navigation has one `Wave views` control with exactly Tasks,
  Dependencies, and Results. The task view suppresses its older inline
  dependency details, so the dependency surface is not duplicated.
- Results require a fresh authoritative completed review; stale authorization
  and failed refreshes cannot unlock it.
- The inspector reads canonical attempts, separates the current reviewer from
  completed worker history, labels a failed/stale current attempt's prior
  proof as not current, and receives the selected wave member's authoritative
  `proof_blocked`/`failed` phase when opened from the wave.

No duplicate navigation or stale-proof acceptance gap was found in this
reviewed current source material. The large shared dirty diff was preserved;
this qualification did not revert or normalize unrelated changes.

## Evidence boundaries and NOT RUN

| Boundary | Status | Reason |
| --- | --- | --- |
| Source review | PASS | Current source inspected; it does not execute a product. |
| Deterministic local tests | PASS | Commands above ran against isolated test fixtures in this checkout. |
| Browser walkthrough | NOT RUN | No authorized service URL/project was supplied; no browser was opened. |
| Installed application | NOT RUN | No build/install/restart was authorized or performed. |
| Human acceptance | NOT RUN | No new operator walkthrough or screenshots were collected. |
| Provider / worker attempt | NOT RUN | No daemon was started and no provider attempts were spent. |

To establish product proof, an operator must run the protected disposable
campaign through the installed application and return any transition failure
with its screenshot plus request/response state. Do not reset or otherwise
mutate that campaign as part of this follow-up.
