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
updated_at: "2026-07-15T10:38:08Z"
state_rev: "sha256:c03e88fdb43daff6ea294763bb270d71a99cf33d5376dc8a7687bec24d3d164c"
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
| [[PRF-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0004]] | review | reviewer | Review evidence and close or return to rework. |
| [[PRF-T-0005]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[PRF-T-0001]] | reviewer:independent | 2026-07-15T06:03:19Z |
| [[PRF-T-0003]] | reviewer:codex | 2026-07-15T10:38:07Z |
| [[PRF-T-0006]] | reviewer:independent | 2026-07-15T06:03:19Z |
| [[PRF-T-0007]] | reviewer:independent | 2026-07-15T06:03:19Z |
| [[PRF-T-0008]] | reviewer:codex | 2026-07-15T10:38:08Z |
