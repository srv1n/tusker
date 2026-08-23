---
schema: "tusker.epic/v7"
kind: "epic"
id: "TBR"
project: "tusker"
title: "TuskerBar runtime reliability"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-08-23T14:27:23Z"
updated_at: "2026-08-23T15:03:09Z"
state_rev: "sha256:9abfe749c77dfcb88acef582a7a53f9ea240731e9aef83ca831601acfa0f36ea"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "TBR epic: TuskerBar runtime reliability."
---

# TBR · TuskerBar runtime reliability

## Thesis

Keep the installed macOS shell, bundled daemon, and Serve UI healthy across opt-in project state.

## Success criteria

- [ ] TuskerBar can start Serve when registered projects are disabled.
- [ ] TuskerBar reports the process that owns the live runtime.
- [ ] Deep links use current UI routes.
- [ ] Focused Go and Swift checks protect the behavior.

## Current decision

Disabled projects stay visible in the registry. TuskerBar can use one as the
Serve target, but the daemon must not dispatch its tasks.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[TBR-T-0001]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
