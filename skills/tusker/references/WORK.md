# Work

Use this guide for task status, proof, gates, review state, and closeout.

## Inspect first

```bash
tusker show <TASK-ID> --capsule
tusker show <TASK-ID> --acceptance
tusker proof status <TASK-ID>
```

The task describes tracked intent; it does not grant wider authority. Perform
source work only when the user and repository rules already authorize it.
Tusker records the work and must not dictate unrelated repository operations.

## Record lifecycle through the CLI

Durable task flow is `idea -> backlog -> ready -> review -> done`. Runtime
activity is not a durable task status.

```bash
tusker status <TASK-ID> ready --reason "Contract is actionable."
tusker verify add <TASK-ID> --covers A1,A2 --check "<CHECK>" --result pass
tusker status <TASK-ID> review --reason "Acceptance proof is recorded."
tusker close <TASK-ID>
```

Use `tusker discard` for cancellation and `tusker status <TASK-ID> rework` for
actionable changes. Never write lifecycle fields directly into task markdown.

Proof is the smallest verification set covering acceptance. Record command
plus PASS/FAIL and a bounded note; do not add progress logs, transcripts, or
raw output. Store noisy artifacts under `.tusker/scratch/<TASK-ID>/` and
promote only evidence that acceptance or a gate consumes.

## Gates

Create a gate only when a named human fact, authority, credential, unresolved
decision, or subjective judgment is missing:

```bash
tusker new gate --vault ./.tusker --blocks <TASK-ID> --kind <KIND> \
  --owner human:<name> --action "<ACTION>" --verification "<PROOF>"
```

Everything already decided by the task, acceptance criteria, governing spec,
or linked decision is not a new human gate. Agents may assess objective proof;
final human acceptance remains appropriate for UX feel, brand quality, legal
authority, credentials, and other explicitly subjective or privileged facts.

## Human wait

If the task reports `readiness: waiting_on_human` or
`agent_action: stop_until_human_response`, do not mutate Tusker or repeat
validation. Report what is blocked, the exact human action, and the task/gate
ID needed to resume. Do not claim completion.

If a Tusker command fails, preserve the source-work result, report the tracker
failure separately, and do not repair tracker internals unless the user asks.
