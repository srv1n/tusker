---
schema: tusker.task/v7
kind: task
id: "{{id}}"
title: "{{title}}"
epic: "{{epic}}"
status: ready
readiness: ready
priority: p2
risk: medium
size: m
domains: []
proof_mode: inline
proof_status: pending
proof_required: []
evidence_budget: 0
raw_artifacts_allowed: false
next_owner: agent
next_source: task
next_ref: "{{id}}"
next_action: "Define acceptance, implement the smallest change, satisfy proof mode, and request review."
agent_action: continue
state_rev: 1
created_at: "{{date}}"
updated_at: "{{date}}"
---

# {{id}} · {{title}}

## Agent capsule

- Essence: {{title}}.
- Next action: define acceptance, do the smallest scoped change, satisfy proof mode, and request review.
- Read next: this note, then only the code/docs anchors named here.
- Avoid: raw logs, full transcripts, generated indexes, copied source files, and attachments unless doing evidence forensics.

## Intent

## Acceptance contract

| ID | Outcome | Proof required | Owner |
|---|---|---|---|
| A1 |  | focused_test or inline verification | agent |

## Scope

In:
-

Out:
-

## Deliverables

-

## Verification

| Acceptance | Check | Result | Notes |
|---|---|---|---|

## Evidence

_No evidence yet. Add evidence only when proof mode requires a durable card or artifact._

## Knowledge delta

| Topic | Before | After | Audience | Target knowledge |
|---|---|---|---|---|
| _none_ | _none_ | _none_ | developer | none |
