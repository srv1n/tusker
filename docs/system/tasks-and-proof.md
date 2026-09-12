---
title: "Tasks and proof"
subject: tasks-and-proof
part_of: overview
status: canonical
read_when: "Writing task contracts or handing work to another worker or reviewer."
skip_when: "Configuring provider adapters or investigating a daemon process."
---

# Tasks and proof

A task tells a worker what result to produce and tells a reviewer how to check
it. Creating or importing a task prepares work. It does not start work.

## Write a task that can be checked

A useful task has:

- one outcome, its current problem, and a concrete before/after example;
- acceptance rows with stable IDs;
- non-goals;
- an exact check for each acceptance row;
- dependencies and gates when they exist; and
- the next owner and next action.

The planner also supplies verified entry points and exact governing sections,
locked decisions, surrounding tasks and their interface/ownership boundaries,
and architect/origin/peer contacts with a supported reply route or explicit
fallback. The worker's configured work level changes who implements the task;
it does not reduce the handoff's completeness. See the
[task authoring procedure](../../skills/tusker/references/TRACK.md) for the
required content and packet-based readiness review.

Existing structural checks catch placeholder acceptance, missing proof mappings
and malformed verification. They do not establish semantic completeness. Before
handoff, the planner reviews the generated packet and exact references without
relying on chat history, records gaps, and resolves essential design or ownership
questions. No word-count minimum or additional human approval gate is required.

Contact metadata uses the existing architect, origin and named peer addresses.
An address must resolve to the intended task/execution; a native thread ID alone
does not establish a Tusker route. Verify the installed messaging capability
before prescribing a reply command, and keep live delivery evidence separate
from an authored contact or receipt.

The exact record identifier is `tusker.task/v7`. This is a file-format name.

Add command checks as pending work:

```sh
tusker verify add APP-T-0001 \
  --covers A1 \
  --check "command: go test ./cmd/tusker -run TestFocused -count=1" \
  --result pending
```

Expected result: the task contains a pending row linked to `A1`. The command
does not run while the row is added. A public `verify add` call refuses
`--result pass` or `--result fail`; only the verification executor records a
command result. If the row is wrong, remove or replace it while the work
session has authority. Do not edit proof fields by hand.

## Start work only when it is ready

The allowed task states are:

`idea`, `backlog`, `ready`, `review`, `rework`, `done`, `cancelled`, and
`superseded`.

The task state and readiness are different fields. Readiness can be `ready`,
blocked by a gate or dependency, waiting for review, waiting for a person,
waiting for CI, held, done, cancelled, or superseded.

The daemon can dispatch tasks in `ready` or `rework` when the readiness and
project checks also pass. `done`, `cancelled`, and `superseded` are terminal.

For user-directed work, start an interactive work session:

```sh
tusker work start APP-T-0001 --by agent:worker
```

Expected result: Tusker claims one execution session and returns its packet and
next action. This command does not enable automation, arm a wave, start a
daemon, or launch another worker.

If the task is held, blocked, terminal, already owned, or overlaps another
task's owned paths, `work start` refuses the claim and reports the blocker.
Follow the reported remedy. Do not force a claim or edit readiness by hand.

To inspect an existing session, use:

```sh
tusker work status APP-T-0001 --json
tusker work wait APP-T-0001 --timeout 60 --json
```

`work status` reports the current owner and state. `work wait` waits for a
state change or the timeout; a timeout is not completion.

## Submit the result for review

After the implementation and its checks are complete, submit the session with
the result, verification summary, and one verdict for each acceptance row:

```sh
tusker work submit APP-T-0001 \
  --by agent:worker \
  --deliverable "Implemented the accepted task scope." \
  --verification "Focused checks passed." \
  --gate-verdicts "A1=pass"
```

Expected result: Tusker releases the execution session and moves the task to
review. Submission is not review, acceptance, landing, or completion. If proof
or acceptance coverage is incomplete, submission refuses and names the gaps.
Record the missing proof or fix the result, then submit again.

## Proof

Tusker-owned transient task artifacts expire seven days after terminal completion. Close and discard do not delete them immediately. Active or reopened work, human waits, unknown ownership, and `tusker gc --keep <TASK-ID>` are protected; `--unkeep` returns the task to its existing terminal-time window. Expiry removes eligible bytes but retains the task/evidence result and an `artifacts_expired_at` receipt.

Worker `PLAN.md` scratch notes are optional. A missing note is never created merely to start or resume work; packets, claim/workspace identity, structured outcomes, blockers, and evidence pointers carry the required resume state.

Each verification row names the acceptance IDs it covers and stores the check,
result, and notes. A passing command does not prove an acceptance row that the
row does not cover. See [Proof and closeout](proof-and-closeout.md) for command
execution, review validity, artifact evidence, and completion.

## Dependencies and gates

A dependency points to another task. A gate points to a fact that needs a
person or an external system. The CLI projects both into readiness.

Human-owned gates (`owner: human:<name>`) resolve only through a native
signed human receipt issued in the UI; `gate satisfy` and `gate waive` refuse
without one, and there is no CLI bypass. The repeatable demo
(`tusker demo seed --with-human-gate`) exercises this: the gated task cannot
even move to ready until the owning human releases it.

Delivery plans can declare cross-scope dependencies (`task` plus producer
`scope`, hard only). The demo follow-up wave uses them to join two
independently imported waves, and readiness reports the real blockers.

## Read one task

Use `tusker show <TASK-ID> --capsule`. Use a full task file only when the
contract or a repair needs it. Do not read all events or attempts for normal
work.

Worker and reviewer packets preserve the complete task body, including
non-goals, verification commands, and artifact requirements. They also include
declared owned paths, generated outputs, migration keys, and shared resources.
Delivery import carries plan non-goals into each task.

## Code sources

- `internal/v7schema/schema.go`
- `cmd/tusker/commands_v7.go`
- `cmd/tusker/v7_control_cmd.go`
- `cmd/tusker/v7_proof_cmd.go`
- `.tusker/WORKFLOW.md`
