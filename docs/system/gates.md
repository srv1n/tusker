---
title: "Gates"
subject: gates
part_of: overview
status: canonical
---

# Gates

A gate records one blocking fact. A person or an external system must supply
that fact.

## Required fields

A gate names:

- its owner;
- the tasks that it blocks;
- the acceptance rows that it covers, when applicable;
- why an agent cannot do the action;
- the action; and
- the check that proves completion.

The gate format identifier is `tusker.gate/v1`. The allowed states are `open`,
`satisfied`, `waived`, and `obsolete`.

## Use a gate for

- a credential or environment fact;
- a human product decision;
- legal, privacy, security, billing, or release authority;
- an external service action; or
- subjective acceptance that a command cannot prove.

Do not use a gate for a missing task description, a normal test, or risk by
itself.

## Resolve a gate

Use `tusker gate satisfy`, `tusker gate waive`, or `tusker gate obsolete`.
Satisfaction needs the named evidence. A waiver needs an actor and a reason.
The CLI recalculates affected task readiness.

## Code sources

- `internal/v7schema/schema.go`
- `cmd/tusker/commands_v7.go`
- `cmd/tusker/v7_validation.go`
- `cmd/tusker/v7_proof_cmd.go`
- `skills/tusker/assets/templates/gate.md`
