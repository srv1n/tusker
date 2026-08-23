---
title: "Orchestration"
subject: orchestration
part_of: overview
status: canonical
---

# Orchestration

The resident daemon polls registered projects. It can dispatch work only when
the project and automation policy allow it.

## Authority

- The task contract owns product scope.
- The CLI owns task state.
- The runtime store owns leases, runs, attempts, and heartbeats.
- The runner owns one code change in its assigned workspace.
- The review result owns the review verdict for its bound revision.

Execution visibility is not dispatch authority. A visible execution does not
grant a task claim.

## Dispatch checks

The daemon checks project enablement, task state, readiness, ownership,
dependencies, gates, runner policy, workspace policy, active leases, attempt
limits, and concurrency limits. An armed-wave policy can add wave authorization
checks.

A failed check produces a refusal or parked state. It must not appear as a
live run.

## Workspaces

The project can use a shared checkout or a worktree. `WORKFLOW.md` stores the
runner workspace policy. A worker must use the workspace that the claim
assigns.

## Interactive work

A user-opened session implements the current request itself. It must not start
another daemon or nested command-line agent. A process with
`TUSKER_ATTEMPT_ID` is a dispatched worker and must stay inside its claimed
task.

## Code sources

- `cmd/tusker/daemon.go`
- `cmd/tusker/daemon_scheduler.go`
- `cmd/tusker/run_ownership.go`
- `cmd/tusker/work_session_cmd.go`
- `cmd/tusker/workspace_manager.go`
- `.tusker/WORKFLOW.md`
- `AGENTS.md`
