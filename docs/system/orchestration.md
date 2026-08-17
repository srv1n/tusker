---
title: Orchestration
subject: orchestration
keywords: [daemon, automation, dispatch, scheduler, departure, launchd, disk-pressure, crash-loop, frontier]
part_of: overview
status: canonical
read_when: "You need what the resident daemon does each tick, why a task will not dispatch, how fair scheduling and capacity caps arbitrate, or how scheduled promotion (departure) runs, holds, drifts, and recovers."
skip_when: "You only need runner/harness behavior ([[runners-and-acp]]), wave arming and delivery planning ([[delivery-and-waves]]), or the landing/completion transaction ([[landing-and-completion]])."
sources:
  - cmd/tusker/daemon.go
  - cmd/tusker/daemon_scheduler.go
  - cmd/tusker/daemon_service.go
  - cmd/tusker/daemon_launchd.go
  - cmd/tusker/daemon_guard.go
  - cmd/tusker/daemon_control.go
  - cmd/tusker/automation_commands.go
  - cmd/tusker/dispatch_scope.go
  - cmd/tusker/adaptive_reconcile.go
  - cmd/tusker/disk_pressure.go
  - cmd/tusker/budget.go
  - cmd/tusker/frontier_index.go
  - cmd/tusker/departure_scheduler.go
  - cmd/tusker/departure_execution.go
  - cmd/tusker/departure_planner.go
  - cmd/tusker/departure_store.go
  - cmd/tusker/scheduled_promotion.go
---

# Orchestration

The resident daemon is the only process that dispatches local runners; everything else —
planning, explaining, queueing — is read-only. Runner internals: [[runners-and-acp]]. Wave
arming and delivery planning: [[delivery-and-waves]]. Landing/promotion transaction:
[[landing-and-completion]]. Observed provider children: [[execution-observability-system]].
Execution visibility is not dispatch authority: an execution record makes work observable, but
only the `runs` lease decides who may act.

## Who may dispatch

Two process-level refusals guard dispatch, both evaluated before any task-local blocker:

| Guard | Code | Trips when |
| --- | --- | --- |
| Agent-session | `rejectAgentSpawn` (`cmd/tusker/execution_mode.go`) | `TUSKER_ATTEMPT_ID`, `CODEX_SHELL`, `CODEX_THREAD_ID`, `CLAUDECODE`, or `CLAUDE_CODE_ENTRYPOINT` set. Applied to `daemon run`, `daemon service install/start`, `automation dispatch`, `delivery rollout repair`, `execution launch`. |
| One-shot | `oneShotDispatchRefusal` (`cmd/tusker/daemon.go:1799`) | Injected into `daemon run --once` (`:130`), `tusker refresh` (`:7222`), `automation external-loop --dispatch` (`automation_external_loop.go:98`). |

`tusker automation dispatch` **never dispatches**: `automationDispatchCmd`
(`automation_commands.go:288`) validates the task exists, then returns the one-shot refusal
unconditionally — there is no success path.

| Command | Effect |
| --- | --- |
| `automation status` | Fleet summary: daemon liveness/PID/uptime, launchd mode, last restart cause, run counts, crash-loop + invariant circuits, disk pressure, per-project summaries, armed waves (`:176`). |
| `automation queue` | Non-terminal tasks split into `eligible` / `blocked` with blockers (`:226`, `:453`). |
| `automation explain <task>` | Per-task blockers, selected runner/profile, workspace, approvals, fanout (`:517`). |
| `automation plan <task>` | Canonical decision — empty `plan.Blockers` ⇒ dispatchable (`:264`). |
| `automation dispatch <task>` | Always refused. `collect-external` / `external-loop` / `advance-external`: see below. |

All accept `--project <id>` / `--repo <path>` / `--vault <path>`; a registered project loads
even when the registry has it disabled, because registry state is a polling-cost control, not a
visibility gate (`:424`).

## Daemon lifecycle

`daemonRunCmd` (`daemon.go:6569`) → `acquireDaemonGuard` → `Daemon.Run` (`:128`).
**Single instance**: non-blocking `flock` on `<state>/daemon.lock`, plus `<state>/daemon.pid`
holding pid, started_at, state_root, serve enabled/addr, managed_by_launchd
(`daemon_guard.go:60`); contention returns `daemonAlreadyRunningError`. **State root**:
`$TUSKER_STATE_ROOT`, else `~/Library/Application Support/tusker`, else `$TMPDIR/tusker`
(`:87`). **`--once`** sets the one-shot refusal, disables departure execution, skips control
server / serve / watchdog, runs exactly one `PollOnce`, returns (`:129-172`). **Resident**:
control server → serve (if configured) → watchdog → initial full poll → loop.

| Wake source | Behavior |
| --- | --- |
| `notifyWake` channel | `"*"` polls everything; a project ID stamps `cli_mutation` activity and polls that project (`:207-227`). |
| Poll timer | `adaptiveProjectsDue`, then any `departureProjectsDue` not already covered (`:228-250`). |
| Attention ticker (1s) | Serve only; polls browser-watched projects at most every 20s (`:268`). |

Next wait is `min(adaptive wait, next departure window)` (`nextDepartureWait`), floored at 100ms
by `resetTimer` (`adaptive_reconcile.go:281`). **Control socket**: `<state>/daemon.sock`, mode 0600, ≤64KB/request, ≤32 concurrent; startup
refuses a symlink, a non-socket path, or a socket owned by another UID (`daemon_control.go:75`).
Commands: `interrupt`, `stop`, `reconcile_project` (optional change hints), `reconcile_registry`.
**Watchdog**: ticks 5s; `pollOnce` writes `daemon_watchdog_beat_at`; beat age >
`3 × adaptiveWatchdogCadence` (smallest live per-project cadence, default 1m) records
`watchdog_stale` and calls `os.Exit(70)` so launchd restarts (`:315-379`).

### launchd service (macOS only)

`tusker daemon service install|start|stop|status|uninstall` (`daemon_service.go:409`). Label
`com.tusker.daemon`; plist in `~/Library/LaunchAgents`; stdout **and** stderr both go to
`<state>/logs/daemon.log`; the executable is copied into `<state>/bin`.

Install/start block when an enabled project sits under a macOS-protected folder;
`--allow-protected-projects` is the explicit override after Full Disk Access. Both wait ≤5s for
a poll newer than the start time; `status` reports healthy when the last poll is within 120s
(`daemonHeartbeatDeadThreshold`). Logs rotate at install/start under an flock: 5 files / 7 days
/ 10MB (`:93`, `:170`). Non-darwin errors out and points at `tusker daemon run` under your own
service manager.

### Crash-loop circuit

Only launchd-managed runs participate. `beginManagedDaemonStart` (`daemon_launchd.go:186`)
consumes pending abnormal-exit causes in one transaction: `stale_pid` (pid file survived —
inferred SIGKILL), `run_error`, `watchdog_stale`, `clean_start`. More than **5** abnormal starts
within **600s** opens the circuit; while open `crashLoopDispatchBlocker` returns `daemon circuit
open: <summary>` for every candidate. `tusker daemon resume` closes it and the invariant circuit.

### Operator commands

| Command | Effect |
| --- | --- |
| `daemon status [--json]` | Paths, liveness, per-project health and dispatch-scope projection, active/parked counts, launchd mode, crash-loop / invariant / budget circuits, disk pressure (`:6641`). |
| `daemon limits [--max-active-runs n] [--disk-pressure-enabled\|-min-free-bytes\|-min-free-percent]` | Reads/writes global `runtime.max_active_runs` (default 2) and the runtime-store disk-pressure config (`:6817`). |
| `daemon resume` / `daemon stop [--drain]` | Closes invariant + crash-loop circuits; `stop` over the control socket waits 5s for the pid file to clear, `--drain` then waits bounded for detached wrappers (`:6893`). |

## One poll tick

`pollOnce(ctx, projectID)` (`daemon.go:836`); empty `projectID` means every enabled project.

1. Cache process identity; feed the watchdog beat. Load projects — a targeted poll with a
   usable frontier hint applies the incremental index instead of re-scanning, otherwise
   frontmatter-only notes. Full task bodies load only for projects with non-terminal tasks or
   external-loop apply inputs (`:1562`).
2. Count global dispatch capacity; resolve the global limit.
3. Per project: refresh budget circuit, `scheduleBatchGateIfDue`, `scheduleDepartureIfDue`,
   `startPendingDepartureExecutions`.
4. Per existing run: normalize dead `retry_queued` rows, park at attempt caps, reconcile
   against plan and tracker, auto-advance the external loop.
5. Per active-state task: resolve runner profile, reset on `work_revision` change, run the
   blocker ladder, append a `daemonDispatchCandidate`. Per review-state task: consume typed
   results, park at reviewer cycle cap, same ladder for the `review` lane.
6. `reconcileReviewCompletion`, delete runs for vanished records, `TouchProjectPoll`, then
   `dispatchFairCandidates` across every project's candidates. Write `daemon_last_poll_at`,
   refresh the invariant sentinel and the Serve snapshot.

A `CAS_CONFLICT` from a poll is not an error — it reschedules and returns (`:303`).

## Blocker ladder

Evaluated per candidate in `pollOnce`, in order; each stamps `run.LastError` and skips.

| # | Check | Source | Reason shape |
| --- | --- | --- | --- |
| 1 | Process dispatch refusal | `d.dispatchRefusalReason` | errors the whole poll, not a per-task skip |
| 2 | Dispatch scope / armed wave | `automationDispatchScopeBlocker` (`dispatch_scope.go:70`) | `dispatch blocked: dispatch scope armed_waves requires task membership in a currently armed wave` |
| 3 | Readiness / dependencies | `daemonDispatchBlockedReason` | `dispatch blocked: <reason>` |
| 4 | Crash-loop circuit | `crashLoopDispatchBlocker` | `daemon circuit open: …` |
| 5 | Budget | `budgetDispatchBlocker` (`budget.go:276`) | **inert — always returns no blocker** |
| 6 | Invariant sentinel | `invariantDispatchBlocker` | circuit summary |
| 7 | Per-state cap | `stateDispatchCapReachedForRun` | `dispatch blocked: state %q concurrency cap reached` |
| 8 | Full automation plan | `executePlanBlockedReason` (`:2056`) | `automation plan do_not_dispatch: <blockers>` |

Review-lane candidates prefix scope failures with `review dispatch blocked: ` and add
`reviewDispatchAllowed`, the reviewer cycle cap (`review parked: automated review cycle cap
reached (n/max)`), and `armedWaveReviewDependencyBlocker`.

### Dispatch scope

`automation.dispatch_scope` resolved from the config layer stack (`dispatch_scope.go:42`).

| Configured | Effective | Meaning |
| --- | --- | --- |
| `armed_waves` | `armed_waves` | Task must name a wave **and** that wave must authorize it. Also the fresh default for unconfigured, automation-disabled projects. |
| `all_eligible` | `all_eligible` | No scope constraint beyond ordinary eligibility. |
| absent + automation enabled | `all_eligible` + warning | Legacy hybrid: waveless tasks pass, wave-bound tasks still need their wave armed. `daemon status` prints the warning and repair line. Anything else is a config error. |

Scope admits *fresh* work only: a run in `retry_queued`/`claimed`/`running`, or with
`AttemptCount > 0` and a lane set, is a continuation and bypasses scope (`:94`).

## Fair dispatch scheduler

`dispatchFairCandidates` (`daemon_scheduler.go:432`) arbitrates globally contended capacity
after every project-local check passed; all projects' candidates compete in one pool.

**Order** (`fairDispatchLess`, `:95`): `priority` rank → least-recent project turn from the
durable ledger → project ID → item ID → lane. The ledger (`fair_dispatch_ledger_v1` setting) is
an ordering hint only; a corrupt value silently falls back to stable IDs. **Gates**, each
persisting `dispatch waiting: <reason>` and moving on:

| Gate | Limit source | Reason |
| --- | --- | --- |
| Global capacity | `runtime.max_active_runs` (default 2) | `global capacity reached (n/m); fair order is priority X then least-recent project turn` — drains the whole remaining list and breaks |
| Post-reactor refresh | `refreshFairExecuteCandidate` (`:367`) | reloads canonical notes and reruns scope + eligibility + full plan for execute/integrator lanes; `post-reactor …` |
| Project capacity | `runtime.max_active_runs_per_project` (default 1) | `project capacity reached (n/m)` |
| Runner capacity | `agents.max_concurrent_agents` | `runner X capacity reached (n/m)` |
| State capacity | `agents.max_concurrent_agents_by_state[status]` | `state %q concurrency cap reached (n/m)` |
| Disk pressure | below | `dispatch blocked: disk_pressure: …` |
| Scope / wave | `scopeDispatchBlocker` | `dispatch scope or wave constraint: …` |
| Owned-path conflict | `ownedPathConflict` | `owned path conflict: <holder> holds <path> for task <id> (candidate <path>; liveness <state>)` |
| Named resources | task `resource_refs` + `concurrency_group` | `named resource %q is held by project X owner Y`; loser registers as a waiter |

Capacity counts only `claimed`/`running` runs (`runConsumesDispatchCapacity`); queued,
released, interrupted, and parked rows wait without consuming a slot — counting them live-locks
dispatch once the queue exceeds the cap (`daemon.go:1599`). Named-resource leases are reserved
before the claim and released if the claim does not retain capacity; each tick reconciles
orphaned `task-dispatch:` leases by releasing those whose run is no longer live, renewing the
rest, and reacquiring for the exact owner after a TTL lapse (`:180`).

## Adaptive reconcile

Per-project cadence, in memory only (`adaptive_reconcile.go`):

| Tier | Cadence | Condition |
| --- | --- | --- |
| `live` | 5s | any run needs hot reconcile |
| `hot` | `nextPollInterval()` — default 60s, `TUSKER_POLL_INTERVAL_MS`, floor 5s | idle < 1m |
| `warm` / `cool` / `cold` | 5m / 10m / 30m | idle < 5m / < 10m / otherwise |

Hot-reconcile runs (`runtimeRunNeedsHotReconcile`, `:58`): non-terminal
`claimed`/`running`/`retry_queued`/`interrupted`; a clean `unclaimed` row with no `LastError`;
a review-lane row waiting for review; a released review row whose typed result awaits the
completion reactor. Any CLI mutation notification stamps the project `hot`.

## Frontier index

In-memory-only projection per project (`frontier_index.go`); canonical Markdown and the runtime
store stay authoritative, so discard and rebuild on any doubt. `rebuild(notes)` builds records, forward/reverse dependency edges, wave membership, and per-task
eligibility. `apply(changed)` replaces only changed records and recomputes the affected closure
— reverse-dependency closure, the wave's members, and (for evidence, attempt, closeout records)
the closure of the task they point at (`:162`). `touch(taskIDs)` recomputes when runtime state
changed but no Markdown did. `eligibility(id)` (`:256`) requires status `ready` or `rework`; a
named wave `armed` **and** listing the task as a member; no open blocking gate targeting it;
every forward dependency edge satisfied. `frontierHints` from `reconcile_project` control
requests let a targeted poll skip the scan when every hinted ID is already known.

## Disk pressure

Measured before every dispatch selection (`disk_pressure.go`); enabled by default at
`min_free_bytes = 2 GiB`, `min_free_percent = 1`. Effective threshold per filesystem is
`max(min_free_bytes, min_free_percent% of total)`.

| State | Condition | Effect |
| --- | --- | --- |
| `ok` / `warning` | available ≥ / < 2 × effective threshold | dispatch continues; `warning` is flagged |
| `paused` | available < effective threshold | `DispatchPaused` |
| `error` | stat failed | `DispatchPaused` (fail closed) |

Paths measured: state root and the selected workspace; an empty workspace path is skipped so the
early guard cannot leave a stale observation on the process CWD. Status is merged and CAS-written
(≤64 attempts); observations older than 5m are dropped and status degrades to `unknown` rather
than staying stuck paused. A paused status that freshly remeasures clean reports `recovered`.

## Budgets — inert

Token budgets are **diagnostic-only**. `withDefaultRuntimeBudgetConfig` (`budget.go:73`)
force-sets `Enabled = false` regardless of workflow or task config; `budgetDispatchBlocker` and
`enforceBudgetForRun` are no-ops; `BudgetCircuitStatus` / `ReadBudgetCircuitStatus` never read
persisted state. Runs still parked in `parked_budget` are released on sight with
`legacy token budget park released: token telemetry is diagnostic-only` (`:294`). Turn
accounting (`SumRunTokens`, `SumAttemptTokens`, `SumTokensSince`) remains for forensics. The one
live cap here is `enforceTurnCapForRun` (`:305`): for `codex-exec` runs only, observed turns >
the lane's `max_turns` stops execution, records `turn_cap_exhausted`, schedules a retry.

## Scheduled promotion (departure)

Daemon-owned system work with a lifecycle deliberately independent of task and attempt states
(`departure_store.go:14`).

| Mode | Observe | Stage | Promote | Release |
| --- | --- | --- | --- | --- |
| `disabled` (default) / `shadow` | — / ✓ | — | — | — |
| `stage` | ✓ | ✓ | — | — |
| `promote` | ✓ | ✓ | ✓ | only if `release.authorized` and a profile is named |

Once `scheduled_promotion` is configured at all, the departure executor becomes the **sole**
authority allowed to advance the default branch; unconfigured/legacy repos keep manual `tusker
land` (`scheduled_promotion.go:48`). **Windows** reuse the batch-gate clock (`orchestration.batch_gate.windows`, daily local
`HH:MM`); no windows ⇒ inert (`departure_scheduler.go:149`). At most one run per
`(project, policy_id, window)`, policy ID `scheduled-promotion/v1/<mode>`. A window missed by
more than 1m (`departureWindowGrace`) is `skipped` unless mode is `promote`, which coalesces on
its newest missed window.

| State | Meaning |
| --- | --- |
| `due` → `evaluating` | created for a window; planner decision accepted (idempotent restart-safe handoff) |
| `staging` → `gating` → `promoted` | land cargo onto wave integration branches; promotion gate + ref update; ref committed |
| `releasing`, `repairing` | separate owners (release; red-gate failure routing) |
| `passed` / `skipped` / `blocked` / `failed` | terminal (`departureTerminal`) |

`executeDeparture` (`departure_execution.go:198`) is a phase loop capped at 16 durable reloads;
each phase re-reads the row and a lost CAS follows the winner. Execution runs on a goroutine per
run outside the poll so a long gate cannot stall reconciliation; `Daemon.Close` cancels with
`errDaemonDepartureShutdown` and waits. `--once` disables departure execution.

**Drift is fatal, not recovered.** The candidate pins cargo task IDs, wave IDs, per-task state
revisions and source SHAs, integration base SHA, candidate SHA/tree hash, and expected default
SHA. `departurePlanningDrift` and `departurePostStagingDrift` compare re-planned facts against
the pinned ones and block with
`departure recompute required: <cargo|wave|task|source|integration|default_ref|gate>_drift`.

A hold is re-checked at *every* phase boundary; promote mode refuses unless there is exactly one
cargo wave; contention on the `gate:full` resource lease registers a waiter and defers
(`errDepartureExecutionDeferred`) rather than failing. `departurePlanner.PlanDeparture` (`departure_planner.go:86`) is read-only apart from a bounded
fetch of the configured remote-tracking ref; dispositions are `disabled`, `empty`, `ready`,
`already_gated`, `blocked`, `indeterminate`. Waveless tasks enter the candidate immediately;
wave-bound tasks stay facts until every member of their atomic delivery unit validates. Staging
delegates to `tusker land` and requires a daemon-issued landing authority.

Operator surface, all `--project <id>` (`departure_commands.go`): `check` runs the planner
read-only and prints disposition + reasons; `status` shows the latest run, count, and active
hold / release-hold; `history [--limit 1..100]` lists windows newest-first (default 20);
`hold [--release-only] --reason <why> --by <actor>` writes a durable runtime-DB hold where a
global hold outranks a project hold; `resume [--release-only] --by <actor>` clears it (`--by`
mandatory). Hold and resume fire a best-effort `reconcile_project` / `reconcile_registry`
control notification with a 250ms timeout (`:10`).

## External loop

For runners whose work happens outside the harness (ChatGPT browser). Durable per-task counters
and policy events; caps default to 3 cycles, 2 repair continuations, 5 external threads, 8h
wall clock (`automation_external_loop.go:32`). `collect-external --runner chatgpt-browser
--job <id>` fetches artifacts, records a review packet, and stores patches as apply inputs;
`external-loop <task>` prints counters/caps/events read-only; `advance-external <task> --job
<id> | --event apply_failed|apply_succeeded|review_succeeded` applies loop policy and emits the
next action (`record_research_artifact`, `apply_patch`, `request_review_next`,
`continue_thread_with_failure`, `close_task`, `escalate_human` → nonzero exit). The daemon
auto-advances the same loop inside `pollOnce` (`daemon_external_loop.go`).

An external verdict never invents proof (`closeExternalLoopTask`). It flips the task's
**existing** verification rows in place — only rows whose covers resolve to real acceptance
IDs, and only from `pending`/`pass` to `pass` with the verdict summary as the note, so a
row's real `command:` check is never replaced by review prose. A `fail` row refuses the
close outright (`INVALID_TRANSITION`), as does a close whose task still has an unsatisfied
machine-owned `proof_required` and no covering command row
(`v7ExternalCloseMachineProofOutstanding`). Only when the task has no covering row at all
does it append one `manual proof:` row recording the external verdict.

## Event-log circuit and streams

An event-sink append failure trips a durable failure registry
(`event_log_persistence_failures`, CAS ≤32 attempts) that merges into the invariant circuit and
blocks dispatch; recovery probes each failure by priority, replays it, and clears it on success
(`daemon_event_log_failure.go`). Every run upsert goes through `upsertRunWithStream`, which
diffs before/after and publishes dispatch, lease-transition, task-status, and review-batch
events to the Serve broker with deep links (`daemon_stream.go`). Serve starts only for the
resident daemon and only when a serve target is configured; the pid file records the bound
address (`daemon_serve.go:30`).
