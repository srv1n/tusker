---
schema: "tusker.epic/v7"
kind: "epic"
id: "ORC"
project: "tusker"
title: "First-class agent-native orchestration"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-20T03:38:53Z"
updated_at: "2026-07-20T07:05:22Z"
state_rev: "sha256:e2aa695eb0842231f9f758c70aa874189ff8635d31013e45e41c59a6fbfad83a"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "ORC epic: First-class agent-native orchestration."
---

# ORC · First-class agent-native orchestration

## Thesis

Graduate Tusker from task tracker to the enforcement point for multi-lane agent development: claims, boards, reports, gates, and shared namespaces become mechanical checks.

## Success criteria

- [ ] A second lane cannot claim work whose owned paths intersect a live claim; the refusal names the holder and its liveness.
- [ ] Any agent (or human) can answer "who is working where, on what branch, how stale" from one generated stream board — no hand-maintained state.
- [ ] No lane can end without a harness-verified end-state record (branch, SHA, worktree, clean/dirty, gate verdicts).
- [ ] Full-suite gates run as unattended scheduled batch; a red batch spawns a repair task instead of blocking anyone; an unchanged tree is never re-gated.
- [ ] Integration work is dispatched from a mechanical merge brief, not a hand-written prose prompt.

## Current decision

Doctrine source of truth is the installed operator skill
(`skills/tusker/references/INTEGRATION_MERGE.md`); this epic converts its rules
into CLI-enforced mechanics in leverage order: ORC-T-0001 → 0002 → 0003 → 0004
(p1 spine), then 0005/0006/0008 (p2), 0007 (p3). Origin evidence: the rzn
backend multi-lane collisions of 2026-07-19/20 (duplicate five-task
implementation, 0014 migration clash, invisible cross-stream main movement,
report-less overnight lane, eight redundant workspace gates).

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[ORC-T-0001]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0002]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0003]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0004]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0005]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0006]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0007]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0008]] | review | reviewer | Review evidence and close or return to rework. |
| [[ORC-T-0009]] | ready | agent | Execute the task contract and satisfy proof mode. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
