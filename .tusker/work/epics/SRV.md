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
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-05T11:16:22Z"
updated_at: "2026-07-06T16:26:00Z"
state_rev: "sha256:08dae5ac368eadca85061e7ca320208c101aede3b3988ae22f5b14d659db6d07"
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
