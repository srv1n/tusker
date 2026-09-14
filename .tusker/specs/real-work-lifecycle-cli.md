---
subject: real-work-lifecycle-cli
title: "Make real task execution and closeout consistent across CLI and UI"
keywords: [repeatable testing, CLI parity, real execution, handoff]
part_of: real-work-test-packets
status: canonical
created: 2026-09-07
read_when: "Implementing or assigning make real task execution and closeout consistent across cli and ui."
skip_when: "Looking up already verified installed behavior; this is an implementation contract."
sources: [repeatable-work-testing.md, work-knowledge-and-retention.md]
decisions_locked: false
capsule:
  what: "Self-contained work packet with scope, ownership, acceptance and verification."
  use_when: "Assigning this bounded work to an implementation agent."
  skip_when: "Orchestrating unrelated tasks or redesigning the product."
---

# Make real task execution and closeout consistent across CLI and UI

## Assignment

You own the ordinary Tusker execution and completion pathway. Implement and verify this packet. Do not take over the fixture or UI work. Suggested work level: Demanding; this is a routing/ownership correctness change, not a new orchestration project.

## User problem and intended experience

The operator currently cannot reliably test a change without manually preparing work. A sibling worker will build one resettable test repository with fictional documentation, a standalone task and three waves. The first acceptance is a real configured Codex Luna task that progresses, is reviewed and closes. Then two waves run concurrently and a third becomes ready after their outputs. Both CLI and UI must use ordinary product mechanisms.

Your prerequisite is to make those mechanisms consistent. Existing source can contain finished implementation while the tracker still says backlog or ready. That is confusing for the human and unusable for automation. Do not solve it by relabeling all work done or adding another status enum.

## Known reproduction and current evidence

A real incident occurred on WUX-T-0013, implemented directly in the shared checkout:

1. `tusker finish WUX-T-0013 --request-review --json` returned `No attempt exists`.
2. `tusker attempt start WUX-T-0013 --json` created a canonical interactive work-session claim in a separate worktree.
3. The same `finish` command still returned `No attempt exists`.
4. `work review` required task status review, so the agent could not complete the intended chain.
5. The unused claim was released without submitting unrelated worktree material. The actual implementation stayed in the shared checkout; its existing evidence must not be misattributed.

Reproduce this safely on a disposable task, not by experimenting on the original task. Current code may have changed; report whether the incident reproduces and trace the active commands to their shared services.

The existing repeatable demo reports three important limitations: normal wave start requires resident runtime authority; normal review-submit requires stamped source identity; demo composes special authorization and reviewer close. Those are gaps to reconcile with the real runtime, not licenses to bypass it.

## Scope

### One consistent lifecycle

Document and implement the supported path for prepare → ready → claim/start → progress → submit → independent review → accepted completion. Reuse existing states, attempts, proof and receipts. A process exiting successfully is not enough to close a task. Failure, cancellation, rejected review and unavailable infrastructure must stay distinct from accepted completion.

Make legacy/alias commands such as `attempt start` and `finish` either participate in that same contract or return one explicit supported replacement command. A command must not instruct the user to create an attempt that the next command cannot recognize.

### Shared-checkout and worktree identity

Provide one explicit supported route for user-authorized work already implemented in a shared checkout. Bind the actual material, workspace, task and revision before review. A new empty worktree must not be treated as an import of old implementation or proof. Snapshot/copy only declared material using existing workspace facilities where needed; preserve untracked and unrelated user work. If material changes after review preparation, stale review must be refused with an actionable remedy.

Do not retroactively invent execution events or duration. Record an adopted/reconciled implementation honestly if that is the supported mechanism. Preserve the distinction between directly authored work and a harness run. The repair path must be visible through CLI and usable by the operator without editing task files.

### Task/wave CLI parity

Expose or repair supported commands for readiness, effective profile selection, start, current progress, bounded wait/watch, cancellation, retry, submit, review, artifact inspection and closeout. These must call the same validators/services used by UI actions. Return JSON with stable task/wave/attempt identities, stages, blockers, source/workspace binding, effective profile and freshness. Invalid requests and failed checks must exit nonzero with actionable reasons.

Human-owned approvals remain human-owned. If a human gate is in the product, provide an authorized CLI route with the same receipt/identity checks as the UI, or identify the exact remaining prerequisite. Never impersonate the human to get an E2E test green.

A user may start either of two independent waves. Explicitly starting both must permit observed overlap when configured capacity is sufficient. Their dependents must wait for native completion rules. A subsequent ready wave must not start without the configured authority. Keep the same resource/lease protections as normal work.

### Profile and event truth

Use installed configured profiles and existing model-level resolution. Codex Luna and Muse are requested harness/model choices, not hard-coded availability assumptions. Record what actually ran, including transport when known. No silent model/transport fallback. Unavailable authentication, executable or capacity is a precondition error, not successful demo execution.

Preserve and expose the existing event/revision mechanism so the UI owner can refresh task/wave/run state. Publish exact event types/payloads, recovery behavior and query endpoints needed; do not make a second SSE stream merely for the fixture.

## Explicit non-goals

- No fixture seeding/reset implementation, UI redesign or Documents parser work.
- No replacement task schema, scheduler framework, custom coding-agent harness or token accounting platform.
- No bulk migration/closure of old tickets.
- No permission-policy weakening or automatic daemon startup from an interactive agent.
- No claim that deterministic timer output establishes real harness conformance.

## Implementation route and ownership

Start with the reported flow in `cmd/tusker/work_session_cmd.go`, `work_session_readiness.go`, `v7_control_cmd.go`, `v7_evidence_attempt_cmd.go`, `review_result.go`, existing attempt/review tests and workspace binding helpers. Inspect `runner_profiles.go` and current runner boundary only when needed. These paths are entry points, not an instruction to edit all of them. Trace callers before changing a shared function.

Own non-demo lifecycle/readiness/review/workspace code and focused tests. Own ordinary command registration in `cmd/tusker/cli.go` and capability entries with narrow edits. Fixture owner owns `demo_*.go` and supplies any demo-only registration diff. UI owner owns frontend/event consumption; agree any small Serve event/projection changes before editing shared Serve files. Update current execution/CLI/task-proof documentation only for behavior actually shipped.

Read first: `docs/reports/model-levels/routing/report.md` section Shared-checkout closeout compatibility gap; `docs/system/runners-and-acp.md`; governing `runner-execution-boundary` spec found through `tusker docs find`. Avoid loading unrelated historical attempts.

## Acceptance criteria

| ID | Required result |
|---|---|
| A1 | A disposable reproduction of the start/finish mismatch is fixed or returns one consistent, usable replacement path. |
| A2 | Shared-checkout implementation is bound to its real material before review; unrelated worktree material and stale review receipts are refused. |
| A3 | Individual start/progress/submit/review/close operates through supported CLI without GUI or hand-edited task state. Final CLI/UI task state reflects accepted completion. |
| A4 | Two authorized independent waves can overlap; joins wait; later readiness does not imply automatic start. |
| A5 | Cancellation releases execution ownership and prevents late success; retry/rejected review preserve prior history and create truthful new attempts. |
| A6 | CLI and UI share validation, permission and outcome semantics; machine JSON and nonzero failure codes support unattended testing. |
| A7 | Actual model/profile/transport and source identity remain inspectable, and explicit human authorization is never fabricated. |
| A8 | The fixture/UI owners receive exact executable command, JSON and event contracts plus relevant limitations. |

## Verification

Add focused behavioral tests under `cmd/tusker/real_work_lifecycle_test.go`, named `TestRealWorkLifecycle...`. Minimum cases: shared-checkout reconciliation, alias consistency, wrong-workspace refusal, stale review refusal, completion state, cancellation/late result and allowed parallel waves. Use existing fixtures/runtime primitives rather than a duplicate state machine.

Required command after adding the tests:

```sh
go test ./cmd/tusker -run '^TestRealWorkLifecycle' -count=1 -v
```

Record the number of executed tests; zero matches is not proof. Run the specific existing work-session/review/workspace tests identified by the changed paths. Do not launch an unbounded full-suite loop. Paid/live checks happen only through the user-authorized installed runtime; a missing runtime prerequisite must be reported honestly.

Deliver `docs/reports/real-work/lifecycle/report.md` with the reproduction before/after, exact CLI journey, candidate identity (base commit plus dirty material where applicable), acceptance matrix, commands/results, known limitations and interface handoff. A behavior/command matrix is the artifact; screenshots are not required for this backend packet.

## Dependencies and completion

Independent implementation can start now. Publish the command/event contract early so sibling workers can connect to it. Coordinate one final genuine task and parallel-wave run with the fixture owner using the same candidate. Do not call this complete based only on unit tests or change another worker's files to satisfy your own tests.
