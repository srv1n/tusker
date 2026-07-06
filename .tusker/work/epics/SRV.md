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
updated_at: "2026-07-06T04:37:47Z"
state_rev: "sha256:b0b804be0c04069b9e2f65ecb2e4e1b464874e9b24b05031980d6efc8c318a0f"
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
| [[SRV-G-0001]] | human:sarav | [[SRV-T-0002]] | Review the serve spec (docs/specs/10-tusker-serve.md) and the UX design produced from docs/design/tusker-serve-ux-packet.md; attach approved design frames as evidence on SRV-T-0002. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[SRV-T-0001]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0002]] | backlog | human:sarav | Accept, waive, or return rework for SRV-G-0001. |
| [[SRV-T-0003]] | backlog | blocked_dependency | Wait for dependency SRV-T-0002 to reach done. |
| [[SRV-T-0004]] | backlog | blocked_dependency | Wait for dependency SRV-T-0001 to reach done. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
