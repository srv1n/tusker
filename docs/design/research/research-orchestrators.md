# Orchestrator / Task-Board Layer: Research for Tusker

Research date: 2026-07-06. Scope: systems that manage MULTIPLE coding-agent sessions
against tracked work items — Tusker's exact niche. Goal: mine concrete mechanisms for
Tusker's four wounds — (a) unbounded re-dispatch of tasks already in review, (b) stale
run leases for done/blocked tasks, (c) daemon death orphaning/killing runners, (d) zero
spend governance.

Sources read as actual source (shallow clones in scratchpad/repos): `vibe-kanban`
(BloopAI, Rust), `claude-squad` (smtg-ai, Go), `beads` (steveyegge, Go), `gastown`
(steveyegge, Go). Closed systems (Conductor) and Hermes read via docs/blog.

**Headline:** The single best-matched analog is **Gas Town** (Go + beads-backed +
tmux + role-specialized supervision + human review) — it independently implements
almost exactly the fixes Tusker needs. **Beads** supplies the durable lease/heartbeat/
reclaim primitive underneath it. **vibe-kanban** supplies the cleanest single-process
"reconcile orphans at boot" pattern. Read those three sections first.

---

## 1. vibe-kanban (BloopAI) — Rust, SQLite, resident server + Tauri/web UI

Closest OSS *architecture* analog: a persistent server process owns spawned agent
processes and a SQLite ledger; a web board is the review surface. This is Tusker's shape
(daemon + ledger + UI) but single-node and event-chained rather than autonomously
re-dispatching.

### Work-item state machine
- **`TaskStatus`** (`crates/db/src/models/task.rs:14`): `Todo, InProgress, InReview, Done,
  Cancelled`. Stored field on the task row, PATCH-updated via routes (not a live
  computation).
- The runtime unit is **not** the task — it's the **`ExecutionProcess`**
  (`crates/db/src/models/execution_process.rs:43`), with its own status
  **`Running, Completed, Failed, Killed`** and a **`run_reason`** enum
  (`:53`): `SetupScript, CleanupScript, ArchiveScript, CodingAgent, DevServer`.
- Data model chain: `Task → Workspace` (a git worktree, formerly "task_attempt") `→
  Session → ExecutionProcess`. A task can have several workspaces (parallel attempts);
  each execution is one spawned agent/script process.
- "Ready to dispatch" is **user-driven** — there is no dependency graph or auto-picker.
  A human (or MCP tool) creates a workspace/attempt and starts execution.

### Agent-session lifecycle
- Spawn: `LocalDeployment::start_execution_inner`
  (`crates/local-deployment/src/container.rs:1316`) launches the executor as a **process
  group** child (`AsyncGroupChild`), tracked in an in-memory `child_store: HashMap<Uuid,
  Arc<RwLock<AsyncGroupChild>>>`.
- Monitor: **two** watchers per execution (`spawn_exit_monitor`, `:480`):
  - `spawn_os_exit_watcher` (`:815`) polls `child.try_wait()` every **250 ms** and sends
    the real OS exit status.
  - An optional executor **exit-signal** channel — because some agents (Claude etc.)
    don't self-exit after finishing a turn, the executor signals "done" and the monitor
    then kills the process group. `tokio::select!` races the two.
- Detect-dead / cleanup: on exit it computes `Completed`/`Failed` from exit code, calls
  `ExecutionProcess::update_completion`, SIGKILLs any orphaned group children (e.g. MCP
  servers), removes the child from the store.
- **Process ownership:** the resident server owns the child processes directly. **Nothing
  survives a server restart** — children are in-memory only. This is the opposite of
  claude-squad/Gas Town (tmux-owned, survive restart) and is why the boot reconcile below
  is mandatory.

### Restart survival — the key pattern for Tusker's wound (b)/(c)
`cleanup_orphan_executions` (`crates/services/src/services/container.rs:272`) runs **at
every boot** (called from `crates/server/src/startup.rs:160` and `main.rs:76`):
```
find all ExecutionProcess WHERE status = Running   // impossible after a clean shutdown
  → update_completion(status = Failed, exit_code = None)
  → snapshot the repo HEAD OID so the diff is preserved
```
It **marks orphans Failed; it does NOT re-run them.** Any process that was "Running" when
the server died is definitionally orphaned and is reconciled to a terminal state on the
next boot. This is the direct antidote to "stale run leases for done/blocked tasks."

### Retry / re-dispatch policy — how it avoids Tusker's loop
There is **no retry loop.** Progress is a **static action chain**: each `ExecutorAction`
carries an optional `next_action` (`try_start_next_action`, `container.rs:1358`) — e.g.
setup-script → coding-agent → cleanup-script. On completion the monitor either advances
the chain or **finalizes** (`should_finalize`, `container.rs:204`):
- **Always finalize on `Failed` or `Killed`** (`:226`) — a failed action moves the task
  toward review; it is never re-run automatically.
- Chaining a coding-agent's next step is **gated on the agent having produced commits**:
  `should_start_next = changes_committed || has_commits_from_execution(...)`
  (`container.rs:581`). A no-op agent run does not trigger downstream work.
- Follow-ups are **user-queued messages** consumed only after the current process ends,
  and are **discarded if the prior run Failed/Killed** (`container.rs:623`, the
  `should_execute_queued` branch). You cannot pile follow-ups onto a broken run.
- `ExecutionProcess::was_stopped(...)` guards `update_completion` so a user-initiated stop
  is not overwritten by the exit monitor (`container.rs:542`).

### Human review flow
- Task → `InReview`; the human reviews the diff in the web UI, then merges. **`MergeStatus`**
  and both **direct merge** and **PR merge** are modeled (`crates/db/src/models/merge.rs`);
  `crates/services/src/services/pr_monitor.rs` polls GitHub PR status.
- **Per-tool-call approval gate** (`crates/services/src/services/approvals.rs`): when an
  agent requests a sensitive tool, a `PendingApproval` with a **`timeout_at`** is pushed to
  the frontend over WebSocket; a `spawn_timeout_watcher` resolves it to
  **`ApprovalOutcome::TimedOut`** (i.e. deny) if the human doesn't answer. Human-gated
  tool use with a fail-safe timeout.

### Budget / cost controls
**None.** No token/cost/spend ceilings anywhere in the tree. The only `max_attempts`
found are transient network retries (`executors/.../opencode/sdk.rs:1146`, auth handoff).
This whole product category has essentially no spend governance — Tusker is not behind
here, but no one is ahead either.

### Steal this (vibe-kanban)
- **Boot-time orphan reconcile** (`container.rs:272`): on daemon start, sweep every
  ledger row marked "running/leased" and force it to a terminal state (Failed/needs-review)
  — never resume it blindly. Cheap, and it directly kills stale leases from a crashed daemon.
- **Finalize-on-failure, never auto-retry** (`container.rs:204,226`): a failed attempt
  advances toward human review, it does not loop. Re-dispatch must be an explicit,
  separately-governed action, not the default exit path.
- **Gate the next step on evidence of progress** (`container.rs:581`): only chain forward
  if the agent actually produced commits. A task that made no diff should stop, not spin.

---

## 2. claude-squad (smtg-ai) — Go, tmux + git worktrees, TUI

A terminal manager for N parallel local agents (Claude Code, Codex, Gemini, Aider). No
ledger, no autonomous dispatch — a human drives everything from a TUI. Its value to Tusker
is the **tmux-as-durable-process** model and its **dead-session detection**.

### Work-item state machine
- **`Status`** (`session/instance.go:17`): `Running, Ready, Loading, Paused`. That's it —
  there is no "done/review/failed" and no dependency graph, because the human is the
  scheduler. No "ready to dispatch" computation.

### Agent-session lifecycle
- An **`Instance`** = one **tmux session** + one **git worktree** (`session/instance.go:31`).
  The agent runs *inside tmux*, not as a child of the Go process.
- Spawn: `Instance.Start` → `TmuxSession.Start` = `tmux new-session -d -s <name> -c <workdir>
  <program>` (`session/tmux/tmux.go:98`), then polls `DoesSessionExist` with exponential
  backoff until live.
- **Process ownership / restart survival:** because tmux owns the process, **the agent
  survives the TUI (orchestrator) dying.** On relaunch, `FromInstanceData`
  (`instance.go:110`) reattaches via `TmuxSession.Restore` = `tmux attach-session`
  (`tmux.go:183`); paused instances are reconstructed without restart. This is the
  strongest "survives orchestrator restart" story of any system here.
- **Detect-dead:** `DoesSessionExist` = `tmux has-session -t=<name>` with an **exact-match
  `-t=`** (a prefix match here is a real bug they fixed) (`tmux.go:458`). `TmuxAlive()` is
  the pre-attach sanity check. Activity/"needs input" is detected by hashing pane content
  (SHA-256) and diffing against the previous tick, plus string-matching prompt text like
  `"No, and tell Claude what to do differently"` (`HasUpdated`, `tmux.go:235`).
- Cleanup: `Kill` closes the PTY, `tmux kill-session`, and removes the worktree
  (`instance.go:277`). `CleanupSessions` reaps every session with the `claudesquad_` prefix
  (`tmux.go:487`). `Pause` (`instance.go:412`) commits dirty work locally, detaches (keeps
  scrollback), removes the worktree but **keeps the branch**; it explicitly handles the
  **orphaned-worktree** case (path/.git missing) by pruning metadata and transitioning to
  Paused for later recovery rather than erroring out.

### Daemon (AutoYes)
- Separate `--daemon` child process (`daemon/daemon.go`): loads instances from storage,
  polls each every `DaemonPollInterval`, and if `HasUpdated()` reports a pending prompt it
  `TapEnter()`s (auto-accept). PID written to `daemon.pid`; the main process kills the
  daemon on start and relaunches on exit; the daemon re-saves instances on SIGINT/SIGTERM.
- The daemon is **stateless-reloadable** — it reads instances from storage every launch, so
  daemon death loses no work (the tmux sessions keep running regardless).

### Retry / budget
**No retry policy and no budget controls — by design.** There is no autonomous
re-dispatcher, so there is no loop to bound. The human presses the buttons. The lesson for
Tusker is a design contrast (below), not a mechanism to copy verbatim.

### Human review flow
Inline in the TUI: per-instance diff view, "checkout to review," push/apply gated on the
human. No batching, no PR-centric flow.

### Steal this (claude-squad)
- **Put the agent under tmux (or an equivalent supervisor), not under the daemon.** Then
  daemon/orchestrator death cannot orphan or kill the runner (Tusker wound (c)); reattach on
  restart instead of respawn. `instance.go:110` + `tmux.go:183` is the whole pattern.
- **Exact-match liveness, and treat "pane process alive" as insufficient.** `has-session
  -t=` exact (`tmux.go:458`) plus content-hash activity detection (`tmux.go:235`). Gas Town
  independently learned the same lesson harder (a shell wrapper outliving the agent looks
  alive) — see §4.
- **Graceful orphaned-worktree recovery** (`instance.go:425-447`): if the worktree dir/.git
  is gone, prune and transition to a recoverable state instead of hard-failing.

---

## 3. Beads (steveyegge) — Go, JSONL + SQLite/Dolt, dependency-aware issue tracker for agents

Beads is the closest analog to **Tusker's ledger** specifically. It is CLI-first (`bd`),
git-backed, and explicitly designed as agents' external memory. It contributes the single
most directly-applicable primitive in this whole report: a **durable lease with heartbeat
and reclaim.**

### Work-item state machine + ready-work eligibility
- **`Status`** (`internal/types/types.go:329`): `open, in_progress, blocked, closed`
  (grouped by `StatusCategory`).
- **Ready-work** is computed in SQL (`internal/storage/sqlbuild/ready.go:86`,
  `BuildReadyWorkWhere`). An issue is ready when:
  - `status IN ('open','in_progress')`
  - **`is_blocked = 0`** — a **materialized** flag, not a live dependency subquery.
    Dependency satisfaction is precomputed and stored (`issueops/blocked_state.go`,
    `blocked_merge.go`, `blocked_consistency.go`); when a blocker closes, `is_blocked` is
    recomputed. `DependencyType.AffectsReadyWork()` (`types.go:851`) decides which edge
    types actually gate (blocks vs. related).
  - not `pinned`, not deferred (`defer_until` passed), not a deferred-parent's child, and
    excludes epic/meta issue types.
- Multi-agent claim is a **compare-and-swap** (`issueops/claim.go:31`, `ClaimIssueInTx`):
  `UPDATE ... SET assignee=?, status='in_progress' WHERE id=? AND status='open' AND (assignee
  empty/null/self)`. 0 rows → `ErrAlreadyClaimed` / `ErrNotClaimable`. Re-claim by the same
  actor is **idempotent** (supports agent retry). `ClaimReadyIssueInTx` (`:125`) computes
  ready-work and claims the first claimable one **in the same transaction** — no
  check-then-act race.

### Lease / heartbeat / reclaim — the mechanism Tusker is missing (wound (b))
`internal/storage/issueops/lease.go`:
- **`DefaultLeaseTTL = 5m`** (`:21`). Claiming stamps `lease_expires_at = now + TTL`,
  `heartbeat_at = now`.
- **`HeartbeatIssueInTx`** (`:96`): pushes `lease_expires_at` forward. Crucially it only
  affects rows `WHERE status='in_progress' AND assignee=<actor>` — so a heartbeat from a
  non-owner, or on an issue that's already closed/reclaimed, touches **0 rows** and returns
  **`ErrNotClaimable`**. That is the signal a live worker uses to learn **"my lease is gone,
  stop working."** (Exactly what should stop a Tusker runner whose task was moved to review
  underneath it.)
- **`ReclaimExpiredLeasesInTx`** (`:146`): reverts `in_progress` issues whose
  `lease_expires_at < cutoff` back to `status='open'` (clears assignee/started_at/lease) and
  emits an `EventLeaseReclaimed` recovery event. The supervisor passes **`cutoff = now −
  graceWindow`, `graceWindow = 2 × TTL`** — only reclaim leases that expired a safe margin
  ago, so a worker briefly paused for GC/clock-skew isn't stolen from. Reclaim re-checks the
  predicate inside the `UPDATE`, so a heartbeat that lands mid-reclaim wins and the issue is
  simply skipped.
- **`row_lock` (`:63`) — the anti-zombie-merge keystone.** On Dolt (which merges concurrent
  commits cell-by-cell with no real row locks), a heartbeat writing `heartbeat_at` and a
  close writing `status` touch *different cells* and would silently merge — letting a reclaim
  quietly revert an issue the owner just closed. Every status/ownership/lease-mutating path
  rewrites one shared random `row_lock` cell, forcing those writers to **collide** and
  surface a serialization conflict that gets replayed. Invariant documented in the file: any
  path mutating status/assignee/started_at/lease MUST rewrite `row_lock`. (Tusker's ledger is
  a markdown/JSON file merged by git — the *same* cell-merge hazard exists; this is a
  cautionary pattern.)
- `bd reclaim` is a **CLI command / cron step**, not a resident daemon (`cmd/bd/reclaim.go`;
  `--older-than` default = 2×TTL grace, `--older-than 0s` reclaims all currently-expired).
  A supervisor above beads (Gas Town, §4) drives it.

### Human review / budget
Beads is a tracker, not a runner — review and budget live in whatever orchestrates it. No
budget controls in beads itself.

### Steal this (Beads)
- **Lease + heartbeat + grace-window reclaim** (`lease.go` whole file). This is the direct
  fix for stale run leases: a running Tusker task holds a TTL lease; the runner heartbeats;
  the daemon reclaims only leases expired past `2×TTL`. A crashed daemon or a killed runner
  self-heals on the next reclaim sweep — no manual cleanup.
- **Owner-scoped heartbeat as a stop signal** (`lease.go:96`): the heartbeat `UPDATE`
  scoped to `assignee=self AND status=in_progress` means the moment a task leaves
  `in_progress` (moved to review, cancelled, reclaimed), the runner's next heartbeat returns
  0 rows / `ErrNotClaimable` — a cheap, race-free "you no longer own this, halt" that would
  have stopped the 88-attempt loop cold.
- **CAS claim + idempotent re-claim** (`claim.go:31`): dispatch is a conditional UPDATE that
  only succeeds if the task is still `open` and unclaimed. Two dispatchers cannot both grab
  it; re-dispatching an already-in-progress task by the same actor is a safe no-op, and by a
  *different* actor is a hard `ErrAlreadyClaimed`. This structurally forbids
  "dispatch a task that's already being worked / already in review."
- **Materialized `is_blocked`** (`ready.go:96`): compute dependency-readiness once and store
  it; recompute on blocker-close. Keeps `tusker next` cheap and makes "ready" a single
  indexed predicate.

---

## 4. Gas Town (steveyegge/gastown) — Go, beads-backed, tmux, role-specialized supervision

**The closest thing to Tusker that exists**, and independently converged on the fixes Tusker
needs. Yegge's multi-agent orchestrator layered on beads: a "Town" (`~/gt/`) coordinates
many project "Rigs"; 20–50 agents run in parallel in tmux + isolated git worktrees.
Supervision is **factored into dedicated agent roles** rather than a single loop.

### Roles (from `docs/glossary.md`, `templates/*-CLAUDE.md`)
- **Mayor** — town-level chief-of-staff: initiates Convoys (batches of work), coordinates
  distribution, notifies the user, and is the **escalation target** when automated recovery
  gives up.
- **Polecat** — the worker. **Persistent identity, ephemeral session**: a permanent agent
  bead + CV chain + work history that survives, but the tmux session and sandbox are spawned
  per task and cleaned on completion. Runs in an isolated worktree.
- **Refinery** — owns the **Merge Queue** for a Rig: merges Polecat branches, resolves
  conflicts, enforces quality gates before `main`.
- **Witness** — per-Rig **patrol** agent: oversees Polecats + Refinery, **detects stuck/dead
  agents**, and triggers recovery (resets the abandoned bead, mails the Deacon).
- **Deacon** — town-level **"daemon beacon" watchdog**: runs continuous Patrol cycles,
  ensures worker activity, and drives re-dispatch/recovery.
- **Dogs** — the Deacon's maintenance crew (cleanup, health checks, feeding stranded work).
- **Boot (the Dog)** — a special Dog that **checks the Deacon every 5 minutes** — i.e. it
  **watches the watchdog**, a deliberate chain of accountability so the supervisor's own
  death is detected.

### Polecat lifecycle state machine (`internal/polecat/types.go:28`)
`working, idle, done, review-needed, stuck, stalled, zombie`. The failure taxonomy is the
sharp part:
- **`stuck`** — the polecat **self-reports** it needs help.
- **`stalled`** — **externally detected**: beads still say `working` (hooked bead, assigned
  issue) but the tmux session/agent process is dead (OOM, disk-full, killed without cleanup).
- **`zombie`** — a tmux session exists with **no corresponding worktree** (incompletely
  nuked / session-name mismatch), OR it called `gt done` but cleanup failed and it's stuck in
  `done`.
- Detection is **at query time by cross-checking tmux liveness against beads state**
  (`types.go:25`). The Witness also detects via monitoring (tmux state, age-in-`done`).

### Liveness detection (`internal/polecat/session_manager.go`)
- Heartbeat-based: `TouchSessionHeartbeat` on spawn (`:627`); `isSessionStale` checks whether
  the tmux pane process died (`:644`).
- **Hard-won lesson (`:350-362`, `:611-617`):** "heartbeat-fresh + pane-PID-alive can hide a
  dead agent — the pane process is often a shell/wrapper that outlives the agent." So zombie
  detection uses `IsAgentAlive` (actual `["node","claude"]` process detection via recorded
  `pane_id`) directly, not just the heartbeat/pane path. Same trap claude-squad's exact-match
  check hints at, encountered at depth.
- Before destroying anything, the Witness computes a **`WorkstateDisposition`**
  (`internal/polecat/workstate.go`): `Verdict = SAFE_TO_NUKE` / `SafeToNuke bool` — a
  guard that the abandoned work is safe to discard before `auto-nuke`.

### Retry / re-dispatch policy — the exact anti-loop design Tusker needs (wound (a))
`gt deacon redispatch <bead-id>` (`internal/cmd/deacon.go:278`). When the Witness finds a
dead polecat with abandoned work, it **resets the bead to `open`** and mails
`RECOVERED_BEAD` to the Deacon, which then:
1. **Tracks re-dispatch state** — attempt count *per bead* (`gt deacon redispatch-state`).
2. **Rate-limits with a cooldown between re-dispatches** (`--cooldown 10m`), exit code **2 =
   "in cooldown, try later."**
3. **If under `--max-attempts` (default 5): re-sling** to an available polecat.
4. **If over the limit: escalate to the Mayor instead of re-slinging** — the terminal state
   that *stops the loop* (hand to a coordinator/human, don't retry forever).
5. Exit code **3 = "skipped (already claimed or non-open status)"** — refuses to re-dispatch
   a bead that isn't still `open`. This is precisely the guard whose absence gave Tusker
   88 attempts on tasks already in review.
- Companion `gt deacon feed-stranded` (`:319`) has the same shape for stranded convoys:
  **per-cycle limit (default 3)** + **per-convoy cooldown (default 10m)**.
- Dolt writes use bounded exponential backoff with jitter: `doltMaxRetries = 10`, `base *
  2^(attempt-1) * (1 ± 25%)` capped (`internal/polecat/manager.go:38,54`). Session
  startup-nudge retries are capped via `StartupNudgeMaxRetriesV()` (`session_manager.go:937`).

### Budget / cost governance — the only real spend controls in this whole survey (wound (d))
- **Model tiering by role** (`internal/cmd/config.go:112`): *"budget: Patrol roles use
  Haiku, workers use Sonnet."* Supervision/health agents run on a cheap model; only workers
  get the expensive one.
- **`gt start --cost-tier standard/economy/budget`** (`internal/cmd/start.go:137`): per-
  session cost tier.
- **`gt sling --max-concurrent N`** (`internal/cmd/sling.go:132`): throttle spawn rate —
  cap how many agents dispatch at once.
- **`gt quota`** (`internal/cmd/quota.go`): scans tmux panes for rate-limit indicators and
  reports which accounts are available / rate-limited / in cooldown; detects blocked sessions.
- Reality check on why this matters: a DoltHub walkthrough clocked **~$100 in Claude tokens
  for a 60-minute session** — cost governance is not theoretical.

### Human review flow
The Refinery role gates merges into `main` (merge queue + conflict resolution + quality
checks). `review-needed` is an explicit polecat state. Batching is the "Convoy" unit the
Mayor initiates. This maps cleanly onto Tusker's wave-boundary batch review.

### Steal this (Gas Town) — highest-value section for Tusker
- **Bounded re-dispatch with attempt-count + cooldown + escalation** (`deacon.go:278`):
  never re-sling more than `max-attempts`; enforce a cooldown between attempts; on exhaustion
  **escalate to a human/Mayor, do not loop.** This is the literal fix for the 88-attempt
  blowup. Pair with exit-code semantics: **"skipped — not in open status" (code 3)** so a
  task in review/done is structurally un-re-dispatchable.
- **Dedicated watchdog role that watches the watchdog** (Deacon + Boot-checks-Deacon-every-
  5m). Tusker's daemon death is unmonitored; a tiny external liveness check on the daemon
  (systemd watchdog, a cron `bd reclaim`, or a "Boot" ping) closes wound (c) without
  re-architecting.
- **The stuck / stalled / zombie taxonomy** (`polecat/types.go`): distinguish *self-reported
  stuck* from *externally-detected dead* from *orphaned-session* — each wants a different
  recovery (nudge vs. reclaim+redispatch vs. nuke). A single "failed" bucket can't route
  recovery correctly.
- **"Agent process alive" ≠ "pane/PID alive"** (`session_manager.go:350-362`): verify the
  actual agent binary is running, not the shell wrapper, before declaring a runner healthy —
  otherwise heartbeats lie.
- **Cost tiering by role** (`config.go:112`): run Tusker's own orchestration/supervision
  agents on a cheap model; reserve the expensive model for implementation runners. Plus a
  spawn concurrency cap (`--max-concurrent`) and a `quota`-style rate-limit scanner.

---

## 5. Conductor (conductor.build) — closed-source Mac app, parallel Claude Code in worktrees

Commercial desktop app; no source. Extracted from docs/changelog/troubleshooting.

- **Model:** parallel Claude Code agents, each in its own git worktree, all threads visible
  at once (progress, diffs, test results). Constraint stated explicitly: *"A Git branch can
  only be checked out in one worktree at a time"* — one-attempt-per-branch, same as everyone.
- **Failure handling:** error handling for individual agents covering **hangs, crashes, and
  auth failures**; changelog notes fixing agents that *"hang without responding"* and
  **terminating "zombie processes from failed Claude installations"** with clearer errors.
  Troubleshooting guidance for a stuck agent is manual: wait for the running command, check
  if it's awaiting approval/input, cancel the response, or restart the chat; **checkpoints**
  can *"revert both code changes and later chat state."*
- **Retry:** thin — a fixed bug where *"resending a message with a failed review-comment
  attachment did not retry the upload."* No evidence of an automatic bounded-retry policy or
  spend caps.
- **Review flow:** per-agent **approval gates** — *"review the requested action before
  approving it"* for shell/tool/MCP/web/file actions, with deny / "ask for a different
  approach." Notifications when an agent needs input (plan review, questions). Workspaces
  history filterable by repo/branch/PR.
- **Budget:** nothing documented.

**Thinness admitted:** with no source, the internal supervision/retry mechanics aren't
verifiable. What's confirmed is a per-action human approval gate + checkpoint-based revert +
one-branch-per-worktree — consistent with the others, no novel mechanism to steal.

Sources: conductor.build/docs/troubleshooting/issues, /changelog, grokipedia Conductor.build.

---

## 6. "Hermes agent" — identification

**Most plausible referent: Nous Research's *Hermes Agent*** — an open-source **agent
*harness*** (persistent cross-session memory, auto-generated skill documents, model-agnostic,
message-driven; "sessions as infrastructure," lineage-based context compression, long-running
across CLI/messaging/scheduled execution). Launched Feb 2026, ~27k+ GitHub stars by
Apr 2026 (v0.9 "everywhere release"). It's frequently named alongside OpenClaw/Devin as an
"always-on" coding agent.

**Verdict for Tusker's purposes: it's the wrong layer.** Hermes is a **single-agent
runtime/harness** — the layer that turns one model into one capable long-running agent
(comparable to Claude Code or codex themselves, i.e. the thing Tusker *dispatches*), **not a
multi-agent task-board orchestrator** that schedules many sessions against a tracked ledger.
Its relevant-to-Tusker ideas are internal-loop hygiene (context compression, durable session
state, skill docs), not orchestration/supervision. If "Hermes agent" in the prompt meant a
*harness* to run under Tusker, it fits as an executor; if it meant an *orchestrator* peer to
Tusker, there isn't a well-known one by that name — say so plainly.

Sources: arize.com/blog/how-hermes-implements-open-source-agent-harness-architecture,
atalupadhyay.wordpress.com Hermes Agent, prommer.net always-on AI coding agents.

---

## Consolidated "Steal this" — ranked for Tusker's four wounds

1. **(a) Unbounded re-dispatch → bounded re-dispatch with escalation.** Adopt Gas Town's
   `deacon redispatch` contract: per-task attempt counter + inter-attempt cooldown +
   `max-attempts` cap; on exhaustion **escalate to human, never loop**; and a hard
   precondition that refuses dispatch unless the task is still in an `open/ready` status
   (Gas Town exit-code-3; Beads CAS claim `WHERE status='open'`). A task in `InReview`/`Done`
   must be structurally un-dispatchable. (`gastown/internal/cmd/deacon.go:278`,
   `beads/internal/storage/issueops/claim.go:31`)

2. **(b) Stale run leases → TTL lease + heartbeat + grace-window reclaim.** Adopt Beads'
   `lease.go` wholesale: claim stamps `lease_expires_at`; runner heartbeats; daemon reclaims
   only leases expired past `2×TTL`. The owner-scoped heartbeat doubles as a **stop signal**
   (`ErrNotClaimable` the instant the task leaves `in_progress`). Reclaim reverts the task to
   ready and emits a recovery event. (`beads/internal/storage/issueops/lease.go:21,96,146`)

3. **(c) Daemon death orphaning runners → decouple runner from daemon + reconcile at boot +
   watch the watchdog.** Run agents under tmux/an external supervisor so daemon death can't
   kill them (claude-squad `instance.go:110` reattach). On daemon boot, sweep every "running"
   ledger row to a terminal state and self-heal via reclaim (vibe-kanban
   `container.rs:272`). Add an external liveness check on the daemon itself (Gas Town's Boot
   pings Deacon every 5m). Distinguish stuck/stalled/zombie so recovery routes correctly
   (`gastown/internal/polecat/types.go:28`), and verify the *agent* process is alive, not
   just its shell wrapper (`session_manager.go:350`).

4. **(d) Zero spend governance → role-tiered models + concurrency cap + quota scanner.**
   Cheapest concrete wins from Gas Town: run supervision/orchestration on a cheap model,
   reserve the expensive one for runners (`config.go:112`); cap concurrent dispatch
   (`sling --max-concurrent`); scan sessions for rate-limit/cooldown state (`gt quota`); add a
   per-task/per-wave token-or-cost ceiling that trips escalation (nobody in this survey has
   the last one — it's Tusker's opportunity to lead, and the $100/hour DoltHub data point
   shows why it's needed).

### One-line contrasts worth remembering
- The systems that **never loop out of control are the ones with no autonomous
  re-dispatcher** (claude-squad, vibe-kanban) — autonomy is exactly what Tusker adds, so
  Tusker *must* import Gas Town/Beads' governance to earn that autonomy safely.
- **tmux-owned runners survive orchestrator restarts; child-of-daemon runners do not** — pick
  tmux (or a real supervisor) if daemon death must not kill work.
- **Reconcile-at-boot (vibe-kanban) and reclaim-on-timeout (beads) are complementary**: the
  first cleans up after a crash you already suffered; the second bounds how long any single
  stale lease can persist while running.
