---
schema: "tusker.epic/v7"
kind: "epic"
id: "AGX"
project: "tusker"
title: "Agent experience: token economy and CLI ergonomics"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-05T11:17:22Z"
updated_at: "2026-07-14T13:05:01Z"
state_rev: "sha256:5afba4017ff849805e0c767d7a2ebcbb7b918ddf7bcf2241ff1b1832061b546a"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or runtime attempt."
  use_when: "Use to triage work on packets, capsules, command budgets, and agent-facing task flow."
  what: "AGX epic for reducing agent token burn and improving CLI ergonomics."
---

# AGX · Agent experience: token economy and CLI ergonomics

## Thesis

Cut token burn per task: one-hop command discovery, fewer roundtrips per state update, risk-gated documentation churn, and measured packet budgets.

## Success criteria

- [ ] Define success criteria.

## Current decision

TBD.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| [[AGX-G-0001]] | human:sarav | [[AGX-T-0006]] | Decide whether AGX-T-0006 may proceed with targeted traceability proof despite the existing go test ./... baseline failures, or send a separate repair/rework task for those broad-suite failures. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[AGX-T-0001]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[AGX-T-0002]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[AGX-T-0004]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[AGX-T-0005]] | review | reviewer | Review evidence and close or return to rework. |
| [[AGX-T-0006]] | review | human:sarav | Accept, waive, or return rework for AGX-G-0001. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[AGX-T-0003]] | reviewer:agent | 2026-07-06T06:37:08Z |
| [[AGX-T-0007]] | reviewer:codex-reviewer | 2026-07-14T13:05:01Z |
