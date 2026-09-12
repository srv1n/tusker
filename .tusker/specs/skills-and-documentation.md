---
subject: skills-and-documentation
title: "External skills and clear everyday documentation"
keywords: [skills, grilling, technical-writing, unslop, documentation, prose, auxiliary]
part_of: planning-handoff-and-agent-entry
describes: [skills, cmd/tusker/init_docs.go, cmd/tusker/v7_plain_top_layer_lint.go, docs/system]
status: canonical
created: 2026-09-11
read_when: "Implementing the held auxiliary skills and documentation tickets."
skip_when: "Testing live task execution, wave scheduling or agent messaging."
sources:
  - .tusker/specs/planning-handoff-and-agent-entry.md
  - docs/reports/pstack-adoption-review.md
decisions_locked: false
capsule:
  what: "External design and writing references, clearer docs, focused prose lint and a combined guidance check."
  use_when: "Assigning the auxiliary work alongside separate runtime testing."
  skip_when: "Changing runtime behavior or starting execution."
---

# External skills and clear everyday documentation

The user is testing task execution, waves and inter-agent communication separately. Prepare the auxiliary work as complete tickets with disjoint ownership and real dependencies; do not start, arm or dispatch it. This scope does not certify or change those runtime journeys.

The [pstack review](../../docs/reports/pstack-adoption-review.md) supplies the source comparison and observed documentation defects.

## External methods and Tusker ownership

Use the existing externally maintained `grilling` skill as the default available design method, while accepting another user-selected method or an already adequate specification. The currently installed source is `/Users/sarav/.agents/skills/grilling/SKILL.md`; this machine-specific observation must not become a shipped absolute path. Resolve the named skill from the host's available skill catalog and read its actual instructions. Tusker must not copy its interview questions, round structure, or decision-tree algorithm into a second maintained skill. The external method owns the discussion; Tusker owns document placement, preservation of decisions and conversion to task contracts. A request to turn settled intent into tickets does not restart an interview.

Use pstack's [technical-writing](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/technical-writing/SKILL.md) and [unslop](https://github.com/cursor/plugins/blob/7366ac128bdf95f45e6734f412b49a4031800169/pstack/skills/unslop/SKILL.md) as named, versioned external writing references at commit `7366ac128bdf95f45e6734f412b49a4031800169`. Prefer an available installed skill; a pinned source URL remains a reviewable reference when it is not installed. Document how to obtain the optional dependency with existing host tooling after checking support and license. Do not invent an installation command, install automatically, or import the wider pstack mode. Record source/version and update procedure once in the existing skills documentation; do not build a registry or vendor the external rule catalogs.

The invocation contract states the task, when the external guidance applies, what artifact returns, and which local constraints still bind. Use design guidance for unresolved product choices and writing guidance for authoring or reviewing prose. No extra model call is required merely to compose instructions. If an optional skill cannot be resolved, report that fact and apply Tusker's short local requirements without pretending to have used the external skill. An explicitly requested unavailable skill is reported to the user with its exact missing dependency.

Keep product commitments, accepted decisions, non-goals, exact commands, identifiers, permission boundaries and uncertainty intact when editing prose. Lead with the reader's outcome, name the actor, show expected results and failure paths, use consistent terms and keep technical reference detail nearby. These are Tusker's output requirements; the external source owns the detailed editing method. Model choices come from configured work/review levels, not skill prose.

Retire Tusker's duplicated interview implementation at the canonical `skills/spec/` and its scaffold caller in `cmd/tusker/init_docs.go`. New scaffolds should route through the one operating skill and external design guidance. Inspect current managed copies and provenance before selecting a migration; preserve user-modified skills and report any manual refresh needed. Do not hand-edit global installations or derived copies independently of their source.

## Bounded outcomes

| Requirement | Outcome |
| --- | --- |
| D1 | Design and writing routes reference available external methods with truthful dependency/provenance handling, while Tusker retains one operating package and configurable models. |
| D2 | Everyday task, wave and proof documentation explains the actual action, expected result and failure path in plain language; command-result examples agree with the executor-owned result rule. |
| D3 | The current design documents expose settled decisions, open questions and next steps without requiring readers to replay superseded directions; durable history remains discoverable. |
| D4 | Worker verification and knowledge routes give bounded, reproducible recipes and distinguish current documentation, inspected source, executed behavior, historical intent and inference. |
| D5 | The prose lint accepts ordinary slash-separated examples while retaining useful diagnostics for real code paths and symbols; no new general style scoring or banned-word system is added. |
| D6 | The combined package is checked from an isolated fresh project, examples are exercised within their permitted scope, and a concise report identifies usability results, actual reads and unresolved limitations. |

## Parallel ownership and existing work

The five implementation tickets own separate files: external skill composition/scaffolding; user-facing workflow docs; current design docs; worker verification/knowledge references; and prose-lint detection/tests. The final integration ticket depends on all five and alone owns generated documentation maps and the combined acceptance report. Individual authors use targeted document checks and report broken foreign links to their owner. Shared Go validation uses the existing build-slot policy and runs after coherent edits; documentation authors do not rebuild the application.

These are narrow delivery slices of existing follow-ups, not replacements for all their obligations. FLW-T-0011 still owns its broader CLI/help/completion contract; the new documentation and recipe tickets supply its prose/example subset. FLW-T-0026 still owns broader instruction provenance and lifecycle coverage; the composition and design-document tickets supply the specifically observed gaps. FLW-T-0025 remains the broader full-lifecycle downstream trial. Before assigning an older umbrella task alongside this work, reconcile ownership and reuse accepted artifacts so the same files are not assigned twice. No existing task is marked complete, superseded or freed from dependencies by this plan.

Runtime code, provider adapters, message delivery, profile settings, active test fixtures, UI components, `docs/system/cli.md` and `docs/system/delivery-and-waves.md` stay with their current owners. In particular ACO-T-0007 owns the wave guide during coordination qualification; the auxiliary writer links to it and records suggestions for that owner instead of editing it concurrently. A recipe may document only commands supported by the inspected candidate and must state when live coordination is unqualified. The synthetic demo's build-skip defect remains with execution qualification; it is not repaired by rewriting docs. Preserve current automation, capacity and installed runtimes. Independent ownership permits parallel assignment when capacity is available; the import itself does not change capacity.

## Acceptance approach

Use runnable command examples, focused behavioral checks and independent review of preserved meaning. A green link/metadata check alone does not certify writing quality. Do not require screenshots or provider turns for a prose-only edit. The final check covers external-method routing, missing optional dependencies, an already settled spec, a fresh reader's bounded discovery, executor-recorded command proof and preservation of user-modified installed guidance. Record zero matched tests, skipped checks and unavailable live coverage honestly. Count actual reads and command-discovery attempts; no invented percentage-savings target.

Independent review evaluates meaning and the reader exercises through the normal review lane. Executable checks cover structure and runtime facts only; their passing does not replace that review. Do not encode an agent review as manual human proof or add a human gate solely to satisfy a planning schema.

This document is the governing contract for the auxiliary wave and is not an implementation-owned rewrite target. That keeps the current-design task from changing the contract of its parallel siblings while editing the older planning documents.

<!-- tusker:delivery-import:463a577f0d4625f6:begin -->

## Work streams

- `[[FLW-T-0034]]` implements delivery source `agent-recipes`.
- `[[FLW-T-0036]]` implements delivery source `combined-check`.
- `[[FLW-T-0033]]` implements delivery source `current-design`.
- `[[FLW-T-0031]]` implements delivery source `external-methods`.
- `[[FLW-T-0035]]` implements delivery source `prose-lint`.
- `[[FLW-T-0032]]` implements delivery source `workflow-docs`.

- `[[W-0018]]` is the imported delivery wave.

<!-- tusker:delivery-import:463a577f0d4625f6:end -->
