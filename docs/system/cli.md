---
title: "CLI reference"
subject: cli
part_of: overview
status: canonical
read_when: "Looking up an exact command, document route, or machine-readable output."
skip_when: "You need the rationale or lifecycle rule behind a command."
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

- `tusker docs find <query>` searches the managed corpus: system pages in
  `docs/system/`, specs in `.tusker/specs/`, and decisions in
  `.tusker/specs/decisions/`.
- Search returns a bounded shortlist with `read_when` and `skip_when` guidance.
  JSON also reports `total_matches` and `truncated`; open the returned subject
  or stable path to read the full document.
- `tusker docs new --kind doc` creates a system page. `tusker docs new --kind
  spec` creates a governing spec under `.tusker/specs/`.
- `tusker docs map` rebuilds the index and graph from the same resolver. It
  includes metadata relationships, Markdown/Obsidian links, backlinks, and
  supersession edges; broken managed routes fail validation.
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
