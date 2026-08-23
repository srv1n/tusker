---
title: "CLI reference"
subject: cli
part_of: overview
status: canonical
---

# CLI reference

The installed program help is the command authority. It shows the commands
that this build can run. A machine-readable command list is also available.

## Common work commands

| Need | Command |
| --- | --- |
| Find runnable work | `tusker next` |
| Read one task | `tusker show <TASK-ID> --capsule` |
| List work | `tusker list` |
| Search tracker text | `tusker search <text>` |
| Create a task | `tusker new task --vault ./.tusker --epic APP --title "..."` |
| Change task state | `tusker status <TASK-ID> <STATE> --reason "..."` |
| Add a check result | `tusker verify add <TASK-ID> ...` |
| Submit review | `tusker review submit <TASK-ID> ...` |
| Close a task | `tusker close <TASK-ID>` |
| Check the tracker | `tusker validate --vault ./.tusker --json` |

## Project and runtime commands

| Need | Command |
| --- | --- |
| Add a project | `tusker projects add --repo . --vault ./.tusker` |
| List projects | `tusker projects list --json` |
| Enable automation | `tusker projects enable --id <PROJECT-ID>` |
| Check the daemon | `tusker daemon status --json` |
| Check one run | `tusker runs inspect <RUN-ID> --json` |
| Start the local service | `tusker serve` |

## Executions

`tusker execution register` records a direct execution.
`tusker execution inbox` lists unbound executions. Use `execution show`,
`execution bind`, `execution rename`, and `execution cancel` for one execution.
These commands do not grant a task claim.

## Documentation and skills

- `tusker docs find <query>` searches the system pages.
- `tusker docs map` rebuilds the index and graph.
- `tusker docs status --json` reports freshness.
- `tusker skill doctor --strict --json` checks the skill routes.

## Output contract

Use `--json` for scripts. A command error returns a non-zero exit code and a
typed error. Normal output is for a person and can change its layout.

## Code sources

- `cmd/tusker/cli.go`
- `cmd/tusker/capabilities_cmd.go`
- `cmd/tusker/commands_*.go`
- `cmd/tusker/install.go`
