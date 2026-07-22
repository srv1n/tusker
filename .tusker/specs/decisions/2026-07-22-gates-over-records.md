---
title: "Decision log: gates over records"
subject: gates-over-records
keywords: [process economics, proof, gates, logging, token budget]
part_of: software-factory
status: canonical
created: 2026-07-22
read_when: "You are adding, keeping, or trimming any Tusker process artifact — logs, attempts, traces, proof rows, hints, status ceremony — and need the test for whether it earns its cost in the agent era."
skip_when: "You need task mechanics or lifecycle commands — read the operator skill; this is the economic principle behind them."
decides_for: ".tusker/specs/software-factory.md"
---

# Decision log: gates over records (2026-07-22)

Operator-initiated. The session started from a worker's friction message ("branch
guard says this implementation branch must open a branch-local attempt before
proof writes") and widened into a first-principles question about all Tusker
bookkeeping.

---

## The question the operator asked

**Operator said:** "With the speed of agent development, is some of this logging
just adding and burning more requirements and tokens? Or is it actually helping
us? Building and testing is much cheaper now with agents — unlike human
development where it can end up being quite expensive. … For smaller tasks it
just ends up being tedious. Sometimes it's not updated with the status and then
we just keep falling behind, or it slows down other stuff. The whole goal is to
move fast — agents can read code quickly and action — as long as they have the
right guardrails."

The economic shift underneath: in human development, redoing work costs weeks,
so you document everything to avoid redoing it. In agent development, redoing
costs minutes and re-verifying costs less than reading a stale record. That
inverts the value of most record-keeping while leaving the value of gates
untouched.

---

## D1 — The test

Every process artifact must pass one of two tests or be deleted:

1. **It gates a decision** — accept, land, dispatch, review — with a mechanical
   check an agent cannot talk its way past.
2. **It preserves human judgment** — a decision and its why — which no re-run
   can regenerate.

Regenerable history (what an agent did, what a test printed, which steps ran)
fails both tests. Git already keeps history; re-running is the cheap path.

## D2 — What stays, and why

- **Acceptance contracts written before work.** The contract is what the gate
  checks against; it is how a vacuous proof (tests aimed at a package with no
  tests) gets caught.
- **The accept gate refusing uncovered or pending proof.** Human attention is
  the scarce resource; the gate is what lets the operator review at wave
  boundaries instead of babysitting.
- **Single-writer durable state on the control branch.** The tracker lives in
  git; parallel branches flipping status directly would corrupt the board on
  merge. Branches carry proposals; main applies them.
- **Human decision logs** (this directory). The only unregenerable artifact.

## D3 — What gets trimmed

- Attempt/trace/replay machinery that no gate consumes is inventory, not
  safety. Deterministic re-adjudication of a recorded run loses to simply
  re-running the task.
- Progress logging, unchanged-state updates, and narrative evidence are
  explicitly worthless: no gate reads them, and every future context pays to
  skip them.
- Agent-transcribed results (running a suite, then writing "pass" into a table
  by hand) are attestation, not observation. See D5.

## D4 — Guards auto-do; they do not refuse-and-educate

A guard whose remedy contains no decision must apply the remedy itself and say
so in one line. Opening an attempt, routing a branch-side status change into a
proposal — nothing there needs a choice, so nothing there may cost a
round-trip. Refusal messages that lecture burn tokens on every dispatch
forever. One-line notices; hints name the command, not the philosophy.

## D5 — Direction: executable proof rows

Command-backed verification rows should be *executed by the gate*, not
transcribed by the worker: `accept` (and review) re-runs the command rows and
fills results itself. Fresher than any transcript, immune to hallucinated
green, and deletes a whole ceremony layer. Manual-proof rows stay: they encode
a human gate, which is a decision, not a record.

## D6 — Proportionality

Proof depth scales with risk and size. A small task's proof is the smallest
set of rows that covers its contract — one command row is a complete proof if
it covers the acceptance. A ceremony floor that makes small tasks tedious is a
defect in the process, not diligence in the agent.

## D7 — Status drift is a system defect

When statuses fall behind reality, the fix is a mechanism (derive it, auto-set
it at the transition that proves it), never more process asking agents to
remember. Anything that must be manually kept in sync will drift; design so
sync is a side effect of the work itself.

---

## Actioned in this session

- Operator skill: "Proof Economics" section added to `skills/tusker/SKILL.md`.
- Dispatched workers: `.tusker/signs.md` created carrying the principle into
  every attempt prompt; worker workflow template's command-budget section
  carries the proportionality rule.
- Refusal hints for the attempt guard and branch guard cut to one line.
- Follow-up tasks cut on the FAC epic: guards auto-do (auto-open attempt,
  auto-propose on branch) and executable proof rows (gate runs command rows).
