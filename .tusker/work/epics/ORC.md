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
updated_at: "2026-07-21T06:09:14Z"
state_rev: "sha256:dffa359a2d872b290e2daa385309dd04688c5886c7f7d494c2c752bdf0f476be"
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
| _None._ |  |  |  |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| [[ORC-T-0001]] | reviewer:fable | 2026-07-21T04:41:22Z |
| [[ORC-T-0002]] | reviewer:fable | 2026-07-21T04:41:23Z |
| [[ORC-T-0003]] | reviewer:fable | 2026-07-21T04:41:23Z |
| [[ORC-T-0004]] | reviewer:fable | 2026-07-21T04:41:23Z |
| [[ORC-T-0005]] | reviewer:fable | 2026-07-21T04:41:23Z |
| [[ORC-T-0006]] | reviewer:fable | 2026-07-21T04:41:24Z |
| [[ORC-T-0007]] | reviewer:fable | 2026-07-21T04:41:24Z |
| [[ORC-T-0008]] | reviewer:fable | 2026-07-21T04:41:24Z |
| [[ORC-T-0009]] | reviewer:fable | 2026-07-21T04:41:39Z |
| [[ORC-T-0010]] | reviewer:fable | 2026-07-21T04:56:09Z |
| [[ORC-T-0011]] | reviewer:fable | 2026-07-21T05:18:00Z |
| [[ORC-T-0012]] | reviewer:fable | 2026-07-21T05:37:45Z |
| [[ORC-T-0013]] | reviewer:fable | 2026-07-21T06:09:14Z |
