# Spec 03: Global Daemon and Project Registry

Status: Draft

## Decision

Tusker v2 uses one host-local daemon plus a single SQLite runtime store.

The runtime store is:

- file: `daemon.db`
- engine: SQLite
- mode: WAL
- owner: the daemon

This decision is not optional. It is the seam that keeps the tracker from becoming a fake process database.

SQLite is mandatory only when orchestration is enabled. Tracker-only mode has no runtime database.

## Why SQLite

| Option | Verdict | Why |
|---|---|---|
| SQLite | pick this | transactions, inspectable, migration-friendly, easy queries across projects/runs |
| BoltDB | no | fine technically, worse operator ergonomics and ad hoc querying |
| JSON sidecars | absolutely not | no transactions, miserable concurrent mutation semantics |

## Goals

- one install, many tracked projects
- per-project runtime policy via `WORKFLOW.md`
- continuous monitoring, dispatch, retry, and review awareness
- local control surface for the CLI
- explicit source-of-truth boundaries

## Non-Goals

- multi-user remote control plane
- rich web UI as a requirement
- treating markdown as the source of truth for in-flight runs

## Process Model

Primary daemon command:

```text
tusker daemon run
```

Recommended deployment:

- macOS: `launchd`
- Linux: `systemd --user`

Single-user, host-local only.

## Local State Root

Default root:

- macOS: `~/Library/Application Support/tusker`
- Linux: `${XDG_STATE_HOME:-~/.local/state}/tusker`

Layout:

```text
<state-root>/
├── daemon.db
├── daemon.sock
├── daemon.log
├── runs/
└── workspaces/
```

`daemon.db` is created lazily on first daemon/project-registration use. It is not committed to the repo and it is not a user-managed project artifact.

## PM Summary

If you need the non-engineer version:

> SQLite is the daemon's crash-proof local memory.

It remembers:

- which jobs are currently leased
- which attempt belongs to which task
- which runner session can be resumed
- which retries are due later
- what the daemon should show in `tusker runs` after a restart

It does not store:

- epics
- stories
- specs
- acceptance criteria
- evidence
- review content humans read in Obsidian

That split is the whole point.

## Source-Of-Truth Boundary

This is load-bearing:

- the daemon **observes** markdown
- the daemon **owns** runtime state in SQLite
- the daemon writes the vault only for durable tracker transitions

It does **not** use markdown as its in-flight truth for:

- leases
- session ids
- heartbeats
- retry timers
- active attempts

If the daemon starts racing Obsidian on those fields, the design is already broken.

## State Ownership

| State class | Canonical owner |
|---|---|
| task intent, durable status, review metadata, evidence | vault markdown |
| project registration | `daemon.db` |
| leases, attempts, turns, sessions, retry queue, heartbeats, failure classes | `daemon.db` |
| raw runner logs and event streams | `<state-root>/runs/` |
| isolated code copies | `<state-root>/workspaces/` |

## Writer Semantics

When the daemon must update a note:

1. read current note
2. verify current durable state still justifies the write
3. apply optimistic concurrency check
4. write only the narrow durable fields from Spec 01
5. on conflict, reload and re-evaluate

Daemon note writes are limited to durable transitions such as:

- `active -> in_review`
- `in_review -> rework`
- `merging -> done`
- review metadata updates

The daemon never writes live lease state into notes.

## Why This Matters For Restarts

Without a runtime store, a daemon restart loses:

- active lease state
- retry timers
- session references
- last known heartbeat

Then the daemon is forced to guess from logs, note files, and process inspection.

With SQLite, restart recovery becomes deterministic:

1. load `daemon.db`
2. find active or queued runtime entries
3. ask the runner adapter whether sessions can be resumed
4. resume, retry, abandon, or release cleanly

That is why SQLite exists here.

## Project Registration

Each registration binds:

- repo root
- vault root
- workflow path
- project identity

### SQLite tables

Minimum tables:

| Table | Purpose |
|---|---|
| `projects` | registered projects |
| `leases` | current orchestration claim state per record |
| `attempts` | execution attempts |
| `turns` | per-Codex-turn execution records inside attempts |
| `sessions` | runner session refs and heartbeats |
| `retry_queue` | pending retry schedule |
| `operator_events` | concise audit/debug events |

### `projects` columns

| Column | Meaning |
|---|---|
| `project_id` | immutable ULID/UUID |
| `project_key` | stable path-safe key |
| `name` | display name |
| `repo_root` | absolute path |
| `vault_root` | absolute path |
| `workflow_path` | absolute path |
| `enabled` | orchestration enabled |
| `health` | `healthy`, `degraded`, `error`, `disabled` |
| `last_poll_at` | last successful poll |
| `last_error` | operator-visible summary |

## Internal Orchestration States

These are daemon states, not tracker states.

| State | Meaning |
|---|---|
| `unclaimed` | no active run and no retry queued |
| `claimed` | reserved, process not yet confirmed running |
| `running` | active runner session exists |
| `retry_queued` | retry timer exists, no active runner |
| `released` | lease removed because item is no longer dispatchable or handling finished |

This is deliberately close to Symphony. It belongs in SQLite, not in frontmatter.

## Attempt And Turn Model

One `attempt` is one worker session against a frozen `record_id + work_revision + runner`.

One `turn` is one Codex turn inside that attempt.

This model is required before continuation turns land. It avoids the misleading pattern where multiple turns on one Codex thread look like unrelated attempts.

Minimum `turns` columns:

| Column | Meaning |
|---|---|
| `turn_id` | immutable Tusker id for this turn |
| `attempt_id` | parent attempt |
| `project_id` | owning project |
| `record_id` | owning work item |
| `turn_index` | 1-based order within attempt |
| `runner_turn_id` | runner-native turn id when available |
| `status` | `queued`, `running`, `succeeded`, `failed`, `interrupted`, `stalled` |
| `failure_class` | runtime failure classification |
| `input_tokens` | usage summary |
| `output_tokens` | usage summary |
| `total_tokens` | usage summary |
| `latest_event_at` | latest normalized runner event |
| `started_at` | start time |
| `completed_at` | terminal time |

The `runs` row may keep denormalized pointers such as `active_attempt_id`, `active_turn_id`, `latest_event_at`, and token totals for fast status views.

## Control Plane

The CLI talks to the daemon over:

```text
<state-root>/daemon.sock
```

Recommended commands:

| Command | Purpose |
|---|---|
| `tusker daemon run` | start daemon in foreground |
| `tusker daemon status` | show daemon health |
| `tusker projects list` | list registered projects |
| `tusker projects add` | register current repo/vault/workflow |
| `tusker projects remove` | unregister project |
| `tusker list --all` | aggregate tracked work |
| `tusker runs` | show active and queued runs |
| `tusker refresh [--project <id>]` | force immediate poll |

## Polling Model

Per cycle:

1. load enabled projects
2. reload `WORKFLOW.md`
3. reconcile SQLite runtime state with runner/session reality
4. refresh tracker view from vault indexes or note scan
5. decide candidate dispatch from durable note state plus SQLite lease state
6. persist runtime updates

Round-robin across projects is good enough for v1.

## Eligibility Rules

An item is dispatchable only if all are true:

- project is healthy and enabled
- item type is allowed by workflow
- durable note `status` is in `tracker.active_states`
- SQLite lease state is `unclaimed` or due `retry_queued`
- no unresolved hard blocker forbids dispatch
- concurrency slot is available
- selected runner is available

Items in `in_review` are observed, not dispatched.

## Health Model

Project health:

| State | Meaning |
|---|---|
| `healthy` | workflow loads, tracker readable, runner available |
| `degraded` | monitor works but something is impaired |
| `error` | dispatch blocked |
| `disabled` | manually disabled |

Daemon health:

| State | Meaning |
|---|---|
| `healthy` | normal operation |
| `degraded` | some projects unhealthy |
| `error` | daemon unhealthy |

## API Shape

Minimum local API:

- `daemon.status`
- `projects.list`
- `projects.add`
- `projects.remove`
- `runs.list`
- `runs.cancel`
- `refresh`

JSON over Unix socket is enough.

## Migration From v1

1. existing CLI remains useful with no daemon
2. `init` gains optional project registration
3. direct `list`, `reindex`, `validate` still work locally
4. daemon adds cross-project aggregation and orchestration
5. legacy note runtime fields are imported or discarded; they are not the new runtime model

## Go Package And File Plan

| File | Responsibility |
|---|---|
| `daemon.go` | bootstrap and main loop |
| `daemon_commands.go` | daemon subcommands |
| `registry.go` | project registry CRUD |
| `runtime_store.go` | SQLite schema and queries |
| `transport.go` | Unix socket transport |
| `runs.go` | lease and attempt coordination |
| `status_commands.go` | status output |

Existing files affected:

| File | Change |
|---|---|
| `cli.go` | add daemon and project routing |
| `install.go` | install daemon service assets |
| `smoke_test.go` | add daemon and registration smoke tests |
