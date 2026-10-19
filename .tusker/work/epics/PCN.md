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
updated_at: "2026-10-19T15:13:12Z"
state_rev: "sha256:9ec2fc00b13526f87e77e3826d92b25b874d239d87118a64a92ecb2c988d8f2d"
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
| [[PCN-T-0004]] | backlog | agent | Wait for dependency PCN-T-0003 to reach done. |
| [[PCN-T-0005]] | backlog | blocked_dependency | Wait for dependency PCN-T-0004 to reach done. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[PCN-T-0001]] | human:sarav | 2026-10-19T15:12:35Z |
| [[PCN-T-0002]] | human:sarav | 2026-10-19T15:12:36Z |
| [[PCN-T-0003]] | human:sarav | 2026-10-19T15:12:51Z |
