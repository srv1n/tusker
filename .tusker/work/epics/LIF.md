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
updated_at: "2026-07-14T08:21:52Z"
state_rev: "sha256:39761218388365a4ff40340c03f330458a5923e58f158c34dfb5f9c064140792"
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
| [[LIF-T-0001]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0003]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0004]] | review | blocked_dependency | Wait for dependency LIF-T-0003 to reach done. |
| [[LIF-T-0005]] | review | blocked_dependency | Wait for dependency LIF-T-0002 to reach done. |
| [[LIF-T-0006]] | review | blocked_dependency | Wait for dependency LIF-T-0003 to reach done. |
| [[LIF-T-0007]] | review | blocked_dependency | Wait for dependency LIF-T-0002 to reach done. |
| [[LIF-T-0008]] | review | blocked_dependency | Wait for dependency LIF-T-0003 to reach done. |
| [[LIF-T-0009]] | review | blocked_dependency | Wait for dependency LIF-T-0003 to reach done. |
| [[LIF-T-0010]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0011]] | review | blocked_dependency | Wait for dependency LIF-T-0002 to reach done. |
| [[LIF-T-0012]] | review | reviewer | Review evidence and close or return to rework. |
| [[LIF-T-0013]] | review | reviewer | Review evidence and close or return to rework. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
