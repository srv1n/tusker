# External review triage and task waves — 2026-09-22

20 findings: **17 still supported by current source; 3 addressed in source and retained as regression requirements.** No new runtime reproduction, test, build, installation, daemon launch or live provider execution was performed. This is planning and inert task creation only.

Baseline HEAD: `cbc14957` (Keep wave controls available during recovery). The reviewers used an older supplied snapshot and could not run full repository suites. Their isolated reproductions are evidence, not current full-suite or installed qualification. A2/A4/A6 have visible source fixes and focused tests in HEAD, but those tests were not run in this authoring session.

## Created waves and recommended order

| Order | Wave | Stories | Outcome |
| --- | --- | --- | --- |
| 1 | W-0032 | TSK-T-0023–0025 (3) | Prevent lost submissions and shutdown/containment hangs. |
| 2 | W-0033 | TSK-T-0026–0030 (5) | Preserve runtime outcomes and make task/wave admission executable. |
| 3 | W-0034 | TSK-T-0031–0035 (5) | Make recovery truthful and durable, adoption exclusive, readback fresh and invalid settings mutation-free. |

All three were read back as `Planned`, `authorization: inert`, with no wave-review blockers. All 13 task capsules report backlog/held with pending proof. Each wave has concurrency 1. Cross-wave ordering is a recommendation, not an encoded dependency; do not start all three concurrently against one checkout. Within-wave dependencies are encoded. No task or wave was started.

## Stories

### W-0032: External review: safe shutdown and worker submissions

- [TSK-T-0023](../../../.tusker/work/tasks/TSK-T-0023.md): **Prevent shared-checkout worker submission loss** — demanding.
- [TSK-T-0024](../../../.tusker/work/tasks/TSK-T-0024.md): **Make ACP cancellation and teardown bounded under blocked I/O** — demanding.
- [TSK-T-0025](../../../.tusker/work/tasks/TSK-T-0025.md): **Publish terminal status before contained runner cleanup** — demanding.

### W-0033: External review: runtime outcomes and executable admission

- [TSK-T-0026](../../../.tusker/work/tasks/TSK-T-0026.md): **Preserve ACP session binding and observed terminal responses** — demanding.
- [TSK-T-0027](../../../.tusker/work/tasks/TSK-T-0027.md): **Support informational ACP extensions and long permission-heavy turns** — demanding.
- [TSK-T-0028](../../../.tusker/work/tasks/TSK-T-0028.md): **Preserve Muse native terminal outcomes in bounded execution** — standard.
- [TSK-T-0029](../../../.tusker/work/tasks/TSK-T-0029.md): **Allow valid lifecycle members when arming a wave** — standard.
- [TSK-T-0030](../../../.tusker/work/tasks/TSK-T-0030.md): **Make an authorized standalone backlog Start reach dispatch** — demanding.

### W-0034: External review: truthful recovery and settings changes

- [TSK-T-0031](../../../.tusker/work/tasks/TSK-T-0031.md): **Preserve recovery identity across capacity waits and restart** — demanding.
- [TSK-T-0032](../../../.tusker/work/tasks/TSK-T-0032.md): **Align unknown-outcome projection with bounded recovery admission** — demanding.
- [TSK-T-0033](../../../.tusker/work/tasks/TSK-T-0033.md): **Fence adoption against ownership arriving during verification** — demanding.
- [TSK-T-0034](../../../.tusker/work/tasks/TSK-T-0034.md): **Refresh authoritative recovery state without stream events** — standard.
- [TSK-T-0035](../../../.tusker/work/tasks/TSK-T-0035.md): **Validate every execution setting before persisting any field** — standard.

## Finding-by-finding disposition

A = [recovery/UI review](A-recovery-ui.md), B = [runner/ACP review](B-runner-acp.md), C = [admission/ownership review](C-admission-ownership.md). Numbers follow each review's JSON findings order.

| Finding | Problem | Disposition | Task | Current evidence |
| --- | --- | --- | --- | --- |
| A1 | Unknown outcomes hidden by review/failure branches | Open: source-supported | TSK-T-0032 | direct_wave_authority.go lifecycle switch still places uncertainty after review branches. |
| A2 | Recovery notice removes Pause/Resume | Addressed in source | TSK-T-0034 regression | cbc14957 filters unsafe Start rather than all lifecycle controls. |
| A3 | Refused settings batch partially persists | Open: source-supported | TSK-T-0035 | serve_actions.go validates and writes in one loop; OperationsScreens accepts positive fractions. |
| A4 | Review-only Retry wave is a successful no-op | Addressed in source | TSK-T-0034 regression | canRetryWave now requires an enabled retry_task member. |
| A5 | Recovery projection omits parent/child bound | Open: source-supported | TSK-T-0032 | Projection lacks ListAttemptsForRun parent and existing-child checks present in admission. |
| A6 | Wave recovery ignores disabled capability | Addressed in source | TSK-T-0034 regression | WaveIssue now receives member recovery, honors enabled and displays refusal reason. |
| A7 | Recovery settlement leaves authoritative views stale | Open: source-supported | TSK-T-0034 | invalidateRunActionQueries covers only run detail, runs and task lists. |
| B1 | Permission write blocks cancellation-critical lock | Open: source-supported | TSK-T-0024 | respondPermission retains state.mu across writeAll; cancellation needs state.cancel. |
| B2 | Descendant-held stderr hangs ACP Close | Open: source-supported | TSK-T-0024 | Close waits for processDone; ACP exec command has no WaitDelay. |
| B3 | Contained cleanup kills publisher or leaves descendants | Open: source-supported | TSK-T-0025 | ErrWaitDelay kills group before publication; wrapper post-status reap remains ACP-only. |
| B4 | Session response/update handoff race | Open: source-supported | TSK-T-0026 | handleResponse removes pending binding before NewSession assigns c.session. |
| B5 | EOF/deadline can discard matched terminal response | Open: source-supported | TSK-T-0026 | awaitCall closure branches do not reconcile an already-observed response. |
| B6 | Bounded Muse execution loses native terminal meaning | Open: source-supported | TSK-T-0028 | monitorBoundedRunnerCommand omits classifyMuseCLIOutput. |
| B7 | Unknown informational extension poisons ACP | Open: source-supported | TSK-T-0027 | handleRequest recognizes four names, then poisons other notifications. |
| B8 | 65th sequential permission exceeds lifetime cap | Open: source-supported | TSK-T-0027 | permissionInvocations increments against MaxPendingRequests without release. |
| C1 | Shared workspace submissions overwrite/cross-consume | Open: source-supported | TSK-T-0023 | One workspace-scoped lifecycle file and no consuming-run identity comparison; serial guard absent. |
| C2 | Arming rejects valid dependency/done/review members | Open: source-supported | TSK-T-0029 | directWaveArmContractBlockers skips readiness only from dispatch validation. |
| C3 | Standalone backlog Start never reaches scheduling | Open: source-supported | TSK-T-0030 | Active-state filtering precedes directive handling; fallback projection requires armed wave. |
| C4 | Adoption can race a newly arriving owner | Open: source-supported | TSK-T-0033 | Final adoption boundary locks material and checks bytes but does not reacquire claim exclusion. |
| C5 | Capacity waits erase recovery parentage/prompt | Open: source-supported | TSK-T-0031 | Attempt parent and prompt still parse previousRun.LastError. |

## What to fix first, and what not to expand

Fix the shared-checkout submission collision and cancellation deadlock first: they can lose acknowledged work or make recovery unreachable. Restore shared serialization now; per-attempt submission storage to enable shared parallelism is a separate future feature. The task also binds consumption to exact run identity so stale requests cannot cross runs.

Use the existing lifecycle, recovery admission and ownership mechanisms. No new recovery framework, scheduler, store, or blanket wall-clock task timeout. ACP teardown bounds do not impose task-duration limits. Validate an entire settings request before writes; this is validation-failure safety, not a claim of crash-atomic multi-setting persistence.

A1 and A5 belong together because phase selection and eligibility must describe the same recovery operation. B1/B2 share the teardown boundary; B4/B5 share reader-to-caller handoff; B7/B8 share protocol request lifetime. Keep containment, Muse classification, admission and adoption independently reviewable. This is why 20 review findings become 13 stories rather than 20 unrelated tickets.

## Ongoing work and overlap

- Existing W-0029 tasks TSK-T-0008, TSK-T-0009 and TSK-T-0011 cover broad recovery implementation, UI and retained/installed qualification. Their packets were inspected. The new tasks are concrete residual defect corrections, not replacement implementations or authority to close the older tasks. Reuse their interfaces; re-evaluate any newly landed fix before editing.
- W-0030 remains broader walkthrough/product qualification. Its acceptance is not supplied by local fixes here. W-0007/FLW-T-0018/0019 and W-0020/AAC-T-0003 are broader runtime/provider work, not evidence these defects are fixed.
- At initial inspection the dirty checkout contained artifact-retention work in daemon/settings/API files. During authoring, concurrent edits also appeared for **Devin session resume and retained ACP session identity** in runner_acp.go, runner_wrapper.go, run_runtime_commands.go, daemon.go and serve_runs.go, plus their tests and generated UI assets. Those edits were inspected read-only and preserved. They overlap the wrapper, session-handoff and recovery stories, but do not remove the observed internal/acp/client.go races or the LastError lineage dependency. Rebase/coordinate before implementing TSK-T-0024/0025/0026/0031/0032; do not overwrite this ongoing work.
- No attempt was made to qualify or change the concurrent retention/resume implementation. All source claims are inspection-time observations in a changing shared checkout.

## Handoff and verification boundary

Each task contains verified source seams, bounded implementation decisions, three observable acceptance items, exact future focused commands, work level, and failure/interleaving cases. New named regressions are explicitly proposed and must be added where absent; zero matching tests is not proof. All 13 generated packets and capsules were read back. Wave reviews expose the expected work-level routes through existing configured profiles; no routing/configuration was changed.

Most important checks: blocked permission response write (not merely blocked prompt write), real wrapper-owned groups (not child-only harnesses), canonical next_owner projections (not all-agent fixtures), Start through daemon poll (not directive insertion alone), actual recovery dispatch through capacity wait/restart (not manually inserted child), ownership arriving at adoption commit, and actual query-cache/input behavior (not source-string assertions).

Future proof stays at the smallest acceptance-relevant boundary. Existing broader recovery qualification owns retained/provider/installed acceptance; it remains separate. Authoring is complete; implementation and all planned verification remain pending.

Authoring inputs: [wave 1](wave-1.json), [wave 2](wave-2.json), [wave 3](wave-3.json). These requests were created through `tusker wave create`, never by editing protected task state.


## Proof-mapping correction

The original authoring requests omitted the explicit Proof column required by the installed dispatch validator. Verification rows existed, but wave review alone did not reveal that dispatch blocker. At follow-up, W-0032 had already been independently repaired and authorized; its tasks were left untouched. TSK-T-0026 through TSK-T-0035 were amended through revision-checked `tusker task update` with exact command proof cells, contract rebind and dependency-contract rebind. Unforced packet checks across all 13 tasks no longer report missing acceptance proof mappings. Remaining lifecycle/authorization blockers are distinct from this correction. No wave was started by this repair.

The wave JSON files above are historical creation requests bound to their original request keys; do not replay them as updated contracts. The current CLI-managed task records contain the amended authoritative bodies.
