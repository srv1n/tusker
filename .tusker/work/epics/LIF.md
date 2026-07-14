---
schema: "tusker.epic/v7"
kind: "epic"
id: "LIF"
project: "tusker"
title: "Reliable execution lifecycle and truthful delivery"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
spec_refs:
  - "docs/specs/reliable-execution-lifecycle.md"
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-12T11:03:42Z"
updated_at: "2026-07-14T13:55:42Z"
state_rev: "sha256:93607a0c3990828aec52e1db05e65c563b297188f23da69d84d41557ad24f15f"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "LIF epic: Reliable execution lifecycle and truthful delivery."
---

# LIF · Reliable execution lifecycle and truthful delivery

## Thesis

Make manual and automated Codex/Claude work project-local, exclusively claimed, heartbeat-backed, resumable, and truthfully represented; disable unreliable token controls until delivery works.

## Success criteria

- [ ] Codex and Claude default to the registered repository cwd; worktrees are explicit opt-in.
- [ ] Daemon and manual actors share atomic task ownership, heartbeat, terminal outcomes, and project concurrency.
- [ ] Every run is inspectable and resumable with truthful project/workspace/session metadata.
- [ ] Work board and Settings derive liveness from real runtime state and never production fixtures.
- [ ] Delivery requires a real diff/artifact plus acceptance-mapped verification.
- [ ] Token budgets, circuit breakers, blocking, and authoritative aggregates remain disabled.
- [ ] The complete automated dogfood matrix passes without human gates.

## Current decision

Prioritize reliable delivery and operator visibility over token optimization. Use shared registered-project workspaces by default, model in-progress work as live runtime ownership rather than a durable task status, and serialize modifying work per project until explicitly configured otherwise. `SRV-T-0028` remains the native-picker implementation; LIF consumes it in final integration rather than duplicating it.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[LIF-T-0012]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0013]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[LIF-T-0001]] | reviewer:codex-lifecycle | 2026-07-14T13:55:07Z |
| [[LIF-T-0002]] | reviewer:codex-lifecycle | 2026-07-14T13:50:12Z |
| [[LIF-T-0003]] | reviewer:codex-lifecycle | 2026-07-14T13:50:12Z |
| [[LIF-T-0004]] | reviewer:codex-lifecycle | 2026-07-14T13:51:22Z |
| [[LIF-T-0005]] | reviewer:codex-lifecycle | 2026-07-14T13:52:53Z |
| [[LIF-T-0006]] | reviewer:codex-lifecycle | 2026-07-14T13:53:42Z |
| [[LIF-T-0007]] | reviewer:codex-lifecycle | 2026-07-14T13:52:53Z |
| [[LIF-T-0008]] | reviewer:codex-lifecycle | 2026-07-14T13:55:07Z |
| [[LIF-T-0009]] | reviewer:codex-lifecycle | 2026-07-14T13:55:07Z |
| [[LIF-T-0010]] | reviewer:codex-lifecycle | 2026-07-14T13:55:07Z |
| [[LIF-T-0011]] | reviewer:codex-lifecycle | 2026-07-14T13:55:42Z |
