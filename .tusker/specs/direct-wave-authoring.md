---
subject: direct-wave-authoring
title: "Direct task and wave authoring"
keywords: [ad hoc tasks, delivery plan removal, wave authoring, task contract, architect contact, migration]
part_of: planning-handoff-and-agent-entry
describes: [cmd/tusker, internal/v7schema, internal/serve/ui, skills/tusker]
status: canonical
created: 2026-09-12
read_when: "Implementing direct task/wave authoring, removing delivery plans, or routing external architect contacts."
skip_when: "Looking up installed commands; this is the future implementation contract."
sources: [planning-handoff-and-agent-entry.md, agent-coordination.md]
updates: [docs/system/delivery-and-waves.md, docs/system/cli.md]
decisions_locked: false
---

# Direct task and wave authoring

## Orchestrator entry

Handoff identifier: **W-0025**. This is the consolidated September 13 handoff
for FLW-T-0043 through FLW-T-0050. Read this spec and the current task packets;
earlier chat summaries are superseded by these records. Implementation was
unstarted when this handoff was authored; this is not current execution status.
The planning-only boundary describes the authoring
session, not a prohibition on a subsequent explicit assignment to implement.

The assigned orchestrator owns implementation, shared-file coordination,
independent review and integrated qualification through the final deletion
audit. Preserve concurrent work. Use the supported interactive ownership
procedure for interactive implementation; do not launch a nested daemon or
worker to bypass existing runtime setup. Actual human gates remain authoritative.
Do not claim that disarmed wave Play is ready because the tickets are complete.

Execute these dependency stages, parallelizing only after checking shared-file
ownership: 0043 and 0047; then 0044; then 0045; then 0050 and 0048; then 0046;
then 0049 after all its prerequisites. The stored concurrency of one is an
automated dispatch cap, not another dependency edge. Task 0047 shares runtime
areas with later authority/0050 work and must coordinate before overlapping edits.

Implementation tiers: Standard for 0043, 0046 and 0048; Demanding for 0044,
0045, 0047, 0049 and 0050. All independent reviews are Demanding. Do not
hard-code vendor models in tickets or change real profile mappings implicitly.

## Status and authority

This document captures the user's September 12 decisions in Codex conversation
01a093ed-b11f-7432-8c8f-4d88d35740b8, host local. This is native origin
provenance, not a verified Tusker messaging endpoint. Return material product
questions to that conversation through an authorized host route or to the
operator. The authoring session creates documentation and inert tickets only;
it does not implement, install, dispatch, enable automation, or select profiles.

This specification supersedes the mandatory delivery-plan, spec-reference,
factory-contract input and heavyweight metadata requirements in the earlier
planning-handoff completion contract for the implementation described here.
Existing installed behavior continues until migration. Existing execution,
review, human authority and proof protections remain required unless explicitly
changed below. This is not a claim that the future commands already exist.

## Product outcome

The user discusses a change with a frontier architect. The architect inspects
the relevant code, settles technical decisions and uses the CLI to write a
complete task or a wave of tasks. The CLI supplies storage metadata. Workers
receive enough concrete guidance to implement the agreed behavior without
rediscovering the design. The user reviews and starts a wave directly.

There are two executable work objects: tasks and waves. An epic is optional
organization. A specification is optional supporting documentation. A delivery
plan is no longer a separately maintained object, file format, runtime input,
review requirement or UI destination.

## Requirements

| ID | Required outcome |
|---|---|
| R1 | CLI creates and updates complete standalone tasks and atomic task batches directly into durable task/wave records. |
| R2 | Normal authoring requires explicit work level and substantive instructions, but no spec, epic, priority, size, risk, factory metadata or context hash. |
| R3 | Wave review, admission and execution use durable wave/task material without a delivery plan. |
| R4 | Existing dependencies, human gates, contract-bound proof, ownership and history survive migration. |
| R5 | Rich technical guidance survives authoring, storage, packet generation and UI projection. |
| R6 | External architect identity includes its harness and exact conversation; communication capability is verified separately. |
| R7 | Obsolete delivery-plan code, commands, APIs, screens, fixtures and executable artifacts are deleted after migration. |
| R8 | Public CLI and UI scenarios prove the resulting flow and report live-provider limitations honestly. |
| R9 | One task or wave Start performs scoped authorization and necessary transitions without a manual readiness step. |
| R10 | Authorized waves continue automatically with reliable pause/resume, visible waiting state and restart recovery. |

## Object and field contract

Task front matter contains machine-readable classification, identity and optional
relationships. Its Markdown body contains the implementation contract. CLI/API
input is a request to mutate those objects, not another durable work object.

| Input/fact | Decision |
|---|---|
| title, work_level | Required for executable agent work. Authoring agent chooses light, standard or demanding without asking the user for routine classification. |
| body | Required substantive instructions including observable completion conditions and verification. No mandatory heading count or word quota. |
| review_level, review_reason | Review inherits work. Different level requires a task-specific reason. Configured lanes choose model, harness and effort. |
| spec_refs | Optional for every work level, including Demanding. Supplied references/anchors must resolve. An absent spec is not a blocker. |
| epic | Optional. Task identity allocation must work without an epic; preserve existing IDs verbatim. Prefer the existing project allocator; settle collision-free new ID syntax before replacing allocation. |
| wave | Optional membership; standalone work must not acquire a synthetic singleton wave. |
| dependencies, owned_paths, generated_outputs | Include when meaningful; preserve graph and shared ownership semantics. |
| acceptance/check mappings | Preserve stable IDs and exact checks for complex work. The CLI may allocate IDs for simple authoring; the author need not repeat completion text in multiple representations. |
| artifact contract | Conditional: required when the outcome depends on a named report, capture or external evidence. Ordinary code work may use the diff and execution-recorded checks. |
| implementation_notes | Optional separate input/section. Technical guidance remains required wherever needed in the body. |
| priority, size, risk | Remove from normal authoring and new default output. Do not silently change historical routing, ordering or completion policies that consume them. |
| author/origin/contact | Auto-capture and inherit available identity; explicit overrides supported. Do not infer that importer equals original author. |
| schema, IDs, timestamps, revisions | CLI-owned; never required planner bookkeeping. |
| planning-context hash, factory-contract schema/version/hash | Remove author input requirements and plan-based Start blockers. Validator/build version may be internal diagnostic metadata. |

Work level is retained to control token expenditure. Light means bounded work
with settled interfaces; Standard means ordinary implementation/integration;
Demanding means substantial technical ambiguity, security, concurrency or
recovery decisions. Ticket length does not determine work level. Review level
remains independent when deliberately overridden.

Risk removal includes auditing workflow reviewer/auto-close rules. Replace
unsupported custom configuration explicitly at cutover; do not retain old-risk
runtime branches or build a policy translation framework. Never map missing
risk to a less restrictive auto-close policy. Preserve the current required
independent review and human authority controls.

## CLI authoring and atomicity

Extend existing task/wave mutation authorities. The final CLI must support
complete body input from a file/stdin, optional relationships, updates with
revision conflict detection, and an atomic multi-task wave operation. Exact
flag names are implementation choices published in help and capabilities before
dependent UI/skill changes. Do not make agents author serialized V7 records.

The batch operation accepts a transient structured mutation request: wave
outcome, task content, temporary intra-request task labels, dependencies and
human actions. It immediately writes task/wave/gate records and returns IDs,
paths and readiness. It does not persist a plan, discover plan files, or require
that input again for review, amendment, Start, or completion.

Validate the entire batch before publishing writes. Reuse existing locks,
rollback and allocation mechanisms. On failure no partial member graph becomes
visible or runnable. A retried identical request must not duplicate tasks;
reuse existing operation receipts/idempotency support, with a request key when
needed. Same key with changed content reports a conflict. Temporary labels
resolve to task IDs inside the transaction and are not permanent dependencies.

Creation defaults to inert work. Creating records never authorizes execution.
Updates identify the durable task/wave and do not require the original batch
request. Preserve raw command strings including pipes and Markdown escapes.

## Rich technical handoff

Reducing metadata must not reduce implementation detail. The frontier architect
must inspect the actual relevant code/schema/callers before writing complex
tickets. A spec link supplements task-specific guidance; it cannot replace it.

For a substantial task the body records:

- Current problem and intended behavior, including a concrete example.
- Existing authorities, verified paths, relevant functions/types and callers.
- Specific field/type/API changes and their semantics when settled; proposed
  names are labeled as proposals rather than invented existing source.
- Locked design choices, relevant rationale and worker discretion.
- Discussed edge cases, failure/recovery behavior and compatibility limits.
- Upstream outputs, downstream consumers, shared ownership and integration.
- Observable acceptance and checks that actually exercise those outcomes.
- Where unresolved material decisions return; routine choices stay local.

For example, a DAG handoff must preserve the settled edge direction, cycle
handling, missing dependency behavior, readiness conditions, failure propagation
and update semantics. It identifies the current scheduler/schema seam and
specific changes after inspection. "Implement a DAG" is insufficient.

Shared context can live once on a wave. Packet assembly includes applicable
shared context plus task-specific details, exact references, dependencies,
classification and contact facts. A standalone packet is self-contained.
Structural checks can reject missing content/mappings; they cannot certify
semantic completeness. Qualification includes a cold-reader comparison against
the spec and source, not keyword counts or a new mandatory human gate.

## Wave review, changes and execution

Make wave review/authorization the single execution authority.
CLI and UI review read the same wave/task projection. Preserve both configured
execute/review route checks, human actions, dependency readiness, workspace
ownership and late dispatch checks. Remove plan bytes/path/scope/hash and factory
contract equality from the start transaction.

Keep internal material fingerprints and state revisions where they prevent
concurrent overwrite or execution of different material than authorized. A task
contract fingerprint covers actual instructions/acceptance/checks and relevant
relationships; a lifecycle status change alone must not redefine its contract.
Evidence remains tied to the contract and material it verified.

A changed spec does not trigger a mandatory planning-context regeneration
ceremony. The architect updates affected task instructions and sends the change
through supported messaging. Record which revision an attempt consumed and
whether the message was queued, delivered, acknowledged or incorporated. Do not
claim delivery from a queued receipt. Material changes to authorized work still
invalidate stale authorization; use the existing controlled update/rework path.
Changes during active execution must not rewrite its historical snapshot or
silently attach old proof to a new contract. Unaffected work can continue.

## Start, pause and autonomous pickup

September 13 clarification: creation is inert, but backlog/held is never a
reason by itself to refuse an explicit Start. Separate authored work, computed
eligibility and execution authorization. Do not require users to mark ready,
arm, then press Play. Remove those duplicate public transitions and their
status-only UI guards. Internal state may represent these facts without
requiring manual synchronization or another lifecycle store.

| Action | Scope and result |
|---|---|
| Create task/wave | Writes complete planned work without dispatch. Autonomous mode alone never authorizes newly created work. |
| Start task | Authorizes that task only, validates real prerequisites, and atomically records the necessary state/claim or durable dispatch intent. It does not start or arm siblings, upstream tasks or the containing wave. |
| Start wave | Authorizes current wave material, schedules eligible members and continues through dependencies and independent review within that wave. No second Play or manual readiness transition. |
| Pause wave | Prevents new worker and reviewer dispatches after acknowledgement. Already admitted attempts may finish; UI states this explicitly. Does not interrupt processes or revoke unrelated explicit task runs. |
| Resume wave | Revalidates current material and resumes pickup. Retry is idempotent; changed material is explained, not silently authorized from a stale request. |
| Start task within paused wave | Explicit task-scoped action may proceed when real prerequisites permit; the rest of the wave remains paused. UI states that scope. |

Normal state labels are Planned, Running, Paused, Waiting and Completed.
Readiness is computed from actual prerequisites, not a second editable status.
Authorization remains visible where it matters: for example, Authorized —
waiting for runtime. Completed does not mean merely worker-finished; retain
independent review and explicit human acceptance requirements where applicable.

Start is one supported backend operation per requested scope, shared by CLI,
API and all UI surfaces. Mode is explicit: interactive claims check the current
host/workspace/ownership and applicable review requirements without requiring
a daemon or project automation. Background requests use the configured runtime.
Missing/disabled routes, incompatible setup, human gates, unresolved required
dependencies and ownership conflicts remain genuine blockers with exact reason,
affected task and repair action. A downstream dependency wait must not prevent
eligible wave roots from starting. A task-scoped Start does not bypass its own
unfinished dependencies.

If background runtime is temporarily offline, a valid authorized request may
remain durably waiting for runtime; UI must not claim it is running. Invalid
configuration must be reported separately with its remedy. Failed validation
must not leave tasks spuriously running. Use existing transaction and durable
dispatch intent mechanisms to recover a crash between authorization and queue
publication. Duplicate clicks/requests and competing workers must result in
one admitted attempt. Pause/Start races are resolved under the same authority:
after Pause acknowledgement no new wave-owned admission may pass until Resume.

An authorized wave is picked up by the existing scheduler automatically when
the runtime is available. Successful prerequisite/review completion releases
the next eligible frontier subject to concurrency, ownership and human gates.
Restart resumes the recorded scope without duplicating attempts. Authorization
does not extend to unapproved waves or newly added material. Creating a future
wave remains inert. An optional policy to auto-authorize all new waves is
outside this change; do not add it just to implement autonomous continuation.

The wave header shows current state and Start/Pause/Resume or actionable waiting
reason without opening a technical disclosure. Task drawer and full task page
offer Start for an eligible planned task with scope clearly stated. Render all
relevant blockers inline, with settings links or human-action controls; do not
hide them in tooltips or tell standalone tasks to start a wave. Keyboard and
disabled/hover contrast must remain usable. The actual WorkExperience wave
screen, legacy product wave screen and Inspector must consume one backend
projection; no screen-specific status gate or alternate authorization path.

## External architect contacts

Reuse execution registration, contact roles, attachment generations and durable
messages. No second conversation registry or scheduler. Store structured
harness/provider, native session/conversation ID and host/connection identity.
A display label such as codex:<id> or devin:<id> is acceptable; never recover
identity by ambiguously splitting a concatenated string. A profile is optional
execution configuration and does not identify a particular conversation.

An external architect may predate Tusker and have no task attempt. Supported
registration must attach that existing conversation after verifying its project
association and connection authority. Remove the assumption that the architect
must be the worker's own live admitted task attempt. Preserve cross-project
access controls and endpoint replacement generation checks.

Auto-capture available host identity once; if input was authored elsewhere,
retain its explicit verified origin and distinguish import context. Unsupported
or unknown host capture stays unknown/unbound, never falsely local/routable.
Tasks inherit wave contacts with explicit per-task override. Distinct architect
and result-origin roles may reference the same registered endpoint.

Capabilities are connector-derived: message while running, continue while idle,
retrieve response, and attachment validity. A provider name alone proves none
of these. For Codex, Claude Code and Devin, inspect the supported installed
integration and record the capability matrix. Implement supported operations
through existing adapters; do not fabricate support or start replacement chats.
Busy endpoints queue according to actual support. Unsupported operations return
a precise reason and operator handoff while preserving the destination.

Message/reply identity includes original question and recipient generation;
duplicate sends are idempotent, stale replacement targets refuse delivery,
responses correlate to the question, and uncertain delivery is not retried
blindly. Reuse W-0017 messaging/wake/correction mechanisms and coordinate its
owned files. Provider-live qualification needs actual authorized connections;
protocol stubs are separately labeled evidence.

## Migration and complete deletion

No backward-compatible product path is required. Delete old plan commands,
formats, aliases, parser branches and readiness-only start procedures rather
than supporting both. One-time conversion protects existing authored work and
evidence; it is not a reason to retain runtime compatibility. Do not build a
general old-policy translation framework. Report actual incompatible custom
configuration in the cutover inventory for replacement with the new supported
configuration; never silently weaken evidence or ownership controls. Do not
discard work, credentials, audit evidence or user-owned content under the label
of removing compatibility.

Migration begins with a read-only inventory of plan files, imported waves/tasks,
dependency source identities, strict proof lineage, custom workflow policies,
active attempts and non-generated authored content. Use exact targets and a
machine-readable disposition: materialize, convert, preserve as historical
evidence, or delete. No broad glob-based deletion of arbitrary .plan files.

Resolve existing scope/source-key edges to durable task IDs and verify unique
targets before removing source identity. Preserve dependency kind, task/wave
IDs, gate owners, contract content, actual attempts and evidence. Reject ambiguous
or missing mappings with a repair report; never guess. Unimported valid authored
work must be materialized as held work before its only source is deleted.

Migrate task-contract hashes and strict proof lineage by meaning, not by field
name. Do not replace proof semantics with a generic state revision. Preserve
immutable historical receipts as evidence; they are not executable legacy plan
support. No read/start fallback may require a plan after migration.

Use the existing migration mechanism with inspect/apply, transactional writes,
restart-safe progress and idempotence. Validate converted material before exact
obsolete files are removed. Do not mutate live attempts; report that migration
unit busy and allow unrelated units to proceed. Migrating records never arms
work, accepts proof or grants a human decision. An interrupted migration must
resume without duplication or data loss. Standard transactional recovery is
allowed; permanent *_BKP copies, archived parsers and dormant dispatch paths are
not. Git/history and existing recovery mechanisms preserve auditability.

After conversion delete plan-only CLI routes/help, parsers/types, file discovery,
Serve endpoints, UI plan picker/path input, plan-bound review/Start, obsolete
fixtures and executable plan artifacts. Move still-used helpers into their task,
wave or proof owner first. Historical transcripts/evidence may mention the old
format; explicitly allowlist those references with reasons. No runtime old-plan
dependency or temporary compatibility reader remains at completion.

## Source map and ownership

Verified audit entry points; executors must recheck the dirty checkout and every
caller before edits. Prefixes below indicate families, not blanket deletion.

| Area | Existing authorities / required action |
|---|---|
| Task creation/schema | cmd/tusker/commands_v7.go, internal/v7schema/schema.go, cmd/tusker/v7_traceability.go; remove unconditional metadata/spec/epic requirements and preserve lifecycle policy. |
| Batch writes | cmd/tusker/delivery_cmd.go and delivery_v2.go; reuse locks/rollback/rendering, remove maintained plan parser/authority. |
| Review/Start | delivery_review_cmd.go, delivery_start_cmd.go, v7_wave_authorization.go, wave_execute.go; converge on wave material. |
| Dependency/proof | delivery_cross_scope.go, v7_dependencies.go, delivery_verification_contract.go; migrate semantic identity and proof protections. |
| UI/API | serve_delivery.go, serve_delivery_snapshot_*.go, internal/serve/ui/src/features/delivery/DeliveryReview.tsx, lib/api.ts, lib/queries.ts, types/domain.ts; replace plan review with wave projection. |
| Origin | task_authoring_provenance.go, agent_contacts.go, execution_ledger.go, agent_coordination.go; support external endpoint ownership and capability dispatch. |
| Policy | cmd/tusker/workflow.go and workflow_validate.go; audit risk/priority/size consumers before removal. |
| Guidance | skills/tusker/references/HANDOFF.md and TRACK.md; retain rich technical context with simple commands. |

W-0023 provides current authoring/route/UI work; consume present implementation
rather than redo it based on held status. W-0017 owns persistent coordination.
Existing runner/ACP/UI/shared schema work is concurrently dirty; preserve it
and explicitly coordinate file ownership with the implementation coordinator.

## Acceptance and qualification

| Case | Observable proof |
|---|---|
| Q1 Ad hoc | Create Light, Standard and Demanding tasks without spec/epic/admin fields; content and checks survive fresh packet reads. Invalid supplied references fail. |
| Q2 Batch | Three dependent tasks plus human action are created atomically; inject write failure and retry, verify no duplicates/partial graph and no stored plan. |
| Q3 Rich handoff | A multi-edge-case spec yields task-specific source/type/decision guidance; a cold reader can implement without recovering chat history. |
| Q4 Routes | Task and wave preview match execute/review admission; missing/disabled routes and open human action block Start. |
| Q5 Changes | Edit reviewed/armed task material, observe stale authority; status-only updates preserve contract identity; old proof cannot certify new acceptance. |
| Q6 Migration | Convert held/progressed/unimported/strict-proof/cross-wave fixtures, inject interruption, rerun, and compare preserved semantics. Ambiguous/missing targets and active attempts report exact blockers. |
| Q7 Deletion | No plan file is needed for create/read/update/review/start/closeout. Scan removed commands/APIs/types; only documented historical references remain. |
| Q8 UI | Real routed wave review supports keyboard and narrow screens, human action and route errors; no plan inbox/path entry. |
| Q9 Contacts | Existing external endpoint with no task attempt registers; correct connector is selected; unsupported/busy/stale/duplicate/uncertain cases remain truthful. |
| Q10 Live | Separately record actual authorized provider send/reply and installed execution evidence; missing setup is NOT RUN, never fixture PASS. |
| Q11 Direct Start | Planned task starts without mark-ready or wave authorization; only the selected task runs. Genuine dependency/human/ownership blockers remain actionable. Interactive mode works without a daemon. |
| Q12 Autonomous | One wave Start triggers eligible roots and subsequent frontiers without another click; unrelated/new waves remain unapproved. Offline runtime/restart recover durable intent without duplicate attempts. |
| Q13 Pause and races | Pause acknowledgement prevents new wave-owned admissions while existing attempts can finish; Resume rechecks material. Duplicate Start, competing claims and Pause/Start races preserve one owner. |
| Q14 Controls | Actual WorkExperience wave header, full task and drawer agree on scope, state, waiting reasons and controls; keyboard, hover contrast and actionable blockers are exercised in routed browser tests. |

Implementation tickets propose named focused tests; those tests do not exist
merely because a ticket names them. Add substantive cases and fail qualification
if zero cases run. Use affected existing checks, then one integrated build/UI
qualification after changes settle. Do not fix unrelated baseline failures in
this wave. Documentation-only authoring does not run product tests/builds.

## Delivery sequence and bootstrap

1. D1: direct complete task/batch authoring and minimal schema/policy behavior.
2. D2: task/wave dependency and proof migration plus inventory/recovery.
3. D3: wave-owned review, amendments and Start; remove plan authority.
4. D8: automatic runtime pickup/pause/recovery, using D3's authority.
5. D4: wave review UI and API migration, using D3/D8's published projection.
6. D5: independent external architect registration/capability integration.
7. D6: shipped authoring guidance and rich packet qualification after D1/D3.
8. D7: integrated migration, deletion and qualification after D2/D4/D5/D6/D8.

D5 may proceed in parallel when contact ownership is clear. D1/D2/D3 share
authorities and should integrate in order. D4 and D6 can proceed independently
once their interfaces land; D4 also consumes D8. D7 owns the final deletion
audit and Q1-Q14 proof matrix. D8 reuses the existing scheduler; it is not a
new automation system.

The bootstrap tickets use today's supported tracker format and remain held in
a disarmed wave. This is intentional: they describe changes to a future format.
The one bootstrap import artifact is itself included in D7's deletion inventory
after its tasks are migrated. It is not an exception that preserves plan support.

No further product interview is required. Exact command names/new no-epic task
ID syntax are bounded implementation choices to publish in D1. Actual external
provider capability is an evidence question owned by D5, not permission to
claim all transports work. Migration ambiguity or a genuinely incompatible
historical policy returns to the operator with concrete affected records.

## Software-factory acceptance extension — September 14

Incorporates the [RZN consumer report](../../../rzn/backend/docs/plans/judgment-card-search/tusker-software-factory-handoff.md)
supplied by Sarav (reported commit `08cf124`). The report is dated rationale;
this section owns the accepted requirements. Historical examples are not
assertions that their defects remain present. No backward-compatible delivery
plan, new scheduler, proof store, registry or model panel is introduced.

### Representation and enforcement decisions

| Meaning | Canonical representation | Enforcement |
|---|---|---|
| Rules, concrete cases, expected/forbidden results | Task body; shared rules once in wave context or an optional spec | Author and reviewer judge completeness; no word/heading quota or required global rule registry |
| Acceptance identity and exact command | Existing acceptance/verification mapping | Reject dangling acceptance mappings; preserve exact commands across storage and worker/reviewer/API/UI projections |
| Case references | Local body IDs when useful | No arbitrary prose parser. If a supported structured mapping is introduced, validate its references atomically; until then semantic review owns case links |
| Required observation | Acceptance body names initiating action, receiver and observable result | Reviewer explicitly checks evidence sufficiency; a self-declared boundary label is not proof |
| Interface dependency | Existing task edge plus body naming supplied interface, consumer operation, contract check and integration owner | Provider proves the promised interface; a downstream integration task owns final behavior, avoiding a dependency cycle |
| Evidence | Existing verification receipts and evidence records | Bind to consumed task/source/artifact identity; required absent, failed, stale or zero-match evidence cannot satisfy acceptance |
| Review findings | Existing typed review/attempt mechanism | Stable finding identity, acceptance reference, evidence, consequence and closure condition; independent closure on exact current material, with changed material required for material repair and same material allowed only for proof-only repair |
| Repair policy | Existing external-loop limits and cumulative runtime budgets | Reuse the existing two-continuation default; do not create a competing policy. Verify its scope and persistence across restart before claiming compliance |

No new mandatory front-matter fields are approved here. Any machine-record
extension must first demonstrate a missing fact in the existing receipt or
review types and publish its input/output shape in the owning task. A free-form
command need not produce a test count: filtered test runners must establish
nonzero matches, while other commands establish their declared observation.
Unknown evidence is pending/blocked, not PASS or an automatic waiver.

Acceptance includes both proof validity and independent semantic review. A
test that invokes publication manually does not prove Save initiates execution.
A boundary acknowledgement does not prove completed restart. Invalid stored
JSON must not become valid-empty reuse evidence. Concurrent guarantees need
the actual read/check/write boundary and a concrete interleaving case. Apply
these cases where relevant, not as universal boilerplate on trivial work.

Keep immutable historical receipts. Changed material cannot inherit acceptance
automatically. Reuse evidence only where relevant inputs are demonstrably
unchanged; default to pending when that cannot be established. Do not add a
general fine-grained invalidation engine merely to avoid a small rerun.

Unclosed blocking findings prevent acceptance and dependent release; advisory
findings do not. Dependency release additionally requires the accepted output
to be available through the existing integration/workspace authority. A
worker's final message or an isolated unintegrated commit is insufficient.
Planner-scoped acceptance may finish independently, but a required integrated
outcome must retain its named owner and remain incomplete.

On exhausted repair limits or a contract conflict, persist one actionable
decision request with finding/acceptance IDs and recommendation. Unrelated
eligible work continues. Clarification within delegated authority differs
from a material amendment, which invalidates old authority under the existing
Start rules. Unsupported architect routing reports an operator action; it
must not fabricate a conversation or claim delivery.

Recovery reconciles admitted attempt and external operation identities before
retrying uncertain effects. Required continuation evidence is fail-closed;
diagnostic logging is not. Preflight checks the required environment/config,
binary identity and observation setup together without exposing secrets.
Reuse existing resource and budget authorities; project-specific Cargo or
shared-main rules are projected policy, not new Tusker-global defaults.

### Current evidence and implementation ownership

September 14 inspection found direct-authoring guidance in the shipped skill,
typed material-bound review submission in `cmd/tusker/workflow.go`, existing
verification rows in `cmd/tusker/v7_proof_cmd.go`, and a two-repair default in
`cmd/tusker/automation_external_loop.go`. These are reuse seams, not proof that
every requirement above is enforced. The installed CLI still returned old
preflight/arm instructions and W-0025 backlog/disarmed records. Source includes
substantial concurrent changes and delivery-plan deletion. Tracker state does
not establish absence of that implementation; source changes do not establish
installed qualification. No new product tests or live runs were performed for
this incorporation.

Keep the current bug-fix owner on the five prior review findings. Do not reopen
or rewrite its contracts from stale reports. The following are bounded
follow-on scopes, not allocated task IDs or claims of implementation. Before
CLI authoring, the coordinator reads the current W-0025 packets and owner
handoff, then removes already-proven cases from these scopes.

| Scope | Outcome and verified entry points | Work / review | Prerequisites |
|---|---|---|---|
| SF-A: packet fidelity qualification | Round-trip rich and ad hoc specimens through direct CLI, storage, worker/reviewer packets and API/UI. Own test/packet fixes adjacent to `direct_authoring_cmd.go`, `v7_traceability.go`, shipped HANDOFF/RUN. Preserve exact commands and meaningful context. | Standard / Demanding | 0043 and 0048 owner handoff; integrate UI assertions with 0046 |
| SF-B: proof and finding closure | Trace typed review submission and proof calculation through closeout and dependency eligibility. Close demonstrated gaps for stale/missing/zero-match evidence, durable finding closure and integrated prerequisite availability. Own proof/review changes; publish any missing receipt fields before consumers edit. | Demanding / Demanding | 0045 corrections; coordinate `work_session_cmd.go`, proof and runtime-store ownership |
| SF-C: bounded repair and continuation | Prove repair counters and cumulative limits survive restart, uncertainty reconciles without duplicate effects, exhaustion emits one decision request and unrelated work progresses. Reuse `automation_external_loop.go` and existing runtime store/daemon. | Demanding / Demanding | SF-B published review contract and 0050 handoff; 0047 for decision route capability |
| SF-D: installed real-wave qualification | Version parity plus a bounded real worker/reviewer repair-and-release pilot, retained failures and cost/time report. Extend existing journey/qualification machinery; no new harness platform. | Standard execution / Demanding review | SF-A/B/C; 0046 and 0049 installed cutover prerequisites |

SF-B owns shared proof/review schema decisions. SF-C consumes that interface;
it does not independently edit competing review semantics. SF-A can proceed
once its owner releases packet files. Existing W-0025 deletion work remains
0049's responsibility: these scopes supplement it rather than replace it.
No task mutations or dispatch are authorized merely by this scope table.

### Acceptance map and bounded pilot

| Report outcomes | Owner | Required evidence |
|---|---|---|
| SF-01, SF-02, SF-03 | SF-A | Disposable actual-CLI round trip; exact checks/content survive; invalid structured mappings, same-key changed request and injected batch failure expose no partial runnable graph |
| SF-04, SF-05, SF-06 | SF-B | Wrong-boundary evidence rejected; blocking finding remains open until verified closure; missing required consumer leaves integrated outcome incomplete |
| SF-07, SF-08, SF-09 | SF-C | Accepted integrated prerequisite releases dependent; repair exhaustion persists over restart; uncertain admission reconciles; independent task still progresses |
| SF-10 | SF-C with 0046 | Material amendment, pause acknowledgement, duplicate claims and resume agree across backend and UI |
| SF-11 | SF-A/SF-D | Shipped skill, installed help, API and CLI agree on supported Start and verification operations; errors retain exact remedy |
| SF-12 | SF-D | Actual configured worker rejects/repairs/reviews/integrates and releases dependent without human message relay |
| SF-13, SF-14 | SF-D | Separate functional, recovery, quality, performance and cost rows; actual route/effort, elapsed time, repair count and interventions; unknown cost is not zero |

Qualification order: first disposable CLI/projection checks, then deterministic
proof/review/runtime cases, then a real installed campaign. Use two dependent
tasks and one independent task with disjoint paths/resources. Include one
predeclared wrong-but-green defect; a stubbed finding proves routing only,
whereas an actual independent model finding evaluates review quality. Retain
a missed defect as a failed evaluation, not repeated hidden trials until PASS.

The pilot records one Start, submission, blocking review, repair, targeted
re-review, accepted integration and automatic dependent pickup. Exercise pause
and restart with retained progress. Test decision escalation separately via a
verified route or an explicitly unsupported result. An unsupported route does
not qualify automatic architect round-trip. No full RZN corpus or paid
extraction campaign is needed.

Before launch record the exact installed build, configured routes/efforts,
workspace, permitted mutations, existing resource owner, attempt/time/spending
limits and cleanup targets. Sarav supplies campaign authorization and spending
ceiling; this specification does not start a daemon, install a build or spend
provider budget. Stop at a configured bound and retain first failure, receipts
and first incomplete action. Use a compact report from existing records; no
analytics subsystem is required.

The immediate shipped-skill changes implement author/reviewer practice only.
They do not certify backend enforcement or a successful installed pilot.

## Historical W-0025 authoring record

September 14 incorporation: the software-factory requirements and follow-on
scope below extend this contract. Existing W-0025 IDs and in-flight ownership
remain intact; its historical status statements below are authoring-time facts.

Created through the current CLI as [W-0025](../work/waves/W-0025.md), open and
disarmed. The records are backlog/held. All review levels are Demanding because
this work changes execution/proof authority or its user-facing representation.
Current configured routes must be checked by the assigned executor before any
start; import success is structural proof only.

Authoring-time preflight reports empty Standard execute and Demanding
execute/review mappings, resident daemon/project health and automation setup,
isolated integration workspace, unattended runner policy and workflow/schema
compatibility blockers. No setup was changed. These are automated execution
prerequisites, not a request to start a daemon from an interactive session.
The operator may hand the documented work to an interactive implementer using
the supported ownership procedure; do not bypass explicit human gates or
assert that wave Play is ready.

| Step | Ticket | Work level | Depends on |
|---|---|---|---|
| D1 | [FLW-T-0043](../work/tasks/FLW-T-0043.md): direct CLI authoring | Standard | None |
| D2 | [FLW-T-0044](../work/tasks/FLW-T-0044.md): dependency/proof migration | Demanding | D1 |
| D3 | [FLW-T-0045](../work/tasks/FLW-T-0045.md): wave execution authority | Demanding | D2 |
| D4 | [FLW-T-0046](../work/tasks/FLW-T-0046.md): wave review and Start controls | Standard | D3, D8 |
| D5 | [FLW-T-0047](../work/tasks/FLW-T-0047.md): external architect routing | Demanding | None; coordinate W-0017 |
| D6 | [FLW-T-0048](../work/tasks/FLW-T-0048.md): complete worker guidance | Standard | D3 |
| D7 | [FLW-T-0049](../work/tasks/FLW-T-0049.md): deletion and qualification | Demanding | D4, D5, D6 |
| D8 | [FLW-T-0050](../work/tasks/FLW-T-0050.md): automatic pickup, pause and recovery | Demanding | D3 |

D8 is Demanding because live admission/pause races and crash recovery invariants
still require critical-path reasoning. Tier classification is based on remaining
worker judgment, not file count: settled implementation with routine caller
tracing is Standard, trivial local edits Light, unresolved critical-path
discovery Demanding. Strong review does not automatically require strong work
tier. The consolidated handoff explicitly assigns D1 Standard work with
Demanding review; the other classifications remain as listed above.

The bootstrap artifact is `direct-wave-authoring.bootstrap.plan.json` in this
directory. It exists solely because the currently installed batch CLI requires
it. D7 must account for its contents through D2 migration and delete it as part
of removing all executable delivery-plan inputs. Executors should begin from
the task packet and this specification, not reverse-engineer that artifact.

<!-- tusker:delivery-import:15b0db1dd236fd4c:begin -->

## Work streams

- `[[FLW-T-0043]]` implements delivery source `authoring`.
- `[[FLW-T-0045]]` implements delivery source `authority`.
- `[[FLW-T-0050]]` implements delivery source `autonomous`.
- `[[FLW-T-0047]]` implements delivery source `contacts`.
- `[[FLW-T-0046]]` implements delivery source `experience`.
- `[[FLW-T-0048]]` implements delivery source `guidance`.
- `[[FLW-T-0044]]` implements delivery source `migration`.
- `[[FLW-T-0049]]` implements delivery source `qualification`.

- `[[W-0025]]` is the imported delivery wave.

<!-- tusker:delivery-import:15b0db1dd236fd4c:end -->
