---
title: "Storage and runtime state"
subject: storage-and-runtime
keywords: [runtime-store, sqlite, event-log, vault, workspace, mount, trace, replay, project-registry, migration, sentinel, streams, state-root]
part_of: overview
status: canonical
read_when: "You must know where a piece of Tusker state actually lives — repo vault vs machine state root — which store owns a directory or SQLite table, how leases/events/traces are persisted, or how vault mounts, project registration, and the vault-root/evidence-policy/close-policy migrations behave."
skip_when: "You only need task semantics, gate policy, or dispatch decisions — read [[tasks-and-proof]], [[gates]], or [[orchestration]]."
sources:
  - cmd/tusker/runtime_store.go
  - cmd/tusker/event_log.go
  - cmd/tusker/vault_workspace.go
  - cmd/tusker/vault_root_migration.go
  - cmd/tusker/workspace_manager.go
  - cmd/tusker/sentinel.go
  - cmd/tusker/trace.go
  - cmd/tusker/trace_replay.go
  - cmd/tusker/trace_head_cache.go
  - cmd/tusker/streams.go
  - cmd/tusker/project_loader.go
  - cmd/tusker/project_prune.go
  - cmd/tusker/project_rebind.go
  - cmd/tusker/private_path.go
  - cmd/tusker/runtime_terminal_retirement.go
  - cmd/tusker/git_facts.go
---

# Storage and runtime state

Tusker persists to **two disjoint roots**. Never conflate them.

| Root | Path | Contents | Committed? |
| --- | --- | --- | --- |
| Repo vault | `<repo>/.tusker` (default) | Markdown truth: tasks, gates, evidence, dashboards, generated indexes and traces | Yes (mostly) |
| State root | `$TUSKER_STATE_ROOT`, else `~/Library/Application Support/tusker`, else `$TMPDIR/tusker` | SQLite runtime DB, per-run logs, live work copies, workspace mount config | No — machine-local |

State root resolution is `DefaultStateRoot()` in `cmd/tusker/daemon.go`. Vault
discovery accepts `.tusker` (`defaultRepoVaultDir`) or the legacy directory; see
migrations below.

## Repo vault layout

Created by `bootstrapV7Dirs` (`cmd/tusker/commands_v7.go`). Hand-edit only the
`work/`, `knowledge/`, and `specs/` trees.

| Path | Owner | Hand-edit? |
| --- | --- | --- |
| `work/{epics,tasks,gates,waves,decisions,inbox,closeouts,archive}` | task commands, CAS-guarded frontmatter writes | Yes, via `tusker` commands |
| `knowledge/domains` | authors | Yes |
| `evidence/<TASK-ID>/` | proof commands | No |
| `attempts/<TASK-ID>/<attempt>.md` | attempt writer (`cmd/tusker/commands_v7.go:1274`) | No |
| `events/<YYYY>/<MM>/*.json` | event writers (`emitV7Event`, `cmd/tusker/commands_v7.go:3354`; `cmd/tusker/v7_close_authority.go:429`) | No |
| `dashboards/streams.md` | `writeStreamBoard` (`cmd/tusker/streams.go`), header says "Do not edit" | No |
| `_generated/{indexes,packets,bases}` | index/packet builders | No |
| `_generated/traces/<TASK-ID>/<attempt>.jsonl` | `traceAttemptPath` (`cmd/tusker/trace.go`) | No |
| `config.yaml`, `config.local.yaml` | managed config (`managedTuskerConfigPath`, `cmd/tusker/runner_profiles.go:30`) | No — use `tusker config` |
| `_system/config.yaml` | legacy compatibility config, read only when `WORKFLOW.md` is absent (`legacyConfigPath`, `cmd/tusker/workflow.go:301`) | No |
| `_system/project.yaml` | `upsertProjectTrackerMetadata` (`cmd/tusker/vault_workspace.go:352`) | No |
| `scratch/<TASK-ID>/` | agents, runner wrapper logs (`cmd/tusker/runner_wrapper.go:93`), retention-pruned (`cmd/tusker/scratch_retention.go`) | Yes — noisy logs belong here |

A skill package carrying `work`, `epics`, `evidence`, `attempts`, `events`,
`_generated`, `_system`, `dashboards`, or `Attachments` is refused at install
(`install.go:395`) and by `skill doctor` (`v7_skill_cmd.go:211`). Note-scanning
skips `_system`, `_config`, `Attachments`, and nested `.tusker` (`cmd/tusker/notes.go:361`).

## State root layout

| Path | Owner |
| --- | --- |
| `daemon.db` (+ `-wal`, `-shm`) | `RuntimeStore` (`cmd/tusker/runtime_store.go`) |
| `workspace.yaml` | Obsidian mount config, `workspaceConfigPath()` (`cmd/tusker/vault_workspace.go`) |
| `runs/<project_key>/<record_id>/rev-NN-<lane>-attempt-NNNN-<id>.{prompt.md,events.jsonl,raw.log,status.json}` | daemon dispatch (`cmd/tusker/daemon.go:3966`) |
| `workspaces/<project_key>/<key>/` | `FSWorkspaceManager` (`cmd/tusker/workspace_manager.go`) |

## Runtime store (SQLite)

`OpenRuntimeStore(stateRoot)` (`cmd/tusker/runtime_store.go`) is the only writer
entry point. It is security-hardened before SQLite ever sees the path:

- refuses a group/world-writable state root, a symlinked or non-directory root, or one not owned by the current uid (`ensureRuntimeStateRoot`, `validateRuntimeStateRoot`);
- refuses `daemon.db`/`-wal`/`-shm` that are symlinks, non-regular, foreign-owned, hard-linked (`Nlink > 1`), or group/world-writable, and chmods them to `0600` (`tightenRuntimeStateFiles`) — deliberately **before** `sql.Open`;
- `db.SetMaxOpenConns(1)`; DSN carries `busy_timeout` (`runtimeStoreBusyTimeout` = 1500ms) and every write retries `SQLITE_BUSY` with backoff up to `runtimeStoreBusyRetryLimit` (5s) via `withBusyRetry`.

On open it runs `Migrate()`, `ReconcileDuplicateProjects()`,
`EnsureProjectUniqueness()`, and `ReconcileExpiredResourceLeases()`. Departure
ref intents are **not** reconciled at open — they need a matching registered
project.

Use `OpenRuntimeStoreReadOnly(stateRoot)` for any diagnostic path. It creates
nothing, migrates nothing, takes no write lock (`mode=ro`, `query_only(1)`), and
applies the same lstat checks to the WAL/SHM sidecars.

Tables created by `Migrate()` (`runtime_store.go:832`): `projects`,
`project_rebind_audit`, `runs`, `run_authorizations`, `run_directives`,
`run_identity_metadata`, `attempts`, `review_results`, `completion_transactions`,
`completion_authority_issuances`, `turns`, `supervisor_decisions`, `sessions`,
`apply_inputs`, `daemon_settings`, `gate_ledger`, `batch_gate_runs`,
`resource_leases`, `resource_lease_events`, `departure_runs`,
`landing_authority_issuances`, `external_loop_events`. Additive column changes go
through `ensureColumn`; there is a single `runtimeSchemaVersion = 1` plus a
`runtimeSchemaComplete()` probe.

Concurrency is **compare-and-swap on snapshots plus leases**, not locks:
`UpsertRunIfSnapshot`, `RedriveRunIfSnapshot`, `InterruptRunIfSnapshot`,
`ClaimRunLease`, `RenewRunLease`, `UpdateRunIfLease`, `SaveAttemptIfRunLease`,
`FinalizeRunLease`, `ReclaimExpiredRunLease*`, `CheckRunLeaseGeneration`.
Defaults: heartbeat 15s, lease TTL 60s, reconcile tick 60s. Global concurrency
lives in `daemon_settings` under `max_active_runs` (`GlobalActiveRunLimit`,
`SetGlobalActiveRunLimit`; must be `> 0`).

`gate_ledger` rows carry a `Toolchain` fingerprint; an empty value is legacy
evidence and **never** satisfies a toolchain-bound lookup
(`LatestGateLedgerBefore`, `LatestCompleteGateLedgerBefore`). See [[gates]].

## Event log

`EventLog` (`cmd/tusker/event_log.go`) is append-only JSONL at a run's
`EventSinkPath`. Each `Event` carries a monotonic `Seq`, RFC3339 `At`,
`AttemptID`, `Runner`, `Kind`, `Payload`.

Integrity is enforced per append, not assumed: `withLockedFile` opens the parent
by descriptor, flocks a sidecar lock file, and compares a persisted
`eventLogSequenceMetadata` sidecar (device, inode, size, mtime-ns, last sequence,
checksum) against a fresh `eventLogFileSnapshot`. A mismatch forces
`fullValidate` — a full rescan — rather than trusting the cached sequence.
Path identities are re-verified after the write (`verifyPathIdentities`), and an
exhausted sequence is an error, not a wrap. `Contains(attemptID, kind)` is the
idempotency probe.

## Traces

`RecordAttemptTraces` (`cmd/tusker/trace.go`) projects the event JSONL into
`_generated/traces/<TASK-ID>/<attempt>.jsonl` as `TraceRecord`s at schema
`tusker.boundary_trace/v1`. Recording is idempotent — `existingTraceIDs` dedupes
by `trace_id`. When the attempt reaches any end state, a terminal
`trc_end_<hash>` sentinel record closes the trail so replay can distinguish a
complete recording from a crash-truncated one.

`code_sha` comes from `sharedTraceGitHeadCache` (`cmd/tusker/trace_head_cache.go`),
which caches `git rev-parse HEAD` per repo root keyed on a signature derived from
the git directory metadata; it returns `"unknown"` rather than failing.

Commands: `tusker trace list <TASK-ID>`, `trace show <trace-id>`,
`trace replay <trace-id>` (`traceV7Cmd`). `ReplayTrace`
(`cmd/tusker/trace_replay.go`) re-executes tool boundaries and diffs expected vs
actual state transitions; a truncated trail fails with
`TRACE_REPLAY_INCOMPLETE`. `ReplayForAdjudication` is the strict variant used
when replay decides an outcome.

## Streams board

`refreshStreamBoardForVault` (`cmd/tusker/streams.go`) rebuilds
`dashboards/streams.md` from runtime leases and attempts — lane, task, runner,
worktree, branch (+ahead/-behind), owned paths, heartbeat age, status, end state.
It no-ops when the vault has no `INDEX.md`, and falls back to a workflow-less
context for minimal fixtures. Branch columns come from `captureGitBranchFacts`
(`cmd/tusker/git_facts.go`), which refuses a non-worktree path and reports
branch, HEAD, dirty, default branch, age, ahead/behind.

## Project registry

`projects` rows are `RegisteredProject{ProjectID, ProjectKey, Name, RepoRoot,
VaultRoot, WorkflowPath, Enabled, Health, LastPollAt, LastError}` with health in
`healthy|degraded|error|disabled`. Unique indexes on `repo_root` and `vault_root`
are enforced by `EnsureProjectUniqueness`; duplicates are collapsed by
`ReconcileDuplicateProjects` using `preferRegisteredProject`.

Commands (`cmd/tusker/cli.go:768`): `projects add|list|limits|enable|disable|rebind|remove|prune`.

- `loadRegisteredProjects` (`cmd/tusker/project_loader.go`) loads workflow + notes per project and **quarantines** a project whose load fails, persisting `LastError` and degrading health. `MetadataOnly` and `LoadDisabled` options exist for lifecycle paths that must see disabled projects.
- `projects prune` (`cmd/tusker/project_prune.go`) is **dry-run by default**; `--apply` is required to remove registrations whose tracker root is gone, and it also removes the matching dangling workspace mount.
- `projects rebind` (`cmd/tusker/project_rebind.go`, schema `tusker.projects-rebind/v1`) repoints an existing project at a new repo/vault path. It requires both directories to exist, requires a clean git repo (`requireCleanGitRepository`), and refuses a target vault that another project already workspace-mounts or that a mount would have to move across filesystems. `RebindProjectRegistration` writes registry row + `project_rebind_audit` in one transaction.
- `projects enable/disable` toggles polling only. Disabled projects still get lifecycle truth applied — see retirement below.

## Obsidian workspace mounts

`workspace.yaml` in the state root holds `obsidian_vault` plus one
`WorkspaceProject` per mount. Commands (`cmd/tusker/vault_workspace.go`):
`vault set|status|mount|unmount|move|repair`.

A mount is a **symlink** `<obsidian_vault>/<mount_name> -> <tracker_root>`.
`ensureWorkspaceMount` refuses when the path exists and is not a symlink; when it
points elsewhere it refuses unless `--force`, except for a recognized historical
`<repo>/tusker` target which is silently relinked. `removeWorkspaceMount` refuses
to unmount a non-symlink. `vault move` renames the vault then repairs every
mount; `vault repair` re-points all mounts. Each mount also writes
`ProjectTrackerMetadata` into the tracker root (`upsertProjectTrackerMetadata`).

## Live work copies

`FSWorkspaceManager.Prepare` (`cmd/tusker/workspace_manager.go`) materializes a
work copy per strategy: `shared` (alias of legacy `in_place`), `worktree`,
`clone`, `copy`. `shared` uses the repo root itself and asserts the tree is
clean apart from Tusker bookkeeping paths (`assertInPlaceWorkspaceReady`).

Non-shared strategies live under `<state_root>/workspaces/<project_key>/<key>`.
A configured `workspace.root` must stay lexically **and** resolved-symlink inside
`<state_root>/workspaces`, else config is refused. `MaxLiveWorktrees > 0` caps
concurrent live copies; the count-and-materialize section is serialized with an
flock on the project workspace root to close the TOCTOU window, and exceeding the
cap is a machine-routable `GateRefusal`, refused before any git worktree is
created. Each copy carries `.tusker/workspace.json` (`WorkspaceMetadata`) whose
`PID` is the liveness signal for orphan pruning; copies older than
`staleWorkspaceThreshold` (24h) are treated as stale.

## Sentinel

The invariant sentinel (`cmd/tusker/sentinel.go`) evaluates each reconcile tick
and trips a circuit breaker persisted in `daemon_settings` under
`invariant_circuit_status`; a spend snapshot lives under
`invariant_spend_snapshot`. Default checks: `held_lease_dispatch_eligible`,
`attempt_count_within_caps`, `fresh_heartbeat_pid_live`,
`unique_active_lease_per_task`, `last_poll_advanced`. `active_spend_monotonic`
has an implementation (`sentinelActiveSpendMonotonic`) but **no caller**:
`evaluateInvariantSentinel` skips it even when config lists it. An open circuit blocks dispatch
(`invariantDispatchBlocker`); recovery is `ResumeInvariantCircuit`.

## Private file writes

Any state file written outside SQLite goes through `cmd/tusker/private_path.go`.
`openPrivatePathParent` walks the path one directory descriptor at a time with
`O_NOFOLLOW`, refusing any symlink component and any non-directory component, and
creating missing components at `0700`. `writePrivateFileReplace` writes to a
private temp file at that descriptor and renames; `readPrivateFile` enforces a
byte cap. Never write runtime state with plain `os.WriteFile`.

## Terminal retirement

`cmd/tusker/runtime_terminal_retirement.go` keeps SQLite runtime rows consistent
with canonical Markdown task state. `backlog` and any workflow-terminal status
retire runtime rows. `preflightCanonicalRuntimeRetirement` runs **before** the
task/gate CAS write and refuses with `LIVE_RUNNER_RETIREMENT_REFUSED` while the
run's process group is alive; `retireCanonicalRuntimeRows` rechecks liveness
again immediately before clearing process identity. Retirement applies even when
the project is disabled — disabled controls polling, not lifecycle truth.

## Migrations

| Command | Effect |
| --- | --- |
| `tusker migrate vault-root --to <dir> [--dry-run] [--json]` | Renames the vault to `.tusker` or the legacy dir (nothing else is accepted), rewrites config `storage.root` and pointer files, then **verifies** `discoverVault` resolves to the destination and rolls the file writes back on any failure (`cmd/tusker/vault_root_migration.go`). Warns on a dirty git tree; refuses if the destination exists. |
| `tusker migrate evidence-policy [--write] [--json]` | Backfills `proof_mode`, `evidence_budget`, `raw_artifacts_allowed`, recomputes `proof_status`, syncs the task evidence section, and flags over-budget tasks. Dry-run unless `--write`; writes are CAS-guarded and stamped `updated_by: tusker:migrate-evidence-policy` (`cmd/tusker/v7_proof_cmd.go:2086`). |
| `tusker migrate close-policy [--write] [--json]` | Rewrites legacy `close_policy.<risk>.required_acceptor: human` and `reviewer.human_required_risks` into explicit gates, touching `WORKFLOW.md` and the **managed** config only — a root-level compatibility config stays readable and untouched so it never becomes a second authority (`cmd/tusker/close_policy_migration.go`). |

`tusker migrate gates` is retired; `capabilities_cmd.go:156` maps it to
`migrate evidence-policy|close-policy|vault-root`.

## Agent rules

- Read state through `tusker` commands and `--json`; do not parse `daemon.db` or hand-edit anything in the table above marked "No".
- Diagnostics open the store read-only. If you need runtime facts, prefer `tusker runs inspect` / `tusker streams` over touching files.
- Put noisy output in `.tusker/scratch/<TASK-ID>/`, which is retention-pruned; nothing else in the vault is safe to scribble in.
- A live runner blocks terminal task transitions by design. Interrupt the run (`tusker runs interrupt`) instead of forcing state.
