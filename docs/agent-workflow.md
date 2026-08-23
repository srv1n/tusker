# Agent workflow

This repository has two execution modes.

## Interactive work

A session that a user opens does the requested work itself.

1. Read the task contract and routed project canon.
2. Check the current workspace and ownership.
3. Make the smallest complete change.
4. Record exact proof.
5. Submit the work for review.

Never start `tusker daemon run`, dispatch automation, or start a nested
command-line agent from an interactive session.

## Automated work

Only a resident daemon can dispatch background work. Registration does not
enable automation. `tusker automation plan` is read-only.

A process with `TUSKER_ATTEMPT_ID` works only on its claimed task, attempt, and
workspace. It does not start another daemon or runner.

## Refusals

Keep a typed refusal visible. Do not turn a dependency, gate, owner conflict,
workspace conflict, disabled project, or daemon failure into a generic waiting
state.

## Proof

Map each check to acceptance row IDs. State the command and the result. Keep
large logs in task scratch. Do not treat a packet, process exit, or model claim
as proof.

## Sources

- `AGENTS.md`
- `CLAUDE.md`
- `.tusker/WORKFLOW.md`
- `cmd/tusker/daemon.go`
- `cmd/tusker/run_ownership.go`
- `cmd/tusker/work_session_cmd.go`
