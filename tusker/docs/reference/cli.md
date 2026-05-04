---
schema: "tusker.doc/v5"
id: "reference/cli"
title: "Tusker v5 CLI surface"
type: "doc"
node: "reference/cli"
audience: "developer"
mode: "reference"
agent_layer: "capsule"
kind: "reference"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "reference/cli"
publish_description: "Tusker v5 CLI surface."
created: "2026-04-29"
updated: "2026-05-04"
---

# Tusker v5 CLI surface

## Shape

The public CLI is small on purpose:

| Area | Commands |
|---|---|
| Setup | `init`, `update` |
| Work items | `new`, `list`, `status`, `evidence`, `verify`, `close` |
| Docs | `docs model`, `docs map`, `docs catalog`, `docs freshness`, `docs check`, `docs apply`, `docs noop`, `docs waive`, `docs export`, `docs dev`, `docs build` |
| Shared vaults | `vault set`, `vault status`, `vault mount`, `vault unmount`, `vault repair`, `vault move` |
| Operator runtime | `daemon`, `projects`, `runs`, `refresh` |
| Health | `validate`, `reindex` |

## Operator/runtime commands

The runtime commands are shipped as operator/internal controls. They support the local Codex pickup loop while keeping task truth in markdown.

| Command | Behavior |
|---|---|
| `tusker projects add --repo . --vault ./tusker` | Register a repo-local vault for daemon polling. |
| `tusker projects list [--json]` | Show registered projects and health. |
| `tusker daemon status [--json]` | Show state root, project count, and active run count. |
| `tusker daemon run [--once]` | Run the daemon loop, or a single poll tick. |
| `tusker refresh [--quiet] [--json]` | Run one poll tick; best smoke command. |
| `tusker runs inspect <TASK-ID> [--json]` | Inspect run, attempts, turns, sessions, latest event, decisions, token totals, and artifact paths. |
| `tusker runs events <TASK-ID> [--lines <n>] [--json]` | Tail normalized runtime events. |
| `tusker runs logs <TASK-ID> [--lines <n>] [--json]` | Tail raw runner logs. |
| `tusker runs interrupt <TASK-ID> [--json]` | Interrupt a live run. |

These stay internal/operator commands. The public workflow still leads with task commands, not daemon mechanics.

## Existing repo migration

```bash
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
```

`--migrate-v5` converts old stories and bugs into tasks, updates IDs and wikilinks, renames epic index files, installs V5 templates/views, and adds missing docs-map nodes for published docs.

## Repo-local skill refresh

```bash
tusker update --repo . --repo-only --no-bin
```

Use this after pulling or rebuilding Tusker when the repository should carry the current agent skill bundle under `.agents/skills/tusker` and `.claude/skills/tusker`.

## Docs pipeline

```bash
tusker docs map --json
tusker docs freshness --stale
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site
```

The site output is generated. Author source docs in `tusker/docs/**` or registered repo docs, not in `site/src/content/docs/**`.

## Codex-only orchestration status

`WORKFLOW.md` currently names `codex` as the default runner and `codex app-server` as the Codex command. The tracker contract says only `active` and `rework` are runnable; `ready` is not dispatched.

The local Obsidian-to-review loop is now testable through the operator commands: register the project, move a task to `active` or `rework`, run `refresh` or `daemon run`, and inspect the run. Codex must still move the task to `review` when it is ready; a clean exit that leaves the task `active`/`rework` queues continuation rather than pretending the work is reviewable.

## Shared Obsidian vault

```bash
tusker vault set --path /path/to/shared-obsidian-vault
tusker vault mount --repo /path/to/repo --vault /path/to/repo/tusker --name repo-name
tusker vault status
```

`vault mount` creates a symlink at `<shared-vault>/<name>` that points to the repo-local Tusker tracker. Use this when one Obsidian workspace should monitor multiple project trackers.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | user input or command error |
| 2 | validation failure |
| 3 | filesystem or I/O failure |
