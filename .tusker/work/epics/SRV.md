---
schema: "tusker.epic/v7"
kind: "epic"
id: "SRV"
project: "tusker"
title: "Tusker Serve: local control-room UI"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or runtime attempt."
  use_when: "Use to triage needs-me queue, read/edit UI, runtime-store API, and embedded SPA work."
  what: "SRV epic for the local Tusker Serve control-room UI and JSON API."
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-05T11:16:22Z"
updated_at: "2026-07-07T13:26:53Z"
state_rev: "sha256:ab45c85e14b241d742d5dc9f6ffa1e34653d57900148502034def037e34c62c0"
---

# SRV · Tusker Serve: local control-room UI

## Thesis

A local control room served by the tusker binary: React + TanStack + Tailwind SPA embedded via go:embed over a JSON API on the SQLite runtime store and vault. Read-only MVP centered on an attention-routed needs-me queue, then a TipTap reading/editing layer.

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
| [[SRV-T-0001]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0003]] | backlog | agent | Wait for dependency SRV-T-0002 to reach done. |
| [[SRV-T-0004]] | backlog | blocked_dependency | Wait for dependency SRV-T-0001 to reach done. |
| [[SRV-T-0005]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0007]] | ready | agent | Execute the task contract and satisfy proof mode. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[SRV-T-0002]] | reviewer:agent | 2026-07-06T16:02:52Z |
| [[SRV-T-0006]] | reviewer:agent | 2026-07-06T16:02:52Z |
