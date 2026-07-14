---
schema: "tusker.epic/v7"
kind: "epic"
id: "FBK"
project: "tusker"
title: "Feedback intake and review"
status: "ready"
owner: "agent:codex"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-05T11:13:34Z"
updated_at: "2026-07-14T14:59:17Z"
state_rev: "sha256:0a32780f6101774d42e27b903d7420b0af435e6994e4b3cfa4d913083f7a6ed9"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or runtime attempt."
  use_when: "Use to triage feedback signals, review findings, promotion rules, and cross-vault health."
  what: "FBK epic for structured feedback intake, review, dedupe, and promotion."
---

# FBK · Feedback intake and review

## Thesis

Bring cross-repo agent feedback into structured Tusker review, promotion, and fanout.

## Success criteria

- [ ] Cross-repo agent feedback can be ingested into structured, provenance-preserving review input.
- [ ] Feedback review can produce stable findings that cite source repos, vaults, and notes.
- [ ] Promotion from feedback findings can create visible Tusker work without hidden fanout.
- [ ] Registry health and cross-vault duplicate handling prevent stale vaults or clone lanes from inflating counts.

## Current decision

Start from the 2026-07-05 feedback intake: 4 explicit notes collapsed to 2 product problems. Derived task/event signals are useful but currently too noisy for promotion until cross-vault dedupe and causal grouping are implemented.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| [[FBK-G-0001]] | human:sarav | [[FBK-T-0002]] | Resolve required broad_test proof: fix the existing red go test ./... baseline or amend/waive FBK-T-0002 broad_test proof. |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[FBK-T-0001]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[FBK-T-0002]] | backlog | human:sarav | Accept, waive, or return rework for FBK-G-0001. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[FBK-T-0003]] | reviewer:agent | 2026-07-07T13:21:34Z |
| [[FBK-T-0004]] | reviewer:codex-fbk4 | 2026-07-14T14:59:16Z |
| [[FBK-T-0005]] | reviewer:agent | 2026-07-07T11:13:41Z |
