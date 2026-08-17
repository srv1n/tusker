---
title: "Execution observability"
subject: execution-observability-system
aliases: [execution-observability-system]
keywords: [executions, ledger, lineage, lifecycle, work sessions, heartbeats, runs, provider observations]
part_of: overview
status: canonical
read_when: "You need to register, correlate, bind, inspect, cancel, or debug an execution strand — direct agent session, daemon-leased attempt, or provider-native child — or to reason about work sessions, run leases, heartbeats, and the live registry."
skip_when: "You need scheduling, admission, wave arming, or daemon dispatch policy ([[orchestration]]), or the runner transports and ACP adapter stack themselves ([[runners-and-acp]])."
sources:
  - cmd/tusker/execution_commands.go
  - cmd/tusker/execution_ledger.go
  - cmd/tusker/execution_graph.go
  - cmd/tusker/execution_lifecycle.go
  - cmd/tusker/execution_mode.go
  - cmd/tusker/codex_execution_adapter.go
  - cmd/tusker/claude_execution_adapter.go
  - cmd/tusker/work_session_cmd.go
  - cmd/tusker/work_session_readiness.go
  - cmd/tusker/live_registry.go
  - cmd/tusker/runner_wrapper.go
---

# Execution observability

An **execution** is one durable, observable strand of agent work behind one
immutable Tusker execution ID. SQLite is the only ownership authority: the `runs` lease row decides
who owns a task. Everything in this document is either identity (immutable), correlation
(append-only audited events), or observation (untrusted provider facts). None of it can claim, arm,
prove, review, land, or spend.

Read this when you need to answer "what was running, under whose authority, and what do we actually
know about it". Dispatch policy lives in [[orchestration]]; transports live in [[runners-and-acp]].

## The three node kinds

`cmd/tusker/execution_ledger.go` (`ExecutionNodeKind`) recognizes exactly three:

| Node kind | Created by | Owns a lease? | Notes |
|---|---|---|---|
| `root` | `CreateDirectExecution`, `CreateWaveExecutionRoot` | no | One per direct invocation; one per wave *authorization generation* (unique on `project_id, wave_id, wave_authorization_generation`). |
| `managed_attempt` | `CreateManagedExecution`, `CreateExecutionContinuation` | yes (Tusker's) | Insert is fenced inside a transaction that re-reads `runs` and rejects unless `active_attempt_id`, `lease_generation`, and `lease_state ∈ {claimed, running}` still match (`insertManagedExecution`). |
| `provider_child` | `UpsertProviderChildExecution` | never | Idempotent on `(project_id, parent_execution_id, provider, provider_child_handle)`. Visible but provider-owned. |

Edges are typed and immutable (`execution_edges`, one parent per child, cycle-checked by SQLite
trigger): `managed_child_of` for a managed child, plus `retry_of`, `resume_of`, `fork_of`,
`provider_child_of`. A trigger also rejects any edge kind that disagrees with the child's node kind.
A retry/resume/fork is a *continuation*, not a concurrent sibling — it still passes the same
live-lease fence.

```text
wave authorization root (root)
└── task attempt (managed_attempt, leased by Tusker)
    ├── retry_of / resume_of / fork_of  → next managed_attempt
    └── provider_child (observed, provider-owned)
```

## Ledger tables and their immutability

`migrateExecutionLedger` creates the first five tables below; `migrateExecutionLifecycle`
(`cmd/tusker/execution_lifecycle.go`) creates the last two. All seven carry `BEFORE UPDATE`/
`BEFORE DELETE` triggers that `RAISE(ABORT, ...)`. Nothing is ever rewritten; every change is a new
row.

| Table | Holds | Effective value |
|---|---|---|
| `execution_records` | immutable identity, lineage, provider correlation seeds | never changes |
| `execution_edges` | typed parent→child lineage | never changes |
| `execution_name_events` | audited renames | latest by `created_at, event_id` |
| `execution_attachment_events` | provider session/cloud-task correlation | latest by `created_at, event_id` |
| `execution_binding_events` | `bind` / `detach` / `rebind` at monotonic `generation` | highest `generation` |
| `execution_lifecycle_evidence` | per-dimension lifecycle snapshots | latest by `observed_at, event_id` |
| `execution_cancellation_evidence` | cancellation stages keyed `(cancellation_id, stage)` and `(execution_id, request_key, stage)` | replayed terminal stage |

`ExecutionView` (`execution_ledger.go`) folds record + latest events. `ProofEligible` is true only
when `BindingGeneration > 0`, the latest action is not `detach`, and a task is bound. Observations
recorded before a bind stay visible and stay proof-ineligible — a bind never launders exploratory
work into delivery evidence.

## Direct-work CLI

`cmd/tusker/execution_commands.go`; all actions are authority-neutral.

```bash
tusker execution register --source direct_codex --provider codex --name "Lease audit" --json
tusker execution attach   --id exec_… --provider codex --provider-session-id THREAD_ID --json
tusker execution rename   --id exec_… --name "Lease recovery audit" --json
tusker execution bind     --id exec_… --task ORC-T-0000 --json     # also: detach, rebind
tusker execution inbox --json      # the unbound inbox: proof-ineligible direct roots
tusker execution list  --task ORC-T-0000 --lifecycle active --json
tusker execution show  --id exec_… --json
tusker execution cancel --id exec_… --request-key <idempotency-key> --json
tusker execution launch --id exec_… --json
```

Refusals worth knowing:

- `register --source` accepts only `direct`, `direct_codex`, `direct_claude`, `codex_cloud`.
- `attach` is idempotent per `(project, provider, provider_session_id)`; re-attaching that identity to
  a *different* execution fails with `INVALID_TRANSITION`.
- `bind`/`rebind` derive the wave from canonical wave membership. Multiple waves, no wave, a
  disagreeing task `wave:` back-pointer, or a conflicting explicit `--wave` all refuse
  (`executionCanonicalWave`). The store additionally refuses when the task has a live
  `claimed`/`running` non-terminal run.
- `launch` calls `rejectAgentSpawn` **before** opening or migrating the state DB
  (`cmd/tusker/execution_mode.go`): it refuses inside a dispatched worker (`TUSKER_ATTEMPT_ID`), an
  interactive Codex session (`CODEX_SHELL`/`CODEX_THREAD_ID`), or an interactive Claude session
  (`CLAUDECODE`/`CLAUDE_CODE_ENTRYPOINT`). It only ever reports process facts —
  `"authority": "observation_only"` — and a `codex_cloud` execution reports `process.available:false`
  and rejects `--pid`.

## Lifecycle dimensions

`ExecutionLifecycle` keeps seven dimensions separate and never collapses them. Process, provider,
outcome, and session default to `unknown`; child attention defaults to `none`; delivery and
admission are always derived from the view. A read merges current facts over the last immutable
snapshot but never deletes a contradicting one.

| Dimension | Values seen in code | Source |
|---|---|---|
| `delivery_state` | `bound`, `unbound` | `ExecutionView.ProofEligible` |
| `admission_state` | `admitted`, `not_admitted` | attempt ID present or a matching run row |
| `process_state` | `running`, `not_running`, `unknown` (`lost` is read by `executionDerivedPhase` but never written today) | `runProcessGroupAlive` on the fenced run |
| `provider_state` | `starting`, `running`, `acknowledged`, `interrupt_requested`, `terminal`, `failed`, `cancelled`, `unknown`, `unavailable` | latest `provider_execution_observations` row; `unavailable` when degraded |
| `outcome_state` | `AttemptOutcome` values, or `unknown` | run's attempt outcome |
| `session_state` | `known`, `unknown` | session ref on run or view |
| `child_attention_state` | `needs_attention`, `none` | newest fact per child in `{unknown, interrupt_requested, failed}` |

`derived_phase` is display shorthand only, computed in this order (`executionDerivedPhase`):
`needs_attention` → `active` → `settled` → `unsettled` → `pending`. A terminal provider or process
fact does **not** imply an outcome.

Child attention is derived from each child's *newest* observation, so a child that recovered is no
longer flagged, while the old failure stays as immutable timeline evidence.

## Cancellation and controls

`ExecutionControlAvailability` (`executionControl`) answers whether a cancel button may exist:

- Managed, non-provider node with a matching run → available **only** if `processIdentityMatches`
  (PID + PGID + process start). Otherwise the reason is "managed run process identity is not
  currently verifiable".
- Otherwise it needs a non-degraded provider observation with a `parent_interrupt` (or
  `independent_child_control` for provider children) capability fact in state `true` and fresher than
  **5 minutes**. Even then it returns *unavailable*: "provider capability is fresh, but no
  target-specific control route is installed". No adapter implements provider-side cancellation today.

`RequestExecutionCancellation` is idempotent per `(execution_id, request_key)` and records every
stage as immutable evidence: `requested`, `provider_acknowledgement`, `descendant_settlement`,
`timeout`, then either `unavailable`, or `wrapper_signal` → (`escalation` on error) → `os_settled`.
Only `target == "managed_run"` reaches `interruptRunProcess`. A prior request with no durable
terminal stage replays as "no second signal sent" rather than re-signalling.

## Graph and timeline

`tusker execution list` / `GET /api/executions` return `ExecutionGraphPage`
(`schema: tusker.execution-graph/v1`, `cmd/tusker/execution_graph.go`). Default limit 100, clamped to
250; the cursor is the last node's execution ID and an unknown cursor is an error, not an empty page.

Filters: `--execution`, `--root`, `--parent`, `--task`, `--wave`, `--source`, `--provider`,
`--provider-id`, `--agent-type`, `--binding`, `--lifecycle`, `--name`, `--attention`, `--cursor`,
`--limit`. Task and wave filters match either immutable provenance *or* the current audited binding.
`--lifecycle` matches any dimension plus provider status, lease state, attempt outcome, and derived
phase.

Pages are **edge-closed**: an edge is emitted only when both endpoints are on the page; when exactly
one endpoint is present the page sets `topology_partial: true` so a consumer never promotes an
orphaned child to a root. `partial_visibility` propagates up from degraded provider observations.

Serve exposes `GET /api/executions`, `/api/executions/inbox`, `/api/executions/timeline`,
`GET /api/executions/{id}/binding-preview`, and `POST /api/executions/{id}/{cancel,rename,bind}`
(`cmd/tusker/serve_command.go`, `cmd/tusker/serve_execution_graph.go`). The timeline
(`tusker.execution-timeline/v1`) is ordered by source epoch and sequence and flags `reset`, `gap`,
and `stale_cursor` rather than silently splicing; the UI answers each by refetching the
authoritative tail. See [[serve-ui]]. Only the graph-specific surface lives here: the generic
operations composition is owned by `ORC-T-0046` and the broad factory-loop regression by
`ORC-T-0047`.

## Provider observation limits

Provider hooks, JSON streams, and cloud status are untrusted input: they are normalized, bounded,
and recorded as observations, never promoted to ownership or proof.

| Provider | Correlated by | Adapter | What it must not imply |
|---|---|---|---|
| Codex local (`codex`, `codex_app_server`, `codex_exec`) | `run.SessionRef` → thread ID | `CodexExecutionAdapter.ObserveRunPayload` | A hook never establishes ownership or delivery authority. |
| Codex Cloud (`codex_cloud`) | `run.CloudTaskID` | `CodexExecutionAdapter.ObserveCloud` | `rejectCodexCloudLocalProcessMetadata` rejects any metadata key containing pid/pgid/heartbeat/process/ossettlement. |
| Claude Code (`claude-code`) | `run.SessionRef` → top-level session; `SubagentStart`/`SubagentStop` | `ClaudeExecutionAdapter.ObserveRunPayload` | A subagent ID never becomes the parent's resumable session. |

Both adapters normalize provider strings into one status vocabulary
(`starting|running|acknowledged|interrupt_requested|terminal|failed|cancelled|unknown`) and mark
degraded visibility rather than guessing. A degraded reason means the fact is unusable until an
authoritative fetch replaces it. Reasons that appear verbatim:
`provider_timestamp_missing_requires_authoritative_fetch`,
`unrecognized_provider_status_requires_authoritative_fetch`,
`provider_status_regression_requires_authoritative_fetch`,
`subagent_start_missing_or_requires_authoritative_reconciliation`, `child_transcript_missing`,
`parent_terminal_before_child_stop_partial_or_lost`.

A terminal Claude parent triggers `reconcileUnsettledChildren`, which writes a distinct `unknown`
diagnostic observation per started-but-unstopped child. That is diagnostic only: it never fabricates
a child outcome, lease, or capability. Default capability facts are deliberately `unknown`/`false`
(`codexDefaultCapabilities`, `claudeDefaultCapabilities`) so controls stay hidden until proved.

## Work sessions (direct, user-directed)

`tusker work start|status|heartbeat|submit|fail|release` (`cmd/tusker/work_session_cmd.go`) is the
interactive claim path. `--source` accepts only `tusker_cli`, `codex`, `claude` — `daemon_auto` is
rejected so a shell cannot impersonate the dispatcher. `start` emits a
`tusker.work-session/v1` packet (task, owner, `work_revision`, workspace, branch, head, lease expiry,
agent packet, next command, run row, authorization).

Admission blockers (`cmd/tusker/work_session_readiness.go`) are deliberately narrower than daemon
dispatch — automation enablement, wave authorization, runner health, and risk policy cannot refuse
direct work:

| Blocker | Error code |
|---|---|
| task status `done`/`cancelled`/`superseded` | `WORK_SESSION_TERMINAL` |
| task status not `ready`/`rework` | `WORK_SESSION_NOT_READY` |
| unsatisfied `blocked_by` / `blocked_by_record_ids` / graph dependency | `WORK_SESSION_DEPENDENCY_BLOCKED` |
| open blocking human gate that passes the well-formedness filter | `WORK_SESSION_HUMAN_GATE` |
| healthy existing owner | `WORK_SESSION_HEALTHY_OWNER` |
| unsafe workspace prepare | `WORK_SESSION_UNSAFE_WORKSPACE` |
| overlapping owned paths | `OWNED_PATH_CONFLICT` |

Reclaiming a same-task holder needs **two independent facts**: heartbeat past the reclaim grace *and*
the recorded process identity gone. Lifecycle commands enforce optimistic concurrency on
`--revision` (`CAS_CONFLICT` against the run row, `WORK_SESSION_STALE` against the task note).
Interactive sessions set `projectConcurrencyLimit = 0`; they notify the daemon one-way with
`reconcile_project` (250 ms timeout) and never start it.

## Runs, heartbeats, and the live registry

`tusker runs` alone prints help. `runs claim` (`runsClaimCmd`) then
`runs start|heartbeat|submit|fail|reclaim` (`runsLifecycleCmd`) drive ownership, and
`runs inspect|logs|events|interrupt|release|retire|redrive` are the operator surface
(`cmd/tusker/cli.go`).

Heartbeats come from the detached wrapper, not the daemon: `runnerWrapperHeartbeat` renews the lease
with the wrapper's own PID/PGID/process-start, and `runnerWrapperStopSignal` stops the child when
the generation advanced, the owner changed, the lease row vanished, or the state left
`claimed`/`running`. A failed renewal is a stop, never a silent continue. Timings and signal
escalation are in [[runners-and-acp]].

`LiveRegistry` (`cmd/tusker/live_registry.go`) is an **in-process** map from attempt ID to
`LiveRunnerHandle`, with fallback lookup by item/record ID. It is how the wrapper and `ACPRunner`
deliver a graceful interrupt inside their own process; it is not durable and is not an authority.

## Migration and backfill

`backfillExecutionLedger` projects legacy `attempts` rows into the ledger at the deterministic ID
`exec_legacy_<sha256(attempt_id)[:12]>`, source `legacy_attempt`, provider from `attempts.runner`.
An attempt whose parent row is missing gets a synthetic `legacy_missing_parent` root. Backfill is
idempotent and restart-safe: it re-reads the existing row and refuses on any change to identity,
lineage, task, provider, or ownership, while deliberately tolerating later enrichment of
`session_ref`, `search_label`, and `created_at`. Attempts predating `started_at` pin to
`1970-01-01T00:00:00Z`.

Ledger constraints (indexes and triggers) are versioned in `execution_ledger_migrations` and
re-created transactionally whenever the recorded version or any stored SQL text drifts from the
compiled statements.
