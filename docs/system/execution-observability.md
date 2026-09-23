---
title: "Execution observability"
subject: execution-observability-system
part_of: overview
status: canonical
---

# Execution observability

An execution record shows work that an agent or provider started. Its identity
does not change.

## Graph

An execution can be a root, a managed child, or a provider-native child. The
runtime stores parent and child edges. A task binding is optional, so the
unbound inbox can show work before an operator connects it to a task.

A managed child has Tusker runtime ownership. A provider-native child remains
owned by the provider. Tusker records the relationship and does not invent a
second task.

## Timeline

The timeline combines stored execution events with provider observations.
Each source has its own cursor. A `stale_cursor` result tells the client to
fetch again. Hooks, JSON streams, and cloud status are observations. They do
not prove task completion.

Run detail defaults to **Messages & tools**, showing the latest 50 captured
worker messages and tool observations with auto-follow. **All events** also
shows the recent protocol diagnostics. Active runs refresh every two seconds;
heartbeat freshness remains separate from work progress. ACP public message
chunks are retained as bounded, redacted snapshots and combined into readable
messages; tool titles, status, and text output are captured without persisting
arbitrary provider metadata or reasoning. Codex and Claude CLI messages and
tool activity are read from the attempt's existing structured raw output.
Readers tail the end of each file within a 1 MiB window and cap individual
messages at 16K characters. Missing timestamps remain unavailable. Historical
ACP attempts that discarded message payloads cannot be reconstructed from
Tusker's event ledger; their UI explicitly reports missing capture.

Run detail shows the current attempt, separate heartbeat/message/tool times,
and the recorded checkpoint or unresolved operation. Reconnect refreshes that
record without launching a worker. Native Continue queues a new attempt on the
same session only when the adapter and current material support it. Explicit
context recovery queues a fresh session and retains the prior attempt history;
unknown effects require operator resolution before replay. Per-run Pause is
unavailable without provider acknowledgement. Stop persists intent and waits
for exact-owner settlement before retry or Start fresh can proceed. Wave Pause
continues to govern admission only.

Codex Cloud and Claude Code use different provider readers. An authoritative
fetch reads the provider before a bind, rename, or cancel action that depends
on current provider state.

## Actions

The CLI and Serve API can register, bind, rename, and cancel an execution. A
bind checks project and task identity. Cancellation records the local request
and provider settlement without changing task claim authority.

## Doctor

`tusker doctor <TASK-ID|WAVE-ID> [--json] [--output <new-path>]` diagnoses
one task or wave across contracts, the dependency graph, authorization,
queued reservations, capacity, proof, review, and daemon freshness. It is
read-only: nothing is queued, claimed, spawned, or signaled. Human output
leads with the first actionable cause and the exact permitted next action;
`--json` retains the complete versioned diagnosis. `--output` writes the
same bounded JSON to a new file for a product-defect report and refuses to
overwrite an existing file. The command exits 0 for healthy, complete, and
normal waits, 1 for actionable faults or decisions, and 2 for unavailable
or invalid diagnoses. A member holding its own current-authorization queued
reservation is released from canonical backlog authoring instead of blocked
by it; normal dependency and capacity waits name their owner and next check
without suggesting repair.

Play, project enable/disable, task acceptance, freed dispatch capacity, and
daemon restart all wake targeted reconciliation; a dropped wake is recovered
by the periodic poll, which stays the fallback rather than a second trigger
path. Duplicate wakes coalesce, and at concurrency one the release of a slot
admits the next eligible attempt without starving a continuously eligible
wave. Paused, inert, project-off, gated, and uncertain work stays waiting
with its owner and is never dispatched by a wake.

Automatic repair is bounded: a recoverable metadata inconsistency (for
example a dispatch reservation proven to have no live or uncertain owner)
gets at most one automatic repair per unchanged diagnostic fingerprint, the
attempt ledger survives restart, and a failed postcondition or recurrence
escalates once with evidence. Materially new state is a new diagnosis.
Repairs reuse the guarded primitives under still-valid authority and never
arm inert work, enable a project, widen scope, change models or budgets,
forge proof, approve a human gate, retry an uncertain external effect, or
hand-edit runtime state.

Each project persists its reconcile schedule and last observation (tier,
cadence, next due, last activity and reason, last poll). Overdue
reconciliation exposes its next actor and action instead of a healthy
daemon; a missing schedule reads unavailable, never healthy. Wave
list/detail and task payloads carry the same `recovery` diagnosis the doctor
reports — authorization, queued state, blocking cause and code, next
actor/action, schedule, and repair escalations — so CLI and UI agree.
Capability flags state what the server supports; there is no manual repair
mutation, so repair states render read-only. Background-work Settings shows
the same resume scope and toggle audit the
`projects automation-scope` endpoint returns, and reading it arms nothing.

## Code sources

- `cmd/tusker/execution_ledger.go`
- `cmd/tusker/execution_graph.go`
- `cmd/tusker/provider_execution_events.go`
- `cmd/tusker/serve_execution_graph.go`
- `cmd/tusker/serve_execution_timeline.go`
- `cmd/tusker/execution_diagnostics.go` — versioned diagnostic envelope and typed recovery descriptors
- `cmd/tusker/execution_doctor.go` — read-only task and wave doctor
- `cmd/tusker/self_service_admission.go` — shared stage-specific admission facts, including own-reservation promotion
- `cmd/tusker/adaptive_reconcile.go` — targeted wake, bounded automatic safe repair, persisted reconcile schedules
