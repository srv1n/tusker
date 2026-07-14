---
schema: "tusker.epic/v7"
kind: "epic"
id: "DEL"
project: "tusker"
title: "Autonomous delivery: approved spec to artifact-first wave"
status: "ready"
owner: "human:sarav"
priority: "p2"
domains: []
spec_refs:
  - "docs/specs/11-spec-to-wave-autonomous-delivery.md"
next_task_number: 1
next_gate_number: 1
next_decision_number: 1
created_at: "2026-07-14T11:20:08Z"
updated_at: "2026-07-14T11:22:37Z"
state_rev: "sha256:d7d88f6d0fff5eec1648647c05f1992b229ec416c6e3efdf65d172d6bbcfdca5"
capsule:
  skip_when: "Skip when you need a specific task contract, proof row, gate, or attempt."
  use_when: "Use to triage this workstream's scope, active tasks, and durable direction."
  what: "DEL epic: Autonomous delivery: approved spec to artifact-first wave."
---

# DEL · Autonomous delivery: approved spec to artifact-first wave

## Thesis

Turn canonical specs into authorized task DAGs that drain through isolated implementation, objective review, integration validation, and artifact-first delivery without inline human ceremony.

## Success criteria

- [ ] An approved spec can be imported as a validated, traceable task DAG and wave without models fabricating tracker state.
- [ ] One explicit arm action authorizes the complete fingerprinted wave; no per-task approval loop is required.
- [ ] The resident daemon drains every runnable DAG frontier through isolated implementation, objective review, integration validation, and landing.
- [ ] Routine code review, tests, logs, benchmarks, and objective artifacts never create a human gate.
- [ ] The final surface leads with screenshots, recordings, benchmark deltas, traces, or the best available compact delivery artifact.
- [ ] A disposable end-to-end dogfood run proves serial dependencies, parallel frontiers, failure containment, restart recovery, and cross-repo rollout.

## Current decision

The binding design is `docs/specs/11-spec-to-wave-autonomous-delivery.md`.
Execution authorization belongs to an armed wave; objective acceptance belongs
to independent reviewer agents; humans are exception-only for capability,
external authority, unresolved intent, or explicitly subjective acceptance.

## Open gates

<!-- tusker:generated open-gates -->

| Gate | Owner | Blocks | Action |
|---|---|---|---|
| _None._ |  |  |  |

## Active work

<!-- tusker:generated active-work -->

| Task | Status | Next owner | Next action |
|---|---|---|---|
| [[DEL-T-0001]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[DEL-T-0002]] | backlog | blocked_dependency | Wait for dependency DEL-T-0001 to reach review with satisfied proof or done. |
| [[DEL-T-0003]] | backlog | blocked_dependency | Wait for dependency DEL-T-0002 to reach review with satisfied proof or done. |
| [[DEL-T-0004]] | backlog | agent | Execute the task contract and satisfy proof mode. |
| [[DEL-T-0005]] | backlog | blocked_dependency | Wait for dependency DEL-T-0002 to reach review with satisfied proof or done. |
| [[DEL-T-0006]] | backlog | blocked_dependency | Wait for dependency DEL-T-0003 to reach done. |

## Recently completed

<!-- tusker:generated recently-completed -->

| Task | Accepted by | Closed at |
|---|---|---|
| _None._ |  | |
