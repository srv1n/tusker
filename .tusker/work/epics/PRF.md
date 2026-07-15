---
schema: "tusker.epic/v7"
kind: "epic"
id: "PRF"
project: "tusker"
title: "Daemon and serve performance overhaul"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-11T08:10:04Z"
updated_at: "2026-07-15T05:08:07Z"
state_rev: "sha256:9d61bda75521029a66551890c29faac52fe288ed47b736786b992c27389689d6"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "PRF epic: Daemon and serve performance overhaul."
---

# PRF · Daemon and serve performance overhaul

## Thesis

Eliminate the ~1.3-core continuous CPU burn in tusker daemon run: 5s full-vault reconcile polls across 27 registered projects (~2750 md files), cache-free YAML re-parsing, redundant serve snapshot walks, unconditional stream invalidations, and per-tick git/ps subprocess churn.

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
| [[PRF-T-0001]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0003]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0004]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0005]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0006]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0007]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0008]] | ready | agent | Execute the task contract and satisfy proof mode. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
