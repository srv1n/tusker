---
subject: completion-and-integrated-acceptance
title: Completion semantics and integrated acceptance
keywords: [task done, wave complete, acceptance, integration, live testing]
part_of: planning-handoff-and-agent-entry
status: canonical
created: 2026-09-07
read_when: "Joining the completed implementation streams and defining task, wave and final outcome completion."
skip_when: "Reimplementing the existing fixture, lifecycle, model settings or retention slices."
sources: [real-work-test-packets.md, remaining-product-work.md, planning-handoff-and-agent-entry.md]
updates: [landing-and-completion]
decisions_locked: true
capsule:
  what: "Three bounded follow-ups: integrated baseline, completion contract, and live acceptance."
  use_when: "Assigning the final integration and testing work."
  skip_when: "Starting workers or authorizing a daemon from a design session."
---

# Completion semantics and integrated acceptance

## Evidence and scope

The supplied implementation handoffs report lifecycle, fixture, UI and five remaining product slices implemented. These are worker reports, not an independent review of the combined candidate. Lifecycle verification used an overlay excluding unfinished demo code. Fixture offline tests passed, but real execution timed out waiting for a resident runtime. UI deterministic tests passed, but no live screenshots/journey were produced. The product-slice report records 33 full-Go-suite failures and unresolved tracker/documentation drift. Do not equate these reports with integrated release acceptance.

Current sources inspected for this design: scripts/test-real-work-project.sh, docs/reports/real-work/{lifecycle,fixture,ui} reports, UI manual walkthrough, docs/system/landing-and-completion.md and cmd/tusker/v7_wave_brief.go. No product tests or execution were run in this design turn. The UI runbook still calls shipped fixture/docs helpers unavailable. The mixed script describes Muse but exposes Claude-named settings with a claude-code default; correct attribution/configuration must be demonstrated before mixed execution is accepted.

These are implementation-ready Markdown contracts, not allocated task IDs. Existing assigned tasks remain their owners' work. Import remains subject to the existing cross-scope provenance repair; do not create duplicate tasks to bypass it.

## Agreed behavior and implementation recommendation

The following execution decisions are locked for this acceptance effort:

- Task Play authorizes that task through execution, required verification, independent review, landing and completion. Wave Play authorizes the selected wave's task DAG, including automatic release of eligible descendants and configured retries. A completed wave may make dependent waves eligible but never starts them.
- The initial project execution model is one configured shared checkout and working branch, including `main` when explicitly selected, with one executing task at a time per project. Existing worktree, clone and copy strategies remain supported but are not prerequisites for a wave. Parallel shared-checkout execution is accepted only when existing ownership and resource leases prove source, Git/index and build-output safety; otherwise it remains serial.
- A task-owned commit must be available on the configured working target before completion can unlock dependents. A private or otherwise unavailable commit is insufficient. Shared execution stages only task-owned changes, serializes Git/index and exclusive build operations, and preserves unrelated dirty files.
- The executing agent supplies `Result`, optional `Handoff`, and `Limitations` through the existing submission. Tusker derives revision, changed files, verification and review references from recorded execution. Completion stores that submission; a dependent attempt receives relevant direct-predecessor results and handoffs without copying them into the durable task contract.
- Before dependent execution, Tusker checks predecessor completion, predecessor material availability in the selected checkout/revision, execution-packet delivery, and required resource acquisition. The attempt records the revision and predecessor results it consumed. Reopened or changed prerequisites never silently alter a running attempt; unsafe cases block for revalidation.

Three milestones, not three new durable task statuses:

1. Task done: required checks/review and task gates pass; changes are committed and integrated into the designated working target accessible to dependent tasks. A private commit in an isolated worktree is insufficient. Preserve existing landing/close authority and source-bound receipts. For non-code deliverables, use the explicitly configured artifact destination.
2. Wave complete: its required tasks and combined wave checks pass against that working target. This makes downstream dependencies eligible; it does not automatically start a wave. An explicit wave gate blocks its consumers only where required.
3. Outcome accepted: a final ordinary integration/acceptance task spanning the relevant waves checks the combined capability, performs any explicitly requested human test, and satisfies the configured final destination. No new epic/release entity is needed.

The working target may be the final target; then no extra merge is needed. A staging branch may collect multiple waves before final integration. Reuse existing integration-target configuration; introduce minimal optional distinction only where absent. The user agreed to milestones; exact target configuration is an implementation recommendation and must preserve existing projects' behavior.

For a human-before-merge workflow, expose a reproducible candidate revision/build, accept that exact candidate, merge to the final target, and run required post-integration checks. Changed material invalidates affected approval/review through existing binding rules. Integration conflicts or changed merged content require renewed checks. Optional screenshot viewing is not acceptance authority. Required human approval belongs on the final acceptance task unless explicitly needed earlier.

## Packet C1 — Establish one reproducible integrated candidate

Level: Standard. No dependencies for inspection. Own integration reports/runbooks and narrowly identified assembly fixes; preserve all other owners' changes. Do not bulk stage, reset, merge or repair tracker state based solely on a stale completion claim.

Outcome: the operator can test one identified candidate containing all reported streams, without overlay stubs, mismatched daemon/CLI/UI versions or contradictory instructions.

Steps:
- Inventory current candidate changes and identify which reported slices are present. Record source revision plus dirty-diff identity when uncommitted; do not claim a commit includes dirty changes.
- Build the CLI/UI from that candidate and rerun TestRealWorkLifecycle, TestRealWorkFixture and the five TestRemaining acceptance suites together against actual sources. Use existing UI scripts for tests/typecheck/build. Verify nonzero test counts; report exact suite names.
- Classify remaining full-suite failures from current evidence; fix integration regressions only. Wave authorization failures are material blockers for a live wave even if inherited. Keep unrelated failures explicit rather than hiding them.
- Refresh fixture/UI runbooks to current CLI and actual candidate URLs/state roots; keep CLI, Serve and independently operated daemon on the same candidate/runtime scope.
- Inspect mixed-profile routing: expose neutral or accurately named harness parameters, retain needed compatibility aliases, and require configured Codex/Muse identities. An available Claude runner is not Muse proof.
- Supply a concise baseline report, current commands, disposable repo location and exact remaining prerequisites. Reconcile ORC cross-scope or software-factory docs issues only in separately owned repair work; do not fabricate proof or alter old contracts to make a report green.

Acceptance:
A1. Combined focused suites pass without excluded/stubbed product source; UI tests/typecheck/build use the same candidate.
A2. Candidate CLI, UI and resident-runtime version/state identity are verifiable; stale installed binaries are detected before testing.
A3. Current runbook no longer calls implemented fixture/docs commands unavailable; all commands are checked against candidate help.
A4. Codex/Muse mixed configuration resolves actual requested identities without Claude or timer substitution; absent authentication is a clear prerequisite.
A5. Report separates baseline failures, new regressions, deterministic proof and untested live behavior. A runnable standalone path is delivered even if a later stage remains blocked.

Verification: existing focused Go and UI suites; command-help/configuration readback; mixed argument/routing tests if changed. Independent reviewer checks the real candidate, not the previous overlays.

## Packet C2 — Align task, wave and final acceptance with working targets

Level: Demanding. Depends on C1's identified baseline before edits/integration. Own the existing landing/close/target contracts and their CLI/API/UI projections. Inspect cmd/tusker/v7_land_cmd.go, v7_close_authority.go, v7_completion_receipt.go, completion_reactor.go, runtime_store.go, v7_wave_brief.go and work dependency consumers. Do not replace lifecycle code delivered by the earlier stream.

Outcome: several waves can deliver to a working target and unlock downstream work before the operator accepts the combined result and integrates to the final target.

Steps:
- Trace every dependency eligibility and close/landing caller. Establish whether existing target configuration already supports this contract; document verified existing behavior and implement only missing pieces.
- Represent working/final target intent using existing fields where possible; CLI authoring/readback and UI must expose the resolved destination. Preserve defaults and old records.
- Keep task done and wave complete derived from existing proof/review/landing receipts. Verify dependent work actually starts from a revision containing prerequisite outputs.
- Use an ordinary final acceptance/integration task with dependencies on the relevant wave completion boundaries. Reuse existing wave-check representation; if absent, use an ordinary terminal verification task, not a second workflow engine.
- Show task/wave completion and pending final acceptance without labeling reviewed staging work released or approved. Lead final result with capability, evidence, limitations and how to try it.
- Keep task and wave starts manual. Once a wave is explicitly started, automatically release eligible descendants within that authorized DAG. Human acceptance gates only their explicit scope; independent lanes remain eligible.
- Update current landing/completion documentation and skill guidance only after behavior is verified.

Acceptance:
A1. A reviewed task cannot unlock consumers until required landing makes its outputs available on their working target, regardless of workspace strategy.
A2. Two linked waves complete on a staging target while final acceptance remains pending; downstream eligibility works without final-branch merge or automatic start.
A3. Required wave checks failing or a wave-scoped human gate blocks dependent work. A final-only gate does not retroactively block unrelated waves.
A4. Existing direct-to-final projects retain their behavior; no duplicate merge or obligatory human gate appears.
A5. Final acceptance binds candidate material, records explicit human authority only where configured, and survives correct integration. Changed material/conflicts cannot reuse stale approval as fresh proof.
A6. CLI, UI and dependency evaluation agree after restart/reconnect; process exit, attached evidence or review alone cannot close work.
A7. Tests cover staging/direct targets, stale revisions, gates, failed checks and merge conflicts using disposable repositories. No new release entity or status enum unless existing mechanisms demonstrably cannot express the contract.

Verification: focused landing/close/dependency integration tests on temporary Git repositories plus UI projection tests. Independent reviewer validates the material and destination, not just labels.

## Packet C3 — Prove the joined journey and hand over a fresh manual fixture

Level: Standard. Existing standalone and offline baseline checks can begin after C1; completion-specific acceptance requires C2. Own scripts/test-real-work-project.sh, test fixture additions and existing real-work browser acceptance/runbooks. Coordinate changes with C1's script owner by finishing C1 first.

Outcome: an agent proves the supported flow through CLI, then hands the human an untouched ready-to-run seeded project using that same candidate.

Test ladder (stop at the first failing prerequisite for each lane):
1. Run the existing offline script on one explicitly disposable repository, with proof outside reset-owned paths. This establishes fixture/reset mechanics only.
2. Read candidate daemon status, profiles/catalog and effective execute AND review routes. The operator independently starts/configures the resident runtime; an interactive agent does not launch nested workers or a daemon. Configure Luna explicitly for the first Codex test rather than accepting the report's Terra resolution as Luna proof.
3. Run only standalone in real mode first. Observe actual claim/model/transport/progress, produced file, required checks/review/landing and closure. Record exact IDs and material. Then run Alpha serially in its shared checkout; Wave Play releases descendants automatically. Start Alpha and Beta explicitly for overlap only after shared-resource safety is proven, then start Follow-up after prerequisites.
4. Exercise task/wave/final milestone scenarios from C2. Test failure/retry/cancel and review rejection on a real lane when supported; offline variants alone do not prove real recovery.
5. Run live browser acceptance against the same project/candidate; prove SSE reconnect convergence, task detail, DAG, documents, wave promise/result and evidence availability. Capture screenshots. Required-case SKIPs are incomplete, not PASS.
6. Test retention boundaries with injected time, not a seven-day wait. Keep/unkeep/expiry metadata must agree across CLI and UI.
7. Only after Codex is green, run mixed Codex/Muse with actual harness/model/transport attribution. Both profiles using a shared adapter still need distinct profile/model proof. Credentials unavailable means mixed unverified, not a blocker on the earlier Codex result.
8. Preview/reset/reseed the disposable project, preserve proof externally, and hand over project ID, URL, candidate identity and first Play action. Verify no active runs and ready fixture state. Do not launch the freshly prepared human scenario.

Acceptance:
A1. Same-candidate offline, real standalone and wave results are separately evidenced; no timer fallback or synthetic closeout in the real lane.
A2. Serial shared-checkout execution is proven first. Parallel overlap is observed only where ownership/resource coordination makes it safe; otherwise it is precisely reported as unsupported or unverified rather than replaced by worktrees.
A3. Dependent outputs are present in actual consumer workspaces; final acceptance matches C2 and uses source-bound evidence.
A4. Live browser required cases pass with screenshots; connectivity/environment blocks identify the exact unavailable prerequisite.
A5. Mixed identity proof is real or clearly unverified; fixture comments/defaults cannot masquerade as Muse execution.
A6. Final handoff contains a fresh, idle seeded project and exact human steps: individual task first, wave next, parallel waves next, final combined acceptance last.

## First useful checkpoint

Do not wait for C2 to finish before testing existing standalone execution. C1 → real standalone gives immediate product feedback. Then use that baseline to implement C2 and extend C3. This is an integration/acceptance phase, not another broad feature wave.

## Canonical end-to-end execution plan

This is the test order and evidence contract for the integrated candidate. It was written before this integration run executed product tests. Stop each lane at its first unmet prerequisite, preserve earlier proof, and record required skips as `NOT TESTED`, never `PASS`. Current behavior uses the existing demo, task/wave, runner-route, Serve API and browser entry points. The task/wave/final milestones above remain proposed semantics until focused product tests pass; a gap is not permission to add another lifecycle.

Common preconditions: local `main` is inventoried and assembled without absorbing active writers; `CANDIDATE` is built from the recorded commit and dirty-diff identity; proof is outside the reset-owned repository; CLI, embedded UI, dev UI and resident runtime identities are recorded separately; every wait is bounded. Default cleanup is `demo reset` preview, `--yes`, then inert reseed. Reset must reject missing demo ownership, wrong roots, symlink escapes and active attempts.

| # | Proof class | Preconditions and exact action | Expected observations | Timeout / failure | Evidence and cleanup |
|---|---|---|---|---|---|
| 1 | Build / installed identity | Record `git rev-parse HEAD` and `git status`; build repository-supported CLI and UI; read candidate version/build info, `daemon status --json`, state root and project registration. | CLI/UI share the candidate; installed-daemon mismatch is visible before live work. | Build failure is `FAIL`; absent/mismatched runtime blocks only live lanes. | Build logs, version/status JSON, commit and dirty-diff hash. No fixture mutation. |
| 2 | Offline simulation | `scripts/test-real-work-project.sh --mode offline --repo "$REPO" --candidate "$CANDIDATE" --proof-dir "$PROOF/offline"`. | Safe seed/reset; 13 tasks; standalone, Alpha/Beta overlap and joins; Follow-up waits for explicit start; native review/closure; fresh attempts on repeat. | Script exits nonzero on invariant failure; prerequisites exit distinctly. | Machine JSON in proof dir. Finish reset/reseed idle. |
| 3 | Documents / CLI parity | Against the seed run candidate `docs check` and bounded browse/read/backlinks commands from candidate help; open Documents UI, folders and introductions. | Same identity, hierarchy, title/body and metadata in CLI/UI; folder introductions navigate; writes validate. | Missing command is `BLOCKED`, not replaced by file reads as parity proof. | Command JSON plus browser screenshot/API snapshot. Reseed after mutation. |
| 4 | Real standalone | Resolve execute and review routes for `s1`; explicitly select configured Codex Luna; run the script's standalone real entry point. | Claim → progress → verification → review → landing/closure; actual attempt/profile/model/transport and artifact are bound. | 20m; unavailable service/profile/auth is `BLOCKED`; no timer or other-model fallback. | Route/run/status/review JSON and artifact hash. Reset after lane. |
| 5 | Real wave | Resolve routes, explicitly run Alpha, wait terminal and inspect. | Branch overlap within capacity; join starts after both branches; expected outcome and actual result are visible; tasks/wave complete under current rules. | 20m; authorization/capacity/landing failure is material `FAIL` or `BLOCKED` by cause. | Wave brief, attempts, intervals and evidence. Reset after lane. |
| 6 | Real overlapping waves | Explicitly start Alpha and Beta, then Follow-up only after both finish. | Alpha/Beta intervals overlap; results stay distinct; Follow-up is blocked first, becomes ready without auto-start, then consumes actual outputs. | 30m; queued-but-disjoint is not overlap proof and reports capacity. | Timeline, status and consumer artifact hashes. Reset after lane. |
| 7 | Failure / recovery | Run existing `fail-once`, `reject-once` and `cancel` offline variants; repeat real failure/retry/cancel/rejection only where the real harness supports them. | Failure blocks its join while unrelated work proceeds; retry has fresh identity/no duplicate output; cancel has no late success; rejection prevents close until corrected/re-reviewed. | Script timeout applies; unsupported real injection is `NOT TESTED`. | Variant JSON and attempt/review identities. Reset between variants. |
| 8 | Browser + SSE | Serve candidate UI against the same runtime/seed; run `TUSKER_REALWORK_BASE_URL=... TUSKER_REALWORK_PROJECT=... node internal/serve/ui/test/real-work.browser.mjs`; interrupt/reconnect stream. | Documents, inspector, DAG, progress, expected/actual result and evidence match CLI; disconnect shows stale state and reconnect converges without duplicates. | Bounded readiness; required-case skip is incomplete; missing browser/service is `BLOCKED`. | Browser result, matched API/CLI snapshots and screenshots. |
| 9 | Completion semantics | Run focused completion/landing/dependency tests and the seeded journey; exercise final acceptance only if C2 exists. | Task done requires proof/review/landing on working target; wave complete requires members/checks; final outcome waits for explicit acceptance; changed material invalidates stale receipts. | Missing C2 behavior is a documented gap, never forged demo status. | Test output, receipt fingerprints and target revisions. |
| 10 | Retention | Use the existing controlled clock in focused retention tests/API; Keep, unkeep, then advance past seven days. | Kept evidence remains; unkept expires; compact result and expired reference agree in CLI/UI. | Never wait seven real days; missing clock injection is `BLOCKED`. | Before/after metadata JSON and UI/API listing. External proof survives reset. |
| 11 | Mixed provider | After Codex passes, resolve actual Codex and Muse execute/review routes; run mixed entry point with explicit profiles/harnesses. | Attempts show actual Codex and Muse attribution; no Claude, Terra or timer substitution; review identity recorded. | Auth/profile/transport absence blocks mixed only; 30m. | Routes, attribution, review receipts and outputs. Reset after lane. |
| 12 | Human acceptance handoff | Reset preview/apply/reseed; confirm zero active runs and ready `s1`, Alpha and Beta; print PID and candidate URL. Human opens `s1`, then Alpha, then Alpha+Beta, then Follow-up/final acceptance. | Fixture is idle; visible progress/review/completion matches CLI; subjective acceptance is separate. | Active lease/run refuses handoff until supported cancellation settles. Human acceptance is never inferred. | Seed/status JSON, PID/URL/candidate and checklist. Leave idle; do not press Play. |

Execution order is fixed: combined focused checks → offline fixture → real standalone → one wave → overlapping waves/follow-up → browser/SSE → mixed providers → fresh human fixture. `scripts/test-real-work-project.sh` owns machine entry points; `docs/reports/real-work/ui/manual-walkthrough.md` owns human actions. The final report enumerates `PASS`, `FAIL`, `BLOCKED`, and `NOT TESTED` with the first actionable error for every incomplete lane.
