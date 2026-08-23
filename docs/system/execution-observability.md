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

Codex Cloud and Claude Code use different provider readers. An authoritative
fetch reads the provider before a bind, rename, or cancel action that depends
on current provider state.

## Actions

The CLI and Serve API can register, bind, rename, and cancel an execution. A
bind checks project and task identity. Cancellation records the local request
and provider settlement without changing task claim authority.

## Code sources

- `cmd/tusker/execution_ledger.go`
- `cmd/tusker/execution_graph.go`
- `cmd/tusker/provider_execution_events.go`
- `cmd/tusker/serve_execution_graph.go`
- `cmd/tusker/serve_execution_timeline.go`
