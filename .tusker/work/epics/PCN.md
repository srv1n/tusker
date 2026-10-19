---
schema: "tusker.epic/v7"
kind: "epic"
id: "PCN"
project: "tusker"
title: "Process containment"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
spec_refs:
  - ".tusker/specs/process-containment.md"
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-10-18T12:39:36Z"
updated_at: "2026-10-19T00:48:26Z"
state_rev: "sha256:84355aa8b291dc10da825c58e34c7e1cacc1b03e34dcd0d089e9ffbeca3ed385"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "PCN epic: Process containment."
---

# PCN · Process containment

## Thesis

Tusker keeps an inventory of every process it starts and can stop all of them; see the process-containment spec.

## Success criteria

- [ ] Define success criteria.

## Current decision

TBD.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[PCN-T-0001]] | review | reviewer | Review evidence and close or return to rework. |
| [[PCN-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[PCN-T-0003]] | review | reviewer | Review evidence and close or return to rework. |
| [[PCN-T-0004]] | backlog | blocked_dependency | Wait for dependency PCN-T-0003 to reach done. |
| [[PCN-T-0005]] | backlog | blocked_dependency | Wait for dependency PCN-T-0001 to reach done. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
