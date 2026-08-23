---
title: "Storage and runtime"
subject: storage-and-runtime
part_of: overview
status: canonical
---

# Storage and runtime

Tusker has repository state and machine state. They have different jobs.

## Repository state

The `.tusker/` directory travels with the repository.

| Path | Content |
| --- | --- |
| `WORKFLOW.md` | Project work and runner policy. |
| `SKILL.md` | Project knowledge route. |
| `work/` | Epics, tasks, gates, waves, decisions, and proposals. |
| `knowledge/` | Durable project facts. |
| `specs/` | Current approved change contracts. |
| `evidence/` | Durable evidence cards. |
| `scratch/` | Temporary task files. |
| `_generated/` | Disposable indexes and views. |

The CLI owns tracker mutations and state revisions.

Tracker writes use owner-only file locks outside the repository. `SKILL.md`
is the vault-wide material lock. When Tusker changes `SKILL.md`, one lock
covers both the file and the material epoch.

## Machine state

`DefaultStateRoot` uses `TUSKER_STATE_ROOT` when it is set. Otherwise, it uses
`~/Library/Application Support/tusker`. If the home directory is unavailable,
it uses the system temporary directory.

The `daemon.db` SQLite file stores project registration, runs, attempts,
sessions, leases, review results, execution records, and other runtime facts.
This database is shared by registered projects on the machine.

## Reset boundary

`tusker purge --repo . --only-tusker-state` removes the known repository
tracker paths for one checkout. It does not clear `daemon.db`.

`tusker projects remove --id <PROJECT-ID>` deletes the project row and the
turn, external-loop, supervisor-decision, attempt, session, and run rows that
`RuntimeStore.RemoveProject` names. It does not delete every table that has a
project ID. Inspect the shared store when a full runtime purge is required.

Never delete the shared database to remove one project.

## Code sources

- `cmd/tusker/daemon.go`
- `cmd/tusker/runtime_store.go`
- `cmd/tusker/purge.go`
- `cmd/tusker/global_uninstall.go`
- `cmd/tusker/scratch_retention.go`
- `cmd/tusker/v7_document_write.go`
