---
title: "Proof and closeout"
subject: proof-and-closeout
part_of: overview
status: canonical
read_when: "Choosing evidence, checking acceptance coverage, or closing a task."
skip_when: "Planning dependencies or choosing a provider."
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

Screenshot, video, and trace requirements need matching evidence types. A
filename extension cannot turn a test result into visual evidence. The wave
brief displays the evidence type actually recorded, not the artifact type
promised by the task.

An artifact contract is complete only when current accepted evidence has the
contract's matching type, covers each promised acceptance row, and retains a
readable copied artifact with its recorded fingerprint. If the task records a
source revision, the evidence records the same revision. A replaced artifact,
an external link, or old evidence without an identity stays visible but cannot
complete the contract.

Use `--proof-category visual|performance|backend|migration` with compact
`--proof-facts key=value,...` only when that category is required. Visual
evidence records a before/after pair or `baseline=new_ui`; performance records
both values and matching workloads; backend records an observable and a
negative case; migration records preservation, interruption, and recovery.
These fields establish structure and provenance. They do not judge image
quality or substitute for an explicit acceptance decision.

## Review

A reviewer submits a typed result for one task revision, work revision,
implementation revision, proof fingerprint, and gate fingerprint. A review
submission does not merge, land, move a ref, or close the task.

The valid review results are pass, changes requested, and blocked. A later
change makes a result stale when its bound facts change, including a changed
artifact fingerprint or a changed proof snapshot. The implementation attempt
and reviewer attempt remain separate authorities.

For a live interactive review, start the review lane with `tusker work review
<task-id> --by reviewer:<name>`. Its packet carries the review attempt, the
linked completed implementation attempt and actor, current proof and gate
fingerprints, and a material fingerprint of the implementation workspace. Its
workspace is that exact implementation workspace, rather than a fresh `HEAD`
checkout that would omit an uncommitted diff. The fingerprint covers tracked
and untracked files in the task's declared owned paths, generated outputs,
knowledge nodes, and repository-local spec references. The stored scope is part
of the end-state identity and cannot be narrowed later. Tusker's mutable
control-plane records are excluded. The route and close consumer recheck that
scope against the implementation workspace, so later relevant dirty edits stale
the receipt while unrelated parallel-task changes do not. Daemon execution and
proposal harvest use the same server-derived scope and reject a missing scope;
whole-project gate-ledger checks retain their separate full-tree semantics. Use
the packet's `next` command to submit the receipt. The path rejects a reviewer
who is the
implementation-session actor; it does not claim that structural checks
establish human authority or subjective quality.

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
