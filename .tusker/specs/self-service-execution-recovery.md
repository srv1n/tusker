---
subject: self-service-execution-recovery            # unique key; the one right name for this document
keywords: [doctor, safe repair, queued forever, admission, self service, recovery]
part_of: overview       # subject of the parent document this sits under
describes: [cmd/tusker, internal/serve/ui/src/features/workbench, skills/tusker]
status: canonical       # canonical, or superseded (then set superseded_by)
created: 2026-09-22      # date this document was first written
last_verified:          # date @ commit this document was last checked against code
read_when: "Implementing task or wave diagnosis, admission consistency, bounded repair, or recovery feedback."
skip_when: "Looking up shipped command syntax or diagnosing a live task; use current CLI help and docs/system."
sources: [docs/system/delivery-and-waves.md, docs/system/execution-observability.md, docs/system/storage-and-runtime.md]
updates: [docs/system/delivery-and-waves.md, docs/system/execution-observability.md, docs/system/storage-and-runtime.md]
decisions_locked: true
---

# Self-service execution diagnosis and recovery

Implementation proposal approved for task authoring on 2026-09-22. The commands and behavior below are targets, not claims about the installed product. Task creation does not authorize execution. All implementation proof remains pending.

## Product contract

After Play, every authorized task progresses or exposes a specific reason, the next actor, and the supported next action. A normal wait is distinguishable from a recoverable inconsistency, a human decision, and a product defect. Queued indefinitely without an explanation is a defect.

Flow: author and validate -> arm -> wait for eligibility -> claim -> execute -> verify/review -> accept -> unlock dependents. Pause, cancellation, failed verification, rework, unknown outcomes, changed contracts, restarts and capacity contention are explicit branches.

## CLI and diagnostic contract

- Proposed `tusker doctor <TASK-ID|WAVE-ID> [--json]` is read-only. Existing project/vault selectors retain exact checkout identity. It covers contracts, DAG, authorization, project switch, daemon freshness, queued intent, leases, capacity, runner/workspace, proof, review and acceptance. Missing input produces unavailable, never a guessed healthy state.
- Extend the existing ReadinessContract and recovery capability representations. Use one set of facts and stage-specific predicates, not a new universal ready boolean or a second scheduler. Authoring validity and current dispatch eligibility remain separate.
- Findings carry stable code and scope; bounded evidence with source/revision/time; classification; affected tasks; next_actor and retry_after where applicable; a typed recovery action with command argv, preconditions, authority and expected effect; and the postcondition that proves success. Normal waits identify their dependency or capacity owner. Evidence must exclude credentials and raw worker prompts.
- Human output leads with the first actionable cause and groups consequences beneath it. JSON retains all findings. Proposed exit codes: 0 healthy/complete/normal wait, 1 actionable fault or decision, 2 invalid invocation or unavailable diagnosis. The JSON classification, not exit 0 alone, distinguishes ready from waiting. Check readiness remains available via existing wave review --check.
- Proposed `tusker repair <ID>` and `--dry-run` only show a plan. `--safe` applies the allowlisted repairs under existing authority and then diagnoses again. Results name planned/applied/no-op/refused/failed actions and verified or unverified postconditions. No generic execute-an-arbitrary-shell-command interface.

## Safety and continuity

Safe actions are limited to reconciling a stale canonical admission projection, rebuilding a missing queued frontier from still-valid wave authority, and releasing a reservation proven to have no live or uncertain owner. Expired standalone intent requires renewed operator authority; a still-armed wave may reconstruct its own eligible frontier. Revalidation and CAS/lease generation checks happen at mutation time.

Repairs never arm inert work, enable a project, widen scope, change models/budgets, forge proof, approve a human gate, mark a task accepted, or retry an uncertain external effect. Existing recovery/adoption/review actions retain their own guards and are recommended explicitly when needed. Unsupported corruption is reported as a product defect with bounded evidence.

Queue reservations remain distinct from active workers. One task counts once in its scope; its reservation cannot disqualify its own claim. Every counted slot has an owner and reason. Terminal tasks consume no execution slots; an executing reviewer does, a review label alone does not. On crash, reconcile authorization, directive, canonical state, attempt and lease using existing stores and locks. Test interruption between writes rather than claiming a cross-store transaction exists.

Pause stops new wave admissions, retains admitted work, and permits the existing explicit task-scoped override. Cancellation requests bounded teardown and preserves durable terminal outcomes. Resume rechecks material without silently renewing drifted authority. Partial/completed waves never re-execute accepted implementation. Dependency advancement uses the declared acceptance boundary; successful process exit alone is insufficient.

Play, project enable, dependency acceptance, capacity release and restart wake reconciliation. Missed events fall back to polling. For one unchanged diagnostic fingerprint, permit one automatic safe repair; repeated persistence or recurrence after that repair escalates once and stays visible. Existing retry/spend limits always dominate. Repaired scheduling may admit existing authorized work; diagnosis itself never dispatches.

## UI and operator control

Display authorization, actual activity, and one leading reason, with details on demand. Queued/reserved is never called Running. Show capacity owners, next check time, unavailable/stale runtime, and disabled-action reasons. Render the same diagnosis and typed actions as the CLI; await authoritative readback after successful or refused mutations even without stream events.

Background work On resumes all still-valid prior authorizations in that project. The setting shows that scope and affected wave names/count before use; it does not arm any additional wave. Persist actor/source, before/after state and timestamp for switch changes; historical changes without evidence remain unattributed. Report CLI and running-daemon build/capability mismatch where observed, without restarting or installing as a diagnostic side effect.

## Delivery boundaries and prior work

TSK-T-0029/0030 in W-0033 own the existing arm-lifecycle and standalone backlog admission corrections. TSK-T-0031/0032/0033 in W-0034 own recovery lineage, uncertainty eligibility and adoption fencing; TSK-T-0034/0035 own recovery readback and setting validation. New tasks consume these changes through explicit hard dependencies; they do not rewrite, move or close those existing tickets. W-0032 owns cancellation/wrapper/submission safety regressions.

Use configured work/review levels, existing runtime/vault stores, ownership fences and test harnesses. Preserve dirty source, retained attempts and evidence. Proposed regression names must be implemented and must actually execute; a zero-match command is not proof. Component checks and process-boundary fake-runner checks are distinct from installed-app or live-provider qualification. The new waves do not authorize installing, restarting a resident daemon, or spending on live providers.

Architect and origin: this user/Codex conversation, unbound provenance. No verified task/execution messaging address is available. Return conflicting requirements or changes to these locked decisions to the operator with task/acceptance IDs, evidence and a recommended resolution.

## Task index

Created 13 detailed tasks in four inert waves. Execution order is W-0035 -> W-0036 -> W-0037 -> W-0038, with existing W-0033/W-0034 prerequisite fixes at the explicit task edges below. W-0032 safety prerequisites were accepted at authoring. Each new wave has concurrency 1.

### W-0035: authoritative diagnosis and author preflight

CLI authoring, Start and daemon decisions share stage-specific facts; doctor explains each blocked task or wave with an exact permitted next action.

| Task | Outcome | Hard prerequisites |
| --- | --- | --- |
| [TSK-T-0036](../work/tasks/TSK-T-0036.md) | Define actionable diagnostic and recovery facts on existing readiness contracts | None |
| [TSK-T-0037](../work/tasks/TSK-T-0037.md) | Unify wave, task and daemon eligibility without self-blocking reservations | TSK-T-0036, TSK-T-0030 |
| [TSK-T-0038](../work/tasks/TSK-T-0038.md) | Expose read-only task and wave doctor with precise recovery guidance | TSK-T-0037 |
| [TSK-T-0039](../work/tasks/TSK-T-0039.md) | Make author preflight distinguish valid contracts from runtime waiting | TSK-T-0038 |

### W-0036: accountable reservations and safe repair

Every reserved slot has a valid owner; safe repair is bounded, race-safe and verified, while uncertainty and human decisions retain explicit boundaries.

| Task | Outcome | Hard prerequisites |
| --- | --- | --- |
| [TSK-T-0040](../work/tasks/TSK-T-0040.md) | Reconcile reservation ownership and interrupted admission durably | TSK-T-0039 |
| [TSK-T-0041](../work/tasks/TSK-T-0041.md) | Implement scoped repair planning and verified safe application | TSK-T-0040 |
| [TSK-T-0042](../work/tasks/TSK-T-0042.md) | Map proof, review and uncertain outcomes to guarded recovery actions | TSK-T-0041, TSK-T-0032, TSK-T-0033 |

### W-0037: automatic progress and honest controls

Relevant changes promptly wake bounded reconciliation; CLI and UI show the same cause, safe action, authorization scope and audited project switch history.

| Task | Outcome | Hard prerequisites |
| --- | --- | --- |
| [TSK-T-0043](../work/tasks/TSK-T-0043.md) | Wake eligible work promptly and bound automatic safe repair | TSK-T-0042 |
| [TSK-T-0044](../work/tasks/TSK-T-0044.md) | Audit Background work changes and expose exact resume scope | TSK-T-0043, TSK-T-0035 |
| [TSK-T-0045](../work/tasks/TSK-T-0045.md) | Render shared diagnosis and recovery actions in wave and task flows | TSK-T-0044, TSK-T-0034 |
| [TSK-T-0046](../work/tasks/TSK-T-0046.md) | Ship author and operator SOP for diagnosis and bounded self-recovery | TSK-T-0045 |

### W-0038: lifecycle and restart qualification

Retained evidence proves Play reaches a worker and the next accepted dependency, while fault, race, restart and self-repair paths remain bounded and explainable.

| Task | Outcome | Hard prerequisites |
| --- | --- | --- |
| [TSK-T-0047](../work/tasks/TSK-T-0047.md) | Exercise complete lifecycle, authority and crash interleavings | TSK-T-0046, TSK-T-0023, TSK-T-0024, TSK-T-0025 |
| [TSK-T-0048](../work/tasks/TSK-T-0048.md) | Qualify CLI and Serve recovery across disposable process boundaries | TSK-T-0047 |

### Authoring verification

On 2026-09-22, all four `tusker wave review <ID> --check --json` commands exited zero with no blockers and Start enabled; authorization remained inert. All 13 generated packets were inspected, and all 26 execute/review route resolutions succeeded without blockers. The initial task validation reported zero errors and warnings; specification graph validation passed. These are authoring/readiness checks, not implementation proof. Every acceptance verification is pending; no implementation tests, worker dispatch, installation or daemon startup was performed for this handoff. Recheck before execution if prerequisite contracts or configuration change.
