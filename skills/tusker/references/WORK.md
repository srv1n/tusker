# Work

Use this guide for user-directed implementation, dispatched implementation or
review, proof, gates, lifecycle handoff, and human wait.

## Interactive work

A directly opened Codex or Claude session implements the current request with
its own tools. Inspect the task and routed project knowledge:

```bash
tusker show <TASK-ID> --capsule
tusker show <TASK-ID> --acceptance
tusker work start <TASK-ID> --by agent:<name> --source codex
```

Use the returned workspace, branch, owner, and revision exactly. This works
while project automation is disabled and does not require a daemon or armed
wave. Interactive admission checks ready/rework state, dependencies, genuine
human gates, ownership, revision, owned-path conflicts, and workspace safety.
A refusal is coordination data; do not edit around it.

Implement the approved contract without asking again about decisions already
made by the task, acceptance criteria, spec, or decision record. Finish with
the same owner and revision:

```bash
tusker work heartbeat <TASK-ID> --owner agent:<name> --revision <REV>
tusker work submit <TASK-ID> --owner agent:<name> --revision <REV>
tusker work fail <TASK-ID> --owner agent:<name> --revision <REV> --reason <WHY>
tusker work release <TASK-ID> --owner agent:<name> --revision <REV>
```

## Dispatched worker

`TUSKER_ATTEMPT_ID` means the daemon atomically claims the task before it
creates the worker process. Verify the injected task, attempt, workspace, and
packet; it must not claim again:

```bash
tusker packet <TASK-ID> --for agent
tusker runs inspect <TASK-ID> --json
```

The runner harness owns session attachment, heartbeats, process monitoring,
and normalized runtime outcomes. The worker owns only implementation and the
smallest acceptance-mapped proof. It never merges, lands, closes, moves refs,
or schedules successors. Abrupt termination is resolved by heartbeat expiry
and safe reclaim; never forge a terminal result. Never write `active` or
`in_progress` into task frontmatter.

## Dispatched reviewer

Review is immutable: inspect the injected task/source/work revision, proof,
gates, and acceptance, then submit exactly one attempt-bound result:

```bash
tusker review submit <TASK-ID> \
  --attempt <ATTEMPT-ID> --task-rev <TASK-REV> \
  --source-sha <SHA> --work-rev <WORK-REV> \
  --proof-fingerprint <PROOF-FP> --gate-fingerprint <GATE-FP> \
  --verdict pass|changes_requested|blocked \
  --covers <ACCEPTANCE-IDS> --summary "<BOUNDED-SUMMARY>"
```

Add one `--finding` per actionable change. `blocked` requires
`--blocker machine|infrastructure|human`; human requires a real open
human-owned gate. Reviewer prose or exit status is not acceptance. The
deterministic control plane consumes the typed result and owns merge, landing,
closure, and successor wake.

## Proof and lifecycle

Durable task flow is `idea -> backlog -> ready -> review -> done`; `claimed`,
`running`, `leased`, and `interrupted` are runtime states. Proof is the
smallest verification set covering acceptance:

```bash
tusker verify add <TASK-ID> --covers A1,A2 --check "<COMMAND>" --result pass
tusker proof status <TASK-ID>
tusker finish <TASK-ID> --summary "<RESULT>" --request-review
```

Do not add progress logs, unchanged-state updates, transcripts, or raw output.
Store noisy artifacts under `.tusker/scratch/<TASK-ID>/` and promote only
evidence a gate consumes.

## Human Approval Boundary

Human gates are for credentials/account authority, security/privacy/legal or
destructive production authority, unresolved product intent, or final human
acceptance of screenshots, recordings, UX feel, brand quality, and other
contractually subjective artifacts.

Everything already decided by the task, acceptance criteria, governing spec,
or linked decision is approved. Agents own objective code, diff, test, log,
screenshot, recording, and artifact review. Risk changes proof depth, reviewer
strength, and landing safeguards; risk alone does not justify a human gate.
Independent reviewers may objectively accept every risk tier through a typed
result.

Before creating a gate, name the missing human fact/authority/judgment, why no
agent can supply it, where the contract leaves it unresolved, and the exact
action/evidence that clears it. If the contract already decided it, continue.

Human-only review becomes `readiness: waiting_on_human` with
`agent_action: stop_until_human_response`. Final Response Shape For Human Wait:
state what is blocked, the exact human action, and the task/gate ID to resume.
Do not claim completion.
