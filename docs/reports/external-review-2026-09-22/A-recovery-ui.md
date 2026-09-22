# RESULT

```json
{
  "kind": "review",
  "verdict": "request_changes",
  "risk": "high",
  "summary": "The recovery changes are not coherent across backend projection, wave controls, and mutation readback. Unknown outcomes can be classified as ordinary review failures, an uncertain member removes enabled wave lifecycle controls, and Retry wave can return success without retrying any work. Recovery eligibility also differs between the projection, UI, and endpoint. The new settings batch handler can persist one setting before rejecting another. Findings are grounded in the supplied ZIP, producer-to-consumer source traces, and isolated UI-source checks; full Go and UI suites were not executable with the installed toolchains.",
  "findings": [
    {
      "severity": "high",
      "path": "cmd/tusker/direct_wave_authority.go",
      "line": 657,
      "problem": "Terminal outcome-unknown classification occurs after generic review-failure and task-status review branches. An uncertain review-lane attempt is projected as failed with retry_review, while an uncertain execute attempt whose task has reached review is projected as awaiting_review or proof_blocked. The wave therefore hides the uncertain outcome and can offer an ordinary retry instead of bounded recovery.",
      "fix": "Evaluate the normalized terminal unknown outcome before generic review/failure branches, preserving legitimate completed-work and live-owner precedence. Do not expose ordinary retry_review for an uncertain attempt.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx",
      "line": 151,
      "problem": "Any OUTCOME_UNKNOWN blocker sets the selected wave control to undefined, suppressing an enabled backend Pause or Resume control. In a mixed wave, other tasks can still be executing or queued while the operator loses the wave's pause control.",
      "fix": "Suppress unsafe Start/Retry actions only. Preserve backend-enabled Pause and Resume controls independently of the recovery notice and recovery action.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "cmd/tusker/serve_actions.go",
      "line": 554,
      "problem": "The new multi-setting handler validates and persists each field in sequence. A request containing a valid workspaceMode and invalid concurrency changes workspace.strategy before returning a refusal. The UI can generate this request because its positive-number check accepts fractional concurrency such as 1.5.",
      "fix": "Validate and normalize every supplied setting before performing any write. Reject non-positive or non-integer concurrency in the UI instead of submitting it or silently omitting it.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx",
      "line": 109,
      "problem": "canRetryWave treats any authorized, idle failed wave without a small blocker blacklist as retryable. A wave containing only failed review-lane members passes this predicate, but wave start retries only enabled retry_task recoveries. Clicking Retry wave can return Queued with no queued tasks and leave the failed review unchanged.",
      "fix": "Require at least one member with an enabled retry_task recovery before presenting Retry wave. Leave review failures on their specific Retry review action and preserve the backend lifecycle control when no implementation retry is available.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "cmd/tusker/direct_wave_authority.go",
      "line": 939,
      "problem": "The outcome-unknown recovery projection checks terminal state and live ownership but not recovery lineage. It advertises enabled=true when the uncertain parent attempt cannot be identified or already has a recovery child, although the recovery endpoint refuses both cases.",
      "fix": "Use a shared read-only recovery eligibility check that includes parent identification and the existing-child bound. Project enabled=false and the same refusal reason when no further recovery can be admitted.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx",
      "line": 184,
      "problem": "WaveIssue manufactures Verify and continue from the OUTCOME_UNKNOWN blocker alone and disables it only while the mutation is pending. It ignores the member's projected recovery.enabled and recovery.reason, so the wave offers an actionable button even when the backend explicitly reports that a live owner prevents recovery.",
      "fix": "Pass the matching member recovery capability into WaveIssue. Render the supplied action, honor enabled, and visibly explain disabled recovery rather than inferring permission from the blocker code.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "internal/serve/ui/src/lib/queries.ts",
      "line": 382,
      "problem": "The new recovery mutations invalidate run detail, run lists, and task lists, but not the singular task-detail key or authoritative wave-review key. When REST succeeds while the event stream is disconnected, the UI can retain the old recovery action and wave state until fallback polling; wave member detail queries without an interval can remain stale longer.",
      "fix": "Broaden recovery settlement invalidation to include the affected task detail, scoped wave reviews, waves, attempts, and affected proof/needs queries. Reuse the broader operator-state invalidation coverage and await the required readback.",
      "creates_followup_task": false
    }
  ]
}
```

# Findings

## 1. High — Review branches hide terminal unknown outcomes

**Primary location:** `cmd/tusker/direct_wave_authority.go:657–663`.

The new `outcome_unknown` branch is too late in the lifecycle switch:

* At `cmd/tusker/direct_wave_authority.go:619–624`, a review-lane run with any nonempty, non-success outcome becomes `phase: failed` with `RUNTIME_FAILED`. This includes a native unknown outcome and legacy `failed` records containing `delivery_unknown`.
* At `cmd/tusker/direct_wave_authority.go:631–642`, a task already in canonical `review` becomes `proof_blocked` or `awaiting_review`, before an uncertain execute-lane outcome is examined.

The first case also selects `retry_review` through `cmd/tusker/direct_wave_authority.go:967–980`. The ordinary review-retry endpoint checks status, ownership, budget, and authorization, but does not distinguish unknown outcomes: `cmd/tusker/serve_runs.go:533–568`.

Consequently, the same attempt can appear as **outcome-unknown in run detail**—the mapping exists at `cmd/tusker/serve_runs.go:229–237`—and as **Failed / Retry review** in the wave. This is more than wording: the offered action bypasses the uncertain-outcome recovery path and its lineage checks.

**Minimal fix:** Move normalized terminal-unknown classification ahead of generic review/failure projection, without displacing genuinely completed work or active ownership. Unknown attempts must not inherit an ordinary review-retry capability.

## 2. High — An uncertain member removes Pause from a still-active wave

**Primary location:** `internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx:150–153`.

`hasUnknownRecovery` causes `enabled` to become `undefined` before the component considers any backend control. That removes **every** wave lifecycle action, not just unsafe retry.

The backend deliberately continues to expose:

* Resume for a paused wave at `cmd/tusker/direct_wave_authority.go:823–824`.
* Pause for an authorized wave at `cmd/tusker/direct_wave_authority.go:825–826`.

A concrete case is an authorized wave with task A in `outcome_unknown`, task B executing, and additional work awaiting admission. The wave UI shows recovery for A but removes the control that stops further wave admissions. A paused wave with an uncertain member also loses its enabled Resume control.

An isolated check against the actual transpiled component confirmed that a mixed unknown/executing fixture with backend-enabled Pause produces **no selected wave control**.

**Minimal fix:** Filter Start/Retry where unsafe, rather than suppressing the whole control set. Keep lifecycle authorization controls separate from the affected member’s recovery action.

## 3. Medium — A refused settings save can still change workspace mode

**Primary location:** `cmd/tusker/serve_actions.go:554–565`.

The handler now collects workspace mode and concurrency into one request, ordering workspace first at `cmd/tusker/serve_actions.go:542–547`. Its loop validates a field and immediately persists it before validating the next.

For example:

```json
{
  "workspaceMode": "shared",
  "maxActiveRunsPerProject": 1.5
}
```

Workspace mode is accepted and written. Concurrency then fails the integer validator at `cmd/tusker/serve_actions.go:628–633`, and the request returns a refusal. The operator sees a failed save even though workspace isolation has changed.

This is reachable from the shipped form: `internal/serve/ui/src/features/product/OperationsScreens.tsx:92–97` accepts any finite positive number. An isolated source-level check confirmed that entering `1.5` submits both fields.

**Minimal fix:** Validate the complete update set before writing any of it. The form should require a positive integer and display a validation error; invalid concurrency should neither be submitted nor silently dropped.

## 4. Medium — Retry wave can be a successful no-op

**Primary location:** `internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx:108–113`.

The retry predicate does not check the member recovery action. A current, authorized, idle wave containing a failed review member with:

```text
recovery.action = retry_review
recovery.enabled = true
```

passes `canRetryWave`. The component then synthesizes an enabled `wave start` control at lines `151–153`, replacing the backend’s lifecycle control.

However, the replay loop at `cmd/tusker/direct_wave_authority.go:1281–1284` handles **only** enabled `retry_task` recoveries. With no ready implementation members, nothing is redriven or queued. The response nevertheless has `Reason = "Queued"` at line `1276` and can return an empty `QueuedTaskIDs` at line `1307`.

An isolated check confirmed that a review-only failure produces `canRetryWave === true` and selects `wave start`.

**Minimal fix:** Require an enabled `retry_task` member before offering Retry wave. Do not imply that implementation replay handles review-lane failures.

## 5. Medium — Projected recovery eligibility omits the bounded-recovery checks

**Primary location:** `cmd/tusker/direct_wave_authority.go:939–950`.

The projection enables `recover_unknown` once the aggregate run is terminal/unknown and has no live owner. It does not inspect attempt lineage.

The endpoint performs two additional, decisive checks:

* No identifiable uncertain parent: refusal at `cmd/tusker/serve_runs.go:477–485`.
* An existing recovery child for that parent: refusal requiring human review at `cmd/tusker/serve_runs.go:487–490`.

The backend therefore returns an enabled capability for states in which that capability cannot be admitted. This remains wrong even in a UI component that correctly honors `recovery.enabled`.

The existing bounded-recovery test explicitly exercises the existing-child refusal, but only by invoking the mutation: `cmd/tusker/walkthrough_recovery_test.go:279–290`. It never checks the corresponding wave projection.

**Minimal fix:** Share the read-only eligibility calculation between projection and admission, including parent lookup and the existing-child bound. Return the human-review reason in the disabled capability; keep the endpoint’s final admission checks.

## 6. Medium — The wave recovery button ignores an explicitly disabled capability

**Primary location:** `internal/serve/ui/src/features/workbench/integration/WaveAuthority.tsx:181–189`.

`WaveIssue` receives a blocker group, not the matching recovery capability. It decides that recovery is available solely from `OUTCOME_UNKNOWN`, and its button is disabled only by `recovery.isPending`.

For a terminal uncertain record whose process group remains alive, the backend explicitly projects:

```text
recovery.action = recover_unknown
recovery.enabled = false
recovery.reason = a live owner still holds this task
```

That guard is at `cmd/tusker/direct_wave_authority.go:944–949`, and admission independently refuses at `cmd/tusker/serve_runs.go:464–470`. Nevertheless, the wave still presents an enabled Verify and continue button and substitutes generic recovery text for the actual reason.

An isolated component check confirmed that supplying `recovery.enabled = false` still leaves this button enabled.

**Minimal fix:** Pass the matching recovery capability into `WaveIssue`; honor both its action and enabled state, and display its reason. This is separate from finding 5: correcting backend eligibility alone does not fix a consumer that discards it.

## 7. Medium — Recovery settlement does not refresh the state that supplied the action

**Primary location:** `internal/serve/ui/src/lib/queries.ts:377–382`.

`useRecovery` delegates to `invalidateRunActionQueries`, whose entire invalidation set is at `internal/serve/ui/src/lib/queries.ts:426–430`:

```text
["run", projectId, taskId]
["runs"]
["tasks"]
```

`["tasks"]` does not invalidate the distinct singular `["task", projectId, taskId]` key, and none of these keys invalidate `["wave-review", projectId, waveId]`.

Normally, stream notifications can mask this. When the stream is disconnected but the REST mutation succeeds, however, the run can refresh while the wave still displays its previous recovery state and action. Wave review relies on the 45-second disconnected fallback defined at `internal/serve/ui/src/lib/stream.ts:4,48–49`. Wave member details use singular task queries without an interval at `internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx:139–142`.

The isolated invalidation check confirmed precisely the three keys above.

**Minimal fix:** Include authoritative wave review and singular task detail in recovery settlement, together with affected proof, attempt, and needs views. The broader coverage already present in `invalidateOperatorState` at `internal/serve/ui/src/lib/queries.ts:433–449` is the appropriate starting point.

# Architecture notes

The material risk is **duplicated action authority**, not a need for a new lifecycle design. `WaveReview` already carries recovery action, eligibility, and refusal reason, but the new wave surface manufactures actions from phase and blocker codes. Meanwhile, the projection itself omits admission checks present in the endpoint. Both gaps must close: truthful backend capabilities are ineffective when the consumer ignores them.

The other material boundary is mutation readback. Stream events should supplement explicit invalidation after a successful local action, not be the only mechanism that updates the authoritative state from which that action was offered.

# Missing tests

**Outcome classification and bounded eligibility:** Extend `cmd/tusker/direct_wave_authority_test.go` to cover native and legacy unknown outcomes in both lanes, including an execute attempt whose task has reached `review`. Assert the member phase, blocker, and recovery action together. Extend `cmd/tusker/walkthrough_recovery_test.go` to compare projected eligibility with endpoint admission for a missing parent and an existing recovery child.

**Wave controls:** Extend `internal/serve/ui/test/direct-wave-authority.test.ts` with a mixed unknown/executing wave, a paused unknown wave, a disabled member recovery, and a review-only failure. Assert that Pause/Resume survive, disabled recovery cannot be invoked, and review-only failure does not offer Retry wave. The existing unknown-outcome test at lines `105–120` currently asserts that Pause is absent; that assertion encodes the regression.

**Settings failure atomicity:** Extend `cmd/tusker/serve_projects_test.go` with a valid workspace change paired with fractional, zero, or malformed concurrency, asserting that refusal leaves configuration unchanged. Add a real input-and-submit test to `internal/serve/ui/tests/projects-settings.test.ts`; its current settings check at lines `63–69` only checks source strings.

**Recovery readback without streaming:** Extend `internal/serve/ui/tests/runs-detail.test.ts` to seed task detail and wave review, settle `recover_unknown`, `adopt_completed`, and `rerun_checks` with the stream unavailable, and assert immediate invalidation/readback of those keys—not just run detail and lists.

**Execution limitation:** The targeted Go command stopped before executing tests because `go.mod:3` requires Go `1.26.5`, while the installed toolchain is `1.23.2`. Bun and installed UI dependencies were unavailable, so no full UI or browser suite was run. The executed UI checks used transpiled source with mocked hooks and component dependencies; they are not substitutes for integration tests.

# If you change one thing

**Fix terminal unknown-outcome precedence in `cmd/tusker/direct_wave_authority.go`.** An uncertain attempt must remain visibly uncertain before lane-specific failure and review-status logic selects its recovery path. Otherwise, even a perfectly refreshed UI can faithfully display the wrong diagnosis and offer the wrong action.
