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

## Confirmed user decisions

- External design skills own the conversation and specification development. Tusker owns its documentation format, discovery and the conversion of supplied intent into tracked work.
- Emit complete tasks with acceptance and a real dependency DAG when the user asks. Allow interfaces to unblock independent backend/frontend implementation.
- Keep spec changes during execution lightweight: update the spec and explicitly tell the worker what changed. Formal revision locking/replanning approval machinery is deferred. Do not silently claim a worker received an update.
- A wave needs a short intended outcome visible before execution: what the operator will be able to do after it completes.
- Completion leads with a concise result, verification, limitations and how to try it.
- One Tusker entry skill routes to focused operating references. Grilling, wayfinding and domain-modeling skills are external and are not bundled or reimplemented by Tusker.

## External planning guidance — outside the Tusker skill

The following capture recommendations may help external design tools. They are not a Tusker interview workflow, mandatory scratch protocol or a second shipped entry skill. Tusker supplies format/validation/discovery helpers that those tools can use.


After each resolved discussion round, update the affected canonical spec sections in place. Record actual decisions and their rationale in the decision log. Label proposals, assumptions, open questions and deferred scope; do not upgrade silence or an agent suggestion to user approval. Preserve examples and counterexamples that change the design, not every conversational phrase.

Keep a small continuation section in the existing decision map/spec: current objective, latest settled decisions, open questions and next decision. Include links to the exact spec/decision sections and existing assigned work. This is planning-session continuity, not another mandatory execution scratchpad. Do not make a new journal file after every turn.

On resumption, read that continuation and the relevant spec sections. Do not reconstruct missing answers from confidence or memory. Retrieve the source discussion only when a specific ambiguity requires it; ask the user only when the answer cannot be recovered. Compaction should reduce conversation history, not remove the authoritative design.

At natural milestones, show a brief change summary: decisions captured, remaining questions and anything superseded. The human need not reread the whole spec after each answer. Before task generation, give a short scope/assumption summary that can reveal omissions.

No mechanism guarantees semantic fidelity. CLI lint can detect missing fields and broken references, not whether the agent understood the user. Fidelity comes from incremental readback, preserved rationale and requirement-to-task coverage checks. A separate semantic reviewer is optional when requested, not a compulsory frontier-model wakeup every round.

## Spec-to-task conversion

Give implementable requirements stable local identifiers within the governing spec or existing requirement structure. Do not tag every sentence. Task acceptance references those requirements. Before emitting a plan, classify each requirement as covered by named tasks or explicitly deferred; surface uncovered requirements and tasks with no stated purpose. Reuse existing requirement_refs/spec_refs and delivery doctor rather than building another tracker.

A task includes outcome, bounded context, non-goals, relevant spec/decision anchors, concise implementation route, likely files to inspect, owned paths, dependencies, work level, acceptance and review/evidence expectations. Implementation hints are as detailed as the conversation supports; label unverified file suggestions. Do not duplicate the entire spec in each task or compress away essential constraints to meet an arbitrary token cap.

Choose dependency edges from actual prerequisites. Schema/interface agreement may unblock migrations, API implementation and frontend work; code layers do not impose a universal sequence. A frontend may start against an agreed contract before the backend exists. Two tasks touching the same file/resource need explicit coordination or serialization even if their features are independent. Reject cycles, missing dependencies and undocumented shared ownership through existing validation.

Generating/importing work remains inert. The operator's explicit start authorization is separate. The planning agent delivers the contract and task/wave links, then returns to design; it does not automatically monitor workers, relay messages or close tasks.

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

One Tusker entry skill is confirmed. Next, specify its exact reference structure, shared entry rules and stage acceptance examples, then emit bounded work for skill/source consistency, requirement-coverage handoff and authored wave outcomes. External planning capture cadence is not a required Tusker workflow. Reconcile existing trust/efficiency backlog before creating duplicate tickets. Formal active-spec locking, automatic change-impact scheduling and orchestration by the design agent are deferred.
