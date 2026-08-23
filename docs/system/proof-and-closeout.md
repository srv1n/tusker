---
title: "Proof and closeout"
subject: proof-and-closeout
part_of: overview
status: canonical
---

# Proof and closeout

Proof shows that a result meets one or more acceptance rows.

## Proof modes

New tasks use inline proof by default. A critical-risk task uses audit proof by
default. The supported modes are `none`, `inline`, `card`, `artifact`, and
`audit`.

Inline proof stores check results on the task. The other evidence-bearing modes
can add evidence files. The default evidence budgets are zero for inline, one
for card, three for artifact, and five for audit.

## Record a result

Use `tusker verify add`. Store the exact command or manual check. Store `pass`
or `fail`. Link the result to the acceptance row IDs.

Raw logs belong in `.tusker/scratch/<TASK-ID>/`. Scratch is temporary. Move a
durable artifact to an evidence record before close.

## Review

A reviewer submits a typed result for one task revision, work revision,
implementation revision, proof fingerprint, and gate fingerprint. A review
submission does not merge, land, move a ref, or close the task.

The valid review results are pass, changes requested, and blocked. A later
change makes a result stale when its bound facts change.

## Close checks

The close path checks the current task, acceptance coverage, proof, gates,
review authority, and repository state. The default close policy needs a
reviewer or a person. Risk alone does not add a gate or required evidence kind.
Project configuration can add exact evidence or gate rules.

When only a person can finish the task, closeout writes a bounded checkpoint
and sets `stop_until_human_response`. It does not invent approval.

## Code sources

- `cmd/tusker/v7_proof_cmd.go`
- `cmd/tusker/review_result.go`
- `cmd/tusker/v7_closeout_cmd.go`
- `cmd/tusker/v7_close_authority.go`
- `internal/v7policy/close.go`
