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
updated_at: "2026-07-06T03:54:02Z"
state_rev: "sha256:723302fa0f0b9ed292616c867e454cc0d377ed46c027e16180c0f68b378672c8"
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

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
