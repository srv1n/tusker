---
schema: "tusker.epic/v7"
kind: "epic"
id: "VSD"
project: "tusker"
title: "Ship V7 as the default Tusker model"
status: "active"
owner: "human:sarav"
priority: "p2"
domains:
  - "cli"
  - "knowledge"
  - "validation"
  - "workflow"
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-05-15T04:45:46Z"
updated_at: "2026-05-19T06:18:52Z"
state_rev: "sha256:02fc002bd7d97e19f1b7464c32894246f2d6584db6a978cd679d8f485a49821e"
---

# VSD · Ship V7 as the default Tusker model

## Thesis

V7 is the main Tusker product, not a compatibility layer. The shippable system is repo-local, branch-safe, agent-native work tracking with skills-style project knowledge. V5/V6 tracker behavior, Diataxis docs publishing, and migration scaffolding are either deleted or quarantined behind explicit legacy commands.

## Success criteria

- [ ] A fresh clone can run `go test ./...`, `go vet ./...`, and Tusker validation without missing module, dependency, or embedded asset failures.
- [ ] Top-level CLI commands create, select, validate, reconcile, and close V7 work objects by default.
- [ ] A clean checkout can create a V7 task, have the daemon claim it, run a fake runner, attach acceptance-linked evidence, enter review, and close according to proof policy.
- [ ] Legacy tracker, docs-map, canon publishing, and V6 knowledge graph surfaces are removed from the main path or exposed only under `tusker legacy ...`.
- [ ] Canonical knowledge uses SKILL routing, domain INDEX/CANON, runbooks, decisions, interfaces, invariants, glossary, and sources. Diataxis modes are not accepted in V7 canonical knowledge.
- [ ] Branch policy, evidence durability, screenshot review, acceptance coverage, and close validation make `done` mean accepted outcome, not "some artifact exists."
- [ ] V7 implementation code is decomposed enough that agents can work in bounded packages with structure tests preventing drift back into monoliths.

## Current decision

No backward compatibility requirement for the V7 release cut. Prefer hard deletion or explicit legacy quarantine over preserving old defaults. Optional human docs publishing may exist later, but it is not canonical project knowledge and must not leak into V7 task, gate, evidence, or packet semantics.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[VSD-T-0001]] | ready | agent | Execute the task contract and attach evidence. |
| [[VSD-T-0002]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0003]] | ready | blocked_dependency | Wait for dependency VSD-T-0002 to reach done. |
| [[VSD-T-0004]] | ready | blocked_dependency | Wait for dependency VSD-T-0002 to reach done. |
| [[VSD-T-0005]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0006]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0007]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0008]] | ready | blocked_dependency | Wait for dependency VSD-T-0007 to reach done. |
| [[VSD-T-0009]] | ready | blocked_dependency | Wait for dependency VSD-T-0005 to reach done. |
| [[VSD-T-0010]] | ready | blocked_dependency | Wait for dependency VSD-T-0003 to reach done. |
| [[VSD-T-0011]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0012]] | ready | blocked_dependency | Wait for dependency VSD-T-0002 to reach done. |
| [[VSD-T-0013]] | ready | blocked_dependency | Wait for dependency VSD-T-0009 to reach done. |
| [[VSD-T-0014]] | ready | blocked_dependency | Wait for dependency VSD-T-0001 to reach done. |
| [[VSD-T-0015]] | ready | blocked_dependency | Wait for dependency VSD-T-0014 to reach done. |
| [[VSD-T-0016]] | ready | blocked_dependency | Wait for dependency VSD-T-0015 to reach done. |
| [[VSD-T-0017]] | ready | blocked_dependency | Wait for dependency VSD-T-0016 to reach done. |
| [[VSD-T-0018]] | ready | blocked_dependency | Wait for dependency VSD-T-0016 to reach done. |
| [[VSD-T-0019]] | ready | blocked_dependency | Wait for dependency VSD-T-0016 to reach done. |
| [[VSD-T-0020]] | ready | blocked_dependency | Wait for dependency VSD-T-0016 to reach done. |
| [[VSD-T-0021]] | ready | blocked_dependency | Wait for dependency VSD-T-0016 to reach done. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[VSD-T-0022]] | reviewer:agent | 2026-05-19T06:18:50Z |
| [[VSD-T-0023]] | reviewer:agent | 2026-05-19T06:18:50Z |
| [[VSD-T-0024]] | reviewer:agent | 2026-05-19T06:18:50Z |
| [[VSD-T-0025]] | reviewer:agent | 2026-05-19T06:18:51Z |
| [[VSD-T-0026]] | reviewer:agent | 2026-05-19T06:18:51Z |
| [[VSD-T-0027]] | reviewer:agent | 2026-05-19T06:18:51Z |
| [[VSD-T-0028]] | reviewer:agent | 2026-05-19T06:18:51Z |
