---
title: "Orchestration"
subject: orchestration
part_of: overview
status: canonical
read_when: "Understanding scheduling, workspace preparation, or worker ownership."
skip_when: "Authoring acceptance criteria or inspecting visual evidence."
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

Worktree and clone preparation must succeed in Git before the workspace is
reported as prepared. A failed Git command returns its error and preserves
preexisting files at the destination.

Orphan cleanup checks active runtime ownership as well as the preparer's PID.
A detached worker can outlive the daemon that prepared its workspace. If the
runtime store is missing or unreadable, the copy stays counted toward the cap
until ownership can be established.

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
(`~/.claude/settings.json` or project `.claude/settings.json`):

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "tusker message inbox --project <project-id> --format hook"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "tusker message inbox --project <project-id> --format hook"}]}]
  }
}
```

Both hooks pass their `session_id` on stdin. Tusker injects pending messages
with distinct IDs; a `Stop` hook blocks once when it has new messages, then
returns no output on the next stop. With no matching registration, it injects
nothing. Keep the hook's project ID aligned with the registration. If the
session is replaced, install or retain the hook in the replacement session;
only its ID receives subsequent inbox output.

**Codex hook integration is unverified in the installed Codex; qualify it
before relying on it.** The published Codex hook schema also carries
`session_id` and supports `UserPromptSubmit` and `Stop`. The proposed operator
owned `hooks.json` entry is:

```json
{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "tusker message inbox --project <project-id> --format hook"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "tusker message inbox --project <project-id> --format hook"}]}]
  }
}
```

Tusker never writes either settings file. The hook output is context for the
architect, not authority to mutate the wave. A continuation proposal still
requires an operator to invoke `ApplyArchitectContinuation`.

Hook schemas: [Claude Code](https://code.claude.com/docs/en/hooks) and
[Codex](https://learn.chatgpt.com/docs/hooks).

## Code sources

- `cmd/tusker/daemon.go`
- `cmd/tusker/daemon_scheduler.go`
- `cmd/tusker/run_ownership.go`
- `cmd/tusker/work_session_cmd.go`
- `cmd/tusker/workspace_manager.go`
- `.tusker/WORKFLOW.md`
- `AGENTS.md`
