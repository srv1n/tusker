---
title: "Orchestration"
subject: orchestration
part_of: overview
status: canonical
describes: [cmd/tusker/daemon.go, cmd/tusker/daemon_scheduler.go, cmd/tusker/workspace_manager.go, cmd/tusker/work_session_cmd.go, cmd/tusker/run_ownership.go, cmd/tusker/process_inventory.go, cmd/tusker/process_janitor.go]
read_when: "Understanding scheduling, workspace preparation, or worker ownership."
skip_when: "Authoring acceptance criteria or inspecting visual evidence."
---
<!-- docs-check: ignore-path .claude/settings.json -->

# Orchestration

For an explicitly authorized coordinator campaign, use
[Coordinator batches](coordinator-batches.md): a foreground runner with durable
recovery, reports and a single end-of-batch digest.

CLI mutations notify the resident daemon immediately. Notifications coalesce
per project, and changes arriving during a poll remain queued for a following
poll. The daemon can dispatch work only when the project and automation policy
allow it.

## Updates and reconciliation

The normal status path is CLI write → project notification → daemon/Serve
refresh → client event. Active runs retain lightweight health checks, while
warm polls reuse the project frontier and refresh active task records. A full
reconciliation runs at startup and periodically (five minutes by default) to
recover missed notifications or edits made outside the CLI. Explicit refreshes
and mutation/dispatch authorization still read current material.

Read operations share note indexes and request-local wave material instead of
walking the vault for each row. Runner log ingestion advances over complete new
records and replays safely after replacement, truncation, or restart. File
hashes stream through a fixed buffer. Log tails share a request byte budget,
and runner output limits prevent truncated authoritative output from being
accepted as a successful result.

The web UI suspends refresh work while hidden and refreshes active queries on
return. The macOS summary has one request in flight with a coalesced follow-up;
Spotlight updates changed records and removes deleted records. Native panel
visibility is sent explicitly to the web UI.

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

Worktree and clone preparation must succeed in Git before the workspace is
reported as prepared. A failed Git command returns its error and preserves
preexisting files at the destination.

Orphan cleanup checks active runtime ownership as well as the preparer's PID.
A detached worker can outlive the daemon that prepared its workspace. If the
runtime store is missing or unreadable, the copy stays counted toward the cap
until ownership can be established.

## Process containment

Tusker records every process it starts in the runtime store's process
inventory: owner, kind, PID, process group, start time, starter, and last sign
of life. Only agent sessions may outlive the daemon. Checks, such as landing
gates and closeout commands, run in their own process group and end with their
starter.

- A check is stopped as a group when it finishes, when its starter cancels or
  shuts down, or when it has printed nothing and used no CPU for the check
  silence limit. That outcome is recorded as stuck, stopped, not as a failure.
- One janitor pass runs at daemon startup and on every poll. It stops checks
  whose starter died, adopts agent sessions whose run is still claimed or
  running, and stops sessions whose run is gone. A session whose attempt finished
  less than two minutes ago is left to exit on its own. It acts only when PID, process
  group, and start time all match; otherwise it reports and leaves the process
  alone. Every action writes a process event.
- Landing gates take one machine-wide slot. Before a gate runs, a copy left by
  a dead starter is stopped, so a restart never runs the same gate twice.
- The watchdog stops its own daemon's checks before it exits. Every restart
  counts toward the crash-loop breaker; while it is open, landing pauses and
  the run shows why.
- An agent session that shows no sign of life past the agent silence flag is
  marked silent. Containment never stops an agent session for running long or
  for being silent.

The full rules are in the `process-containment` spec.

## Interactive work

A user-opened session implements the current request itself. It must not start
another daemon or nested command-line agent. A process with
`TUSKER_ATTEMPT_ID` is a dispatched worker and must stay inside its claimed
task.

### Interactive architect inbox

An operator can register an existing Claude Code or Codex conversation as a
wave or task architect contact. The registered binding reports `inbox`:
questions and wave reports are retrieved by a hook at that session's next
prompt or turn end. It cannot push a message into an active turn. Devin
external contacts remain unsupported. Registration alone does not install a
hook; the operator adds the following to their own Claude Code settings
(`~/.claude/settings.json` or project `.claude/settings.json`).
`tusker message hook print --harness claude --project <project-id>` prints
this block with the running binary's absolute path already filled in; it
never writes a file.

If the daemon uses a custom `TUSKER_STATE_ROOT`, prefix each hook command with
`TUSKER_STATE_ROOT=/absolute/path/to/state` to read the same store (the print
command adds that prefix itself when the variable is set).

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/absolute/path/to/tusker message inbox --project <project-id> --format hook"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "/absolute/path/to/tusker message inbox --project <project-id> --format hook"}]}]
  }
}
```

Both hooks pass their `session_id` on stdin. Tusker injects pending messages
with distinct IDs; a `Stop` hook blocks once when it has new messages, then
returns no output on the next stop. With no matching registration, it injects
nothing. Keep the hook's project ID aligned with the registration. If the
session is replaced, install or retain the hook in the replacement session;
only its ID receives subsequent inbox output. A new Claude session ID, including
one created by `/clear`, needs `tusker execution register` again.

**Codex hook integration is unverified in the installed Codex; qualify it
before relying on it.** The published Codex hook schema also carries
`session_id` and supports `UserPromptSubmit` and `Stop`.
`tusker message hook print --harness codex --project <project-id>` prints the
proposed operator owned `hooks.json` entry, flagged unverified:

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/absolute/path/to/tusker message inbox --project <project-id> --format hook"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "/absolute/path/to/tusker message inbox --project <project-id> --format hook"}]}]
  }
}
```

Tusker never writes either settings file. The hook output is context for the
architect, not authority to mutate the wave. A continuation proposal still
requires an operator to invoke `ApplyArchitectContinuation`.

Hook schema: [Claude Code](https://code.claude.com/docs/en/hooks).

## Code sources

- `cmd/tusker/daemon.go`
- `cmd/tusker/daemon_scheduler.go`
- `cmd/tusker/run_ownership.go`
- `cmd/tusker/work_session_cmd.go`
- `cmd/tusker/workspace_manager.go`
- `.tusker/WORKFLOW.md`
- `AGENTS.md`
