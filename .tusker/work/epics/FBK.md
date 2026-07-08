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
updated_at: "2026-07-08T05:06:10Z"
state_rev: "sha256:669a4366f4032293690e683b38549618d5c0b051c3f98bc2107074d329e6a212"
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
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[FBK-T-0001]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[FBK-T-0002]] | ready | agent | Execute the task contract and satisfy proof mode. |
| [[FBK-T-0004]] | ready | agent | Execute the task contract and satisfy proof mode. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[FBK-T-0003]] | reviewer:agent | 2026-07-07T13:21:34Z |
| [[FBK-T-0005]] | reviewer:agent | 2026-07-07T11:13:41Z |
