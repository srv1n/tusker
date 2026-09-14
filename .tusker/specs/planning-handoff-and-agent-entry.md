---
subject: planning-handoff-and-agent-entry
title: "Planning capture, task handoff and progressive agent guidance"
keywords: [spec, compaction, skill, progressive disclosure, handoff, wave outcome]
part_of: spec-to-proof
status: canonical
created: 2026-09-07
read_when: "Designing spec capture, spec-to-task handoff or the shipped Tusker agent skills."
skip_when: "Executing a task or looking up current CLI syntax; use the installed operating guide."
sources: [spec-to-proof.md, work-knowledge-and-retention.md, decisions-2026-09-07-work-area-redesign-grill.md]
decisions_locked: false
capsule:
  what: "One Tusker entry skill with stage-specific documentation and work procedures."
  use_when: "Finishing the planning-to-execution product contract."
  skip_when: "Implementing another scheduler or formal spec-change approval system."
---

# Planning capture, task handoff and progressive agent guidance

## Why

The user wants a design partner to grill, sharpen terminology and preserve a faithful specification, then emit executable work without becoming its coordinator. Long conversations can lose detail at compaction. The response is continuous durable capture and a checkable handoff, not repeatedly loading the whole transcript or requiring another mandatory scratch journal.

This is a focused extension of [[spec-to-proof]], not a replacement corpus. It records intended behavior; current CLI/skills do not automatically satisfy it. No worker dispatch or installed-skill edits are performed by this specification.

## Current direction

The September 12 completion contract below is the current acceptance boundary
for ticket authoring and Play. Correcting tickets after the operator notices
missing metadata is not evidence that the default authoring workflow works.

**Settled choices.** External design methods keep the user and architect in the product conversation; Tusker owns durable documentation, bounded task contracts, configured work/review routing, proof and lifecycle state. Imported work is inert until authorized. Once authorized, Tusker—not the architect—owns mechanical scheduling, monitoring and message delivery. The [[agent-coordination]] contract adds targeted architect/peer questions and configured continuation across waves, while the current rollout may remain paused until that behavior is qualified.

**Active proposals.** The `expected_outcome` wave field below is an authored-field proposal; reuse an existing purpose field if it already provides the same behavior. The operating-skill implementation and validation scenarios remain work to deliver, not claims about the installed runtime. The held [skills and documentation contract](skills-and-documentation.md) owns the September 11 auxiliary edits.

**Open questions.** Qualification must establish which installed transports can resume or wake an existing conversation and when configured cross-wave continuation is safe to enable. Unsupported routes must remain explicit. Exact schema and UI names are implementation choices unless a linked decision says otherwise.

**Deferred.** Formal active-spec locking, automatic change-impact scheduling, a Tusker-owned interview method and model-driven status relaying are out of scope. Current named models and effort are resolved from configured work/review levels; historical fixed-model examples are not runtime proof.

## Confirmed user decisions

- External design skills own the conversation and specification development. Tusker owns its documentation format, discovery and the conversion of supplied intent into tracked work.
- Emit complete tasks with acceptance and a real dependency DAG when the user asks. Allow interfaces to unblock independent backend/frontend implementation.
- Keep spec changes during execution lightweight: update the spec and explicitly tell the worker what changed. Formal revision locking/replanning approval machinery is deferred. Do not silently claim a worker received an update.
- A wave needs a short intended outcome visible before execution: what the operator will be able to do after it completes.
- Completion leads with a concise result, verification, limitations and how to try it.
- One Tusker entry skill routes to focused operating references. Grilling, wayfinding and domain-modeling skills are external and are not bundled or reimplemented by Tusker.

## September 12 completion contract

The operator's first product test exposed a disconnected experience: the
architect supplied named-model recommendations, and classification/wave setup
needed follow-up prompting. The resulting repaired records do not prove the
original handoff was complete. The product must carry the same contract through
the skill, CLI, import, task/wave UI, admission and actual execution.

This section records requested behavior and the implementation sequence. It
does not certify the current application or enable execution. Existing
[[model-level-configuration]], [[agent-coordination]] and proof authorities
remain in force. It adds no replacement scheduler, registry or proof store.

### Classification and execution choice

The architect classifies every newly generated coding task as Tier 1 / Light,
Tier 2 / Standard or Tier 3 / Demanding. Classification considers uncertainty,
scope and failure risk: Light is a bounded change against settled interfaces;
Standard is ordinary cross-file implementation/integration; Demanding covers
unresolved technical complexity, shared-state races, security or recovery
design. Running a prescribed qualification procedure can remain Standard if
design defects return to their named Demanding owner rather than expanding
the driver's assignment. Record task-specific reasons when they affect scope.

The normal authored field is `work_level`. Review follows the work level unless
the architect deliberately sets `review_level` for the independent review risk.
Do not ask the author to also choose legacy `complexity` or a named model.
Profiles configured for each tier own harness, model, effort and access policy.
Preserve explicit operator profile overrides and historical routing, expose
their precedence, and never invent or change a profile to make a handoff pass.

Human work is a separate responsibility, not a fourth model tier. Use the
existing named human action/gate contract for a decision, credential, external
action or human acceptance. Mixed work has agent tasks plus the human gates
that actually block them. Show the human owner and required action; a human
action never queues an LLM merely because it appears in Work. Do not encode
human responsibility in the implementation/integrator `work_kind` field or in
a transient `next_owner` value alone.

### Architect identity and self implementation

Every generated task must retain its reported authoring conversation provenance:
native conversation ID and host when supplied by the host environment, plus
the stable registered architect/origin address when available. Capture this
once at authoring and project it into each task's effective contacts. Runtime
endpoint bindings own provider session IDs; contacts own design/origin roles.
Never manufacture a registered execution ID from a native UUID. An unavailable
attachment remains explicitly unbound, with its real provenance and fallback
visible. A coordinated run cannot claim an operational architect route from
prose or a syntactically valid address alone.

Author and implementer are distinct roles that may use the same conversation.
An explicit "work in this conversation" claim records that actual execution
and its task ownership without replacing author/origin provenance. It does
not launch a nested worker, change the tier, pretend the current model matches
a configured profile, or remove independent review. Play uses the configured
harness profile; self implementation is an explicit claim path with truthful
actual identity. Parent/root lineage, author identity and dependency edges
remain distinct relationships.

### Waves and Play

Implementation default selected for the user's open wave question: standalone
tasks remain supported. Do not require a synthetic one-member wave to run one
bounded task. Standalone and wave starts use the same effective-route and
admission checks. A deliberate one-member wave remains valid when useful.

For a multi-task delivery, authoring includes creation of its wave, intended
outcome, membership and dependency order. The operator should not need a second
prompt to make the prepared batch visible and executable in Work. Import stays
inert; authoring a wave does not authorize starting it.

Before Play, show the requested tier and resolved worker and reviewer profile,
model, effort and harness, with configuration source and any explicit override.
If the mapping is empty, disabled, ambiguous or unsupported, show the exact
blocker and the relevant configuration control. A general enabled-agent check
must not mask a missing member-task route. A profile's tier eligibility alone
is not a selected primary. Human actions show their owner/action instead of
an LLM Play button. Revalidate current configuration at start and retain the
actual admitted execution identity; settings changes do not relabel past runs.

### Small authoring surface and metadata retention

Reuse the wave authoring input and canonical task records. The planner supplies
outcome, relevant context/decisions, work level, scope/dependencies, acceptance
and real verification/evidence requirements. Shared governing references,
non-goals and architect/origin provenance are authored once and projected into
self-contained worker/reviewer packets. Exact task-specific sections must
survive import as governing references, not only as incidental prose.

| Treatment | Fields or information |
|---|---|
| Author | Outcome, implementation direction, work level, scoped paths, meaningful dependencies, acceptance/checks and required evidence; review override only when needed. |
| Inherit/project | Plan context, architect/origin, shared non-goals, domains and governing references; retain explicit task overrides. |
| Generate/retain | IDs, revisions, fingerprints, lifecycle/proof state, timestamps, actor receipts and next-action projections used by discovery, filtering and UI. |
| Omit when absent | Empty optional profile, concurrency, knowledge and contact values after reader/default compatibility checks. |
| Retain for compatibility | Historical `complexity`, legacy profile assignments, existing epic links and unknown compatible metadata until all consumers and effective behavior are accounted for. |

Trim redundant authoring and empty generated output first. Removing a useful
storage field is not a prerequisite for a simple experience. Each deletion
must identify its readers and show equivalent default behavior; never erase
proof requirements, explicit false/zero policy choices or historical identity
merely because they look verbose. Do not rewrite historical tasks en masse.

### Enforced handoff and first product test

| ID | Required outcome |
|---|---|
| TH1 | New batch authoring explicitly classifies agent versus human responsibility and agent work level without a reminder; import and packets preserve that choice. |
| TH2 | Task/wave preview, admission and dispatch agree on effective worker/reviewer routes and report missing configuration before Play. |
| TH3 | Every task exposes real architect/origin provenance and binding status; self implementation records its executor separately and preserves independent review. |
| TH4 | Batch creation includes a visible wave automatically; a standalone task follows the same start rules without a mandatory wrapper wave. |
| TH5 | The author-facing contract omits machine-maintained fields and redundant selections, with legacy reads and meaningful policy preserved. |
| TH6 | Skill, capability/help output, CLI and UI use the same supported authoring and execution contract. |
| TH7 | Governing sections, literal verification commands and required evidence survive import and every projection; prerequisite checks cannot alone certify browser/live outcomes. |
| TH8 | An installed-product trial proves authoring, exact profile selection, human waiting, execution, failed-check correction, independent review and truthful completion. |

Allow held drafts with explicit missing setup. Structural import success must
not be described as execution readiness. A fresh-reader packet review must
verify the complete task-specific behavior and proof; checks enforce structural
facts and real evidence, not prose length or a model-written readiness claim.

Sequence the missing integration work as follows:

1. **Admission and projection:** reuse the route resolver for both lanes of every
   wave member and standalone task; preserve literal check parsing. This closes
   the misleading green runner check before any first-test attempt.
2. **Authoring and retention:** enforce explicit generated-task classification,
   preserve human actions and exact references, simplify new output, and ship
   canonical skill/help guidance through the managed installation path.
3. **Identity and self claims:** connect trusted authoring provenance, stable
   contact binding and explicit current-conversation execution. Reuse the
   ACO baseline/transport owners for actual supported attachment; do not replay
   their message, wake or resume implementation.
4. **One visible start contract:** render the same worker/reviewer, contact,
   blocker and human-action facts on task and wave surfaces. Preserve standalone
   execution; batch intake creates the wave. Reuse existing components/APIs.
5. **First product trial:** a disposable project authored from settled intent
   without reminder prompts contains three classified agent tasks and a real
   human-owned action. Configure chosen profiles explicitly, compare preview to
   actual execution/review, exercise a failed check and a safe correction,
   inspect contact/self-claim facts and return an accepted result. Add a
   standalone task and a wave case. Record actual binary/build, profile revision,
   task/attempt identities and UI evidence. Deterministic fixtures precede the
   bounded installed-harness trial; absent live evidence stays NOT RUN.

The persistent architect question/yield/resume, stalled-wave supervision,
bounded corrections and next-wave continuation trial remains owned by
ACO-T-0001 through ACO-T-0010 in W-0017. Reuse that work after the first-test
prerequisites above; do not create a second coordination implementation wave.
Existing model/profile work and FLW skills/documentation work retain their
scope. Reconcile overlapping owned files before implementation, particularly
the currently dirty runner/daemon/ACP files and generated UI assets.

Host-reported authoring provenance for this completion contract: native Codex conversation
`01a08f6f-7bc8-7551-a17b-4e506b62bf87`, host `local`, obtained from the current
host context. This is not a claim of a registered Tusker execution endpoint.

## External planning guidance — outside the Tusker skill

The following capture recommendations may help external design tools. They are not a Tusker interview workflow, mandatory scratch protocol or a second shipped entry skill. Tusker supplies format/validation/discovery helpers that those tools can use.


After each resolved discussion round, update the affected canonical spec sections in place. Record actual decisions and their rationale in the decision log. Label proposals, assumptions, open questions and deferred scope; do not upgrade silence or an agent suggestion to user approval. Preserve examples and counterexamples that change the design, not every conversational phrase.

Keep a small continuation section in the existing decision map/spec: current objective, latest settled decisions, open questions and next decision. Include links to the exact spec/decision sections and existing assigned work. This is planning-session continuity, not another mandatory execution scratchpad. Do not make a new journal file after every turn.

On resumption, read that continuation and the relevant spec sections. Do not reconstruct missing answers from confidence or memory. Retrieve the source discussion only when a specific ambiguity requires it; ask the user only when the answer cannot be recovered. Compaction should reduce conversation history, not remove the authoritative design.

At natural milestones, show a brief change summary: decisions captured, remaining questions and anything superseded. The human need not reread the whole spec after each answer. Before task generation, give a short scope/assumption summary that can reveal omissions.

No mechanism guarantees semantic fidelity. CLI lint can detect missing fields and broken references, not whether the agent understood the user. Fidelity comes from incremental readback, preserved rationale and requirement-to-task coverage checks. A separate semantic reviewer is optional when requested, not a compulsory frontier-model wakeup every round.

## Spec-to-task conversion

Give implementable requirements stable local identifiers within the governing spec or existing requirement structure. Do not tag every sentence. Task acceptance references those requirements. Before emitting a plan, classify each requirement as covered by named tasks or explicitly deferred; surface uncovered requirements and tasks with no stated purpose. Reuse existing requirement_refs/spec_refs and the wave review projection rather than building another tracker.

A task includes outcome, bounded context, non-goals, relevant spec/decision anchors, concise implementation route, likely files to inspect, owned paths, dependencies, work level, acceptance and review/evidence expectations. Implementation hints are as detailed as the conversation supports; label unverified file suggestions. Do not duplicate the entire spec in each task or compress away essential constraints to meet an arbitrary token cap.

Choose dependency edges from actual prerequisites. Schema/interface agreement may unblock migrations, API implementation and frontend work; code layers do not impose a universal sequence. A frontend may start against an agreed contract before the backend exists. Two tasks touching the same file/resource need explicit coordination or serialization even if their features are independent. Reject cycles, missing dependencies and undocumented shared ownership through existing validation.

Generating/importing work remains inert by itself. The planning agent delivers the contract and task/wave links, then returns to design. The September 10 [[agent-coordination]] requirement adds durable architect and peer contacts: Tusker routes clarifications and wave results, waking the relevant agent only for an actionable turn. Configured continuation can validate and start subsequent waves within the agreed objective. The temporary unattended-execution pause is a rollout setting, not a design restriction. Tusker owns monitoring, message delivery and lifecycle mutation; the planning agent does not spend turns polling workers or relaying their status. This is intended behavior until the coordination implementation is qualified.

## Wave intended outcome

Proposed authored field: `expected_outcome`, one or two plain-language sentences. Exact schema naming should reuse an existing authored-purpose field if it already serves this job. Keep it separate from generated runtime counts and the observed completion summary.

Good: “You can sign in with Google and return to the page you started from.”
Good: “You can send text to the OpenAI-compatible embeddings endpoint and receive a vector.”
Poor: “Implement the API, migrations and frontend across eight tickets.”

Show this near the wave title before start and in its detail view. CLI authoring/import supplies it and CLI reads return it. It describes a promise backed by task acceptance, not a success claim. When complete, show the actual result and limitations beside or linked to that original promise. Partial delivery must remain visible.

## Confirmed package: one Tusker skill

Tusker starts where an agent needs to create or use Tusker-formatted documentation, turn supplied intent into tasks, arrange tasks into waves, configure the system or operate tracked work. The agent may arrive from any external planning/interview tool. Tusker does not require a specific grilling method or teach domain modeling. Do not ship a Tusker-specific design/interview entry skill for this scope.

The entry SKILL.md is a small operating router: purpose, how to identify the current job, shared safety/authority rules and links to focused references. References are part of this one skill package, not independently triggered skills. The entire procedure library must be discoverable without being loaded on every invocation.

| Current job | Focused reference content | First useful result |
|---|---|---|
| Write or find documentation | Managed roots; front-matter fields/types/examples; folder introductions; read/skip guidance; canonical/current/superseded ownership; meaningful links; browse/read/check/scaffold helpers | Correctly located and validated document, or the exact relevant section |
| Create tasks from supplied intent | Required context, scope, implementation pointers, requirement/acceptance coverage, exact verification, artifacts, work/review levels and inert import | Complete bounded task contracts and explicit gaps |
| Organize waves | Expected outcome, task dependencies, safe parallelism, shared ownership, validation and manual start boundary | Valid DAG and an understandable wave promise |
| Execute assigned work | Full relevant packet; start/ownership/workspace; effective profile; progress; submit; failure/retry | One task executed through the supported protocol |
| Review and finish | Acceptance, material/evidence binding, independent review, closeout and compact completion receipt | Truthful reviewed result and consistent task state |
| Configure Tusker | Global/project model levels, harness/transport discovery, overrides, explicit fallbacks and project registration | Valid effective configuration with provenance |
| Operate or troubleshoot runtime | Daemon purpose and ownership, status/capacity, event freshness, leases/workspaces, cancellation and exact recovery commands | Bounded diagnosis and supported next action |

The daemon reference must explain the difference between configuring/preparing work, authorizing a start, running the resident service and being a dispatched worker. It must not tell an interactive agent to start nested workers merely because a spec became tasks. The actual supported protocol is authoritative; contradictory old no-claim/attempt/finish instructions must be reconciled with it.

Load the reference for the current job and only the relevant subsection of another when a concrete prerequisite requires it. Do not require every reference, full command help or the whole documentation corpus at startup. CLI outputs should expose the next supported action and a targeted reference when a command is blocked. Do not guess a fix from an unrelated guide.

A worker still receives its complete task contract. Progressive disclosure removes unrelated context, never acceptance requirements or authority boundaries. Skill procedures reuse CLI capability/help output as command truth and explain how the operations fit together. Profiles/models come from configuration, not hard-coded skill defaults.

## Skill density and maintenance

Prefer a few hundred words in the entry skill as an engineering target, not a completeness limit. Count actual startup reads, total workflow calls and repeated context before declaring savings. A small entry followed by ten mandatory reads is not efficient.

Use one authoritative source for each procedure. Shared CLI/metadata conventions live in focused references; generated help and examples are checked against the executable. Retain read_when/skip_when and accurate paths. Front-matter assistance should fill structure and validate types/links without inventing subject meaning or rationale.

Current project guidance contains concrete conflicts to reconcile in the Tusker operating package: old hard-coded model/dispatch assumptions versus configured work levels, and no-run-claim guidance versus the current interactive work-session protocol. The separate external grilling/spec skills are outside this change. Do not preserve contradictory operating instructions merely to save words. Document the real supported daemon/interactive distinction in one place.

A later skill implementation must reconcile source versus generated/installed copies and test the shipped entry route. Do not hand-edit several copies independently. No skill should trigger orchestration just because it produced tasks.

## Validation scenarios for a future implementation packet

1. Pause a design after a resolved round; a fresh reader recovers decisions, non-goals and open questions from the saved artifacts without the whole conversation.
2. Change one earlier decision; the spec states the current choice and the log preserves why it changed, without competing authoritative versions.
3. Convert a spec into tasks; every in-scope requirement maps to acceptance, real dependencies form an acyclic graph and omitted/deferred work is explicit.
4. A fresh worker reads its complete bounded contract and reaches the first valid action without reading every guide.
5. A reviewer receives relevant changes/evidence and cannot claim a pass from stale or mismatched material.
6. A documentation query reaches the correct note through bounded folder/front-matter discovery.
7. A completed wave reports observed results without substituting its expected-outcome promise for proof.

## Next decisions

One Tusker entry skill is confirmed. The next product decision is when qualified, configured architect continuation may be enabled for a real objective; the current manual-start rollout remains the safe setting until that evidence exists. The held [skills and documentation work](skills-and-documentation.md) defines the September 11 follow-ups, including exact reference structure, shared entry rules, stage acceptance examples, requirement-coverage handoff and authored wave outcomes. Reconcile the existing trust/efficiency backlog before creating duplicate tickets. External planning capture cadence is not a required Tusker workflow. Formal active-spec locking, automatic change-impact scheduling and orchestration by the design agent remain deferred.

<!-- tusker:delivery-import:9208edbc81eea5d1:begin -->

## Work streams

- `[[FLW-T-0037]]` implements delivery source `admission`.
- `[[FLW-T-0040]]` implements delivery source `experience`.
- `[[FLW-T-0041]]` implements delivery source `first-trial`.
- `[[FLW-T-0039]]` implements delivery source `identity`.
- `[[FLW-T-0038]]` implements delivery source `intake`.

- `[[W-0023]]` is the imported delivery wave.

<!-- tusker:delivery-import:9208edbc81eea5d1:end -->
