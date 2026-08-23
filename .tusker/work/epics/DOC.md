---
schema: "tusker.epic/v7"
kind: "epic"
id: "DOC"
project: "tusker"
title: "Current product documentation"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-08-23T13:41:04Z"
updated_at: "2026-08-23T15:41:58Z"
state_rev: "sha256:db29018b6d76d82103d58f415a3b8d1559a69369996b0828d94f3efeeb3161c0"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "DOC epic: Current product documentation."
---

# DOC · Current product documentation

## Thesis

Keep only source-backed documentation for the product that exists now.

## Success criteria

- [ ] Old tickets, plans, reports, design packs, and migration guides are absent.
- [ ] Current product behavior has one small documentation set under `docs/system/`.
- [ ] Each system page names the code files that support its claims.
- [ ] User-facing text does not present Tusker as a sequence of product versions.
- [ ] The fresh tracker contains only work discovered or started after the reset.

## Current decision

Code and stored schemas define product behavior. `docs/system/` explains that
behavior. Historical proposals and delivery records are not product canon.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[DOC-T-0001]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
