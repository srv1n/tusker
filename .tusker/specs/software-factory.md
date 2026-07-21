---
title: "Software Factory: Tusker as the production loop harness"
status: canonical
created: 2026-07-21
read_when: "Planning, contract-cutting, or implementing any FAC epic work; onboarding a new agent session to the factory direction."
skip_when: "Working an unrelated bugfix or a task whose contract already embeds what you need."
decisions_locked: true
sources:
  - "AIE software-factories conference vault (mined 2026-07-21)"
  - "Loops-vs-graphs field research (2026-07-21)"
  - "Operator grill session (2026-07-21)"
---

# Software Factory: Tusker as the production loop harness

## Why we are building this

Writing code is no longer the expensive part. Agents write and review code well;
what they need is explicit definition-of-done, clean context, and a truthful
record of state. The operator's time goes to specifications, schemas, API
contracts, and storage decisions — never to reading plumbing code. Tusker is the
harness that makes that division of labor real for our own portfolio (this repo,
the rzn backend, Rust and Xcode projects). It is a private harness: every design
decision optimizes for our workflow, not a general audience.

## Operating principles (locked)

1. **Humans own intent, agents own execution.** The operator reviews specs,
   invariants, schemas, and artifacts. High-risk changes become a discussion
   between operator and agents. Routine diffs are never human-reviewed.
2. **Artifact-first acceptance.** A UI change proves itself with before/after
   screenshots; a performance change with before/after benchmarks; a security
   change with a posture artifact. Humans approve artifacts, not code.
3. **Human gates are for blockers only**: missing credentials, an unclear spec,
   or a replacement decision ("this will supersede that"). Never for code
   review. Skills and prompts must state this.
4. **Plain language everywhere.** Task contracts, logbooks, discussions, and
   the code itself (variable and function names) prefer simple human words over
   jargon. A former-PM reader must understand any task's top layer.
5. **Two-layer tasks.** Every contract leads with a PM-readable layer (what,
   why, acceptance-as-artifacts, plain sentences, no symbol names) and carries
   an implementer appendix (file map, symbols, exact commands) that only
   workers load. Proof stays capsule-compact; no transcript dumps.
6. **Validator is never the author.** Review runs in fresh context, ideally a
   different vendor/model than the implementer. Contracts are written before
   code; proof tests must fail on the base commit.
7. **No stacking.** A loop does not run again while its previous output is
   un-adjudicated. Merge windows are the fan-in points.
8. **The spec is the durable artifact.** Canonical, no-versions,
   Obsidian-style backlinked docs with front-matter (`read_when`/`skip_when`);
   updated in place. Code is regenerable; specs and decisions are not.
9. **Work registers in Tusker no matter who drives.** Daemon-dispatched work
   and interactive Claude/Codex sessions both claim, update, and close through
   the CLI so the UI and stream board are always truthful.
10. **Measured floors, not guesses.** Any resource floor (disk, time budgets)
    is measured, never estimated (the 2026-07-20 unmeasured-15GB incident).

## Architecture stance

The consensus hybrid from the field holds and matches what we built: a **DAG of
task contracts on the outside** (dependency-scheduled, gate-fenced, durable)
with **loop-ish workers on the inside** (an agent free to traverse its own path
within one task). We do not adopt a graph framework; we harden our own DAG
execution: idempotent re-dispatch, bounded fan-out, and quarantine of
dependents when a gate fails.

## The four streams (FAC epic)

### Stream A — Session-attach and the honest UI
Any interactive Claude/Codex session picks up work through the tusker skill:
claim sets a flag ("worked outside the daemon"), status flows through the CLI,
and the stream board, logbook, and serve UI always reflect reality. This is
first because broken visibility is what forced manual orchestration.

### Stream B — Reviewer lane and PM-readable tasks
After a worker submits, the daemon spawns an independent reviewer (different
vendor/model where configured) against the diff; findings auto-return the task
to the implementer with the finding pasted in; the task closes only after
adjudication. Task templates move to the two-layer format with a plain-language
lint on the top layer. `tusker accept` collapses status+verify+close for a
reviewer with proof rows already green.

### Stream C — Build and test economics
Written policy plus gate-tier enforcement: per-change gates run check + lint +
focused tests on touched crates/packages only; wave-end runs one collective
compile+lint+workspace-test; the full suite runs nightly on the Hetzner box
(Crabbox + sccache to the Hetzner bucket). Optimistic apply across stories with
disjoint file ownership is the sanctioned pattern. Shared build caches across
worktrees, a hard cap on live worktrees, and measured disk floors before any
gate. Xcode/Mac work stays local. Escalation timer: if a local gate exceeds its
budget or disk drops below the floor, route the run to a VM.

### Stream D — Graph hardening
Idempotent re-dispatch of a failed task without re-running neighbors; a bounded
fan-out cap on daemon dispatch; gate-failure quarantine that holds dependents
instead of letting a red node's children start; boundary traces on worker runs
so a failed attempt can be replayed for adjudication without new model calls.

## Deferred (explicitly not now)

- Cloud sandboxes as the default execution substrate (napkin-math doc plus one
  Xcode spike first; Crabbox stays the nightly/heavy-push tool).
- Sensor/set-point loops where the daemon generates its own tasks from
  telemetry (after Streams A-D land).
- Productizing for other teams.
- `tusker describe` feature-impact index and read-receipt doc gates (queued
  behind the first wave).

## Spec pipeline (how work is born)

A grill-style spec session (frontier model, one decision at a time, facts
looked up, decisions put to the operator) walks: customer story → product
invariants → architecture → program design (types, signatures, contracts) →
vertical slices. The session's last step emits the epic and two-layer task
contracts with dependency order, each linking back to its spec section. Hard
tasks (p1 or medium+ risk) are blocked from ready without a spec link; trivial
work is exempt.
