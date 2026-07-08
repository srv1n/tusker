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
updated_at: "2026-07-11T01:54:12Z"
state_rev: "sha256:ce68868dbdc1f86e0bbe1a681483ca362af5440bb5e336541a5808bafc946354"
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
| [[SRV-G-0002]] | human:sarav | [[SRV-T-0023]] | Open /panel?shell=1 at 420x640 and verify sections, stream refresh, no horizontal scroll, chrome hiding, client-side navigation, and the Open Tusker header shortcut. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[SRV-T-0001]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0003]] | backlog | agent | Wait for dependency SRV-T-0002 to reach done. |
| [[SRV-T-0004]] | backlog | blocked_dependency | Wait for dependency SRV-T-0001 to reach review with satisfied proof or done. |
| [[SRV-T-0005]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0009]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0011]] | backlog | blocked_dependency | Wait for dependency SRV-T-0004 to reach review with satisfied proof or done. |
| [[SRV-T-0012]] | backlog | blocked_dependency | Wait for dependency SRV-T-0011 to reach review with satisfied proof or done. |
| [[SRV-T-0018]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0019]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0020]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[SRV-T-0021]] | review | reviewer | Review evidence and close or return to rework. |
| [[SRV-T-0022]] | review | reviewer | Review evidence and close or return to rework. |
| [[SRV-T-0023]] | ready | human:sarav | Accept, waive, or return rework for SRV-G-0002. |
| [[SRV-T-0024]] | review | reviewer | Review evidence and close or return to rework. |
| [[SRV-T-0025]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[SRV-T-0002]] | reviewer:agent | 2026-07-06T16:02:52Z |
| [[SRV-T-0006]] | reviewer:agent | 2026-07-06T16:02:52Z |
| [[SRV-T-0007]] | human:sarav | 2026-07-07T07:28:00Z |
| [[SRV-T-0008]] | human:sarav | 2026-07-08T03:01:48Z |
| [[SRV-T-0010]] | reviewer:codex | 2026-07-07T16:33:21Z |
| [[SRV-T-0013]] | human:sarav | 2026-07-07T10:40:35Z |
| [[SRV-T-0014]] | human:sarav | 2026-07-08T05:10:07Z |
| [[SRV-T-0015]] | human:sarav | 2026-07-08T06:14:48Z |
| [[SRV-T-0016]] | human:sarav | 2026-07-08T06:14:48Z |
| [[SRV-T-0017]] | human:sarav | 2026-07-08T07:12:47Z |
