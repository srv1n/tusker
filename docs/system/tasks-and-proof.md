---
title: "Tasks and proof"
subject: tasks-and-proof
part_of: overview
status: canonical
read_when: "Writing task contracts or handing work to another worker or reviewer."
skip_when: "Configuring provider adapters or investigating a daemon process."
---

# Tasks and proof

A task is a written work contract. One part is machine data. The other part
explains the work to a person.

## Task contract

A useful task has:

- one outcome;
- acceptance rows with stable IDs;
- non-goals;
- an exact check for each acceptance row;
- dependencies and gates when they exist; and
- the next owner and next action.

The exact record identifier is `tusker.task/v7`. This is a file-format name.

## State

The allowed task states are:

`idea`, `backlog`, `ready`, `review`, `rework`, `done`, `cancelled`, and
`superseded`.

The task state and readiness are different fields. Readiness can be `ready`,
blocked by a gate or dependency, waiting for review, waiting for a person,
waiting for CI, held, done, cancelled, or superseded.

The daemon can dispatch tasks in `ready` or `rework` when the readiness and
project checks also pass. `done`, `cancelled`, and `superseded` are terminal.

## Proof

Each verification row names the acceptance IDs that it covers. It also stores
the check, result, and notes. A passing command is not enough when it does not
cover an acceptance row.

Use `tusker verify add`. Do not edit lifecycle, proof status, or state revision
fields by hand.

## Dependencies and gates

A dependency points to another task. A gate points to a fact that needs a
person or an external system. The CLI projects both into readiness.

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
