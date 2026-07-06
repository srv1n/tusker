# Prior-Art Playbook: Runtime Robustness Semantics for Tusker

Date: 2026-07-06. Author: planner session. Status: **binding design** for the runtime-hardening track (supersedes ad-hoc numbers in earlier docs where they conflict; complements `execution-stability-workstreams.md`, which remains the incident record and D1–D6 contract).

Provenance: synthesized from three commissioned source-level research reports in `docs/design/research/`:
- `research-infra-prior-art.md` — SQS/DLQ, Sidekiq, Temporal, Kubernetes controllers + client-go, Netflix Conductor, systemd.
- `research-harnesses.md` — OpenHands agent-sdk, opencode (sst), goose (Block), Aider. Plus two deep-dive passes (OpenHands lease/recovery internals; opencode state/retry/process model) whose findings are folded in below.
- `research-orchestrators.md` — vibe-kanban (BloopAI), claude-squad, Beads + Gas Town (Yegge), Conductor.build, Hermes identification.

Reports carry file-and-line citations into the actual upstream source; this document carries only the decisions.

---

## 1. The doctrine — five principles every system converged on

Independently, across job queues, Kubernetes, and every serious agent harness, the same five rules appear. These are now Tusker law:

**P1 — Never trust a persisted "running" flag.** Liveness is always *derived* at read time: heartbeat freshness + verified process identity + (where possible) an active probe. Disk records durable facts (attempts, exits, evidence, spend); "is it alive" is recomputed, never stored-and-believed. (OpenHands recomputes status live through the whole stack; opencode keeps busy-state memory-only; Gas Town cross-checks tmux against the ledger at query time; vibe-kanban declares any boot-time "Running" row definitionally dead.)

**P2 — Dispatch eligibility is one compare-and-swap against task state.** "Should this run?" is answered exactly once, in one shared function, enforced as a conditional update in the store transaction (`WHERE status IN ('ready','rework') AND unleased`), not check-then-act. A task in review/done/backlog/blocked is *structurally* un-dispatchable — the UPDATE touches zero rows. (Beads `ClaimIssueInTx`; Gas Town redispatch exit-code 3 "not in open status"; opencode's one-run-per-session-key coordinator; OpenHands' atomic `conversation_already_running` check-and-set.)

**P3 — Every loop is bounded, and the bound lands in a terminal parked state with manual redrive.** Separate counters for separate diseases: infra-failure retries (cap + backoff + jitter) and no-progress continuations (small cap, the guard our 88-attempt incident lacked). At the cap: a loud terminal state a human must explicitly redrive — never silent, never auto-requeued. (SQS `maxReceiveCount`→DLQ; Sidekiq 25→Dead set and super_fetch's 3-crashes-in-72h breaker; Conductor retryCount default 3 hard cap 10; goose `MaxAttemptsReached`; Gas Town max-attempts→escalate-to-Mayor; Aider `max_reflections=3`.)

**P4 — The dispatcher is a level-triggered reconcile loop.** Each tick: read desired (ledger: which tasks warrant exactly one active run; everything else warrants zero), read actual (leases, pids, processes), converge idempotently (reap, release, dispatch, park), and resync on a timer so a missed event can never strand state. Events only *wake* the loop; they are never themselves the state. (Kubernetes controller contract verbatim; Temporal's event-sourced replay is the same idea for state; vibe-kanban's boot sweep is the degenerate one-shot form.)

**P5 — Budgets are the defense against failure modes you haven't met yet.** Caps on attempts/tokens/wall-clock don't require knowing *how* the next loop happens — they bound its blast radius regardless. Only OpenHands (per-run USD hard stop checked every step) and Gas Town (model tiering, concurrency caps) have anything here; the category is wide open and Tusker's incident (1.63B input tokens) is the argument.

**Scoping decision that follows from the survey:** Tusker supervises at the **run/attempt** granularity. In-run step loops, doom-loop tool detection, and reflection caps belong to the runner harnesses (codex, claude) — they all have them. Tusker's job is the layer nobody else owns: many runs, one ledger, real money.

---

## 2. Semantics table — the numbers

Defaults to config; chosen for minutes-long, dollars-per-attempt agent runs. Justification column names the donor system(s).

| Concern | Value | Rule | Donor |
|---|---|---|---|
| Runner heartbeat interval | **15 s** | Runner (or its event ingester) beats the run record every 15 s. | OpenHands lease renew 15 s; Sidekiq 5 s. |
| Lease TTL | **60 s** | Derived-dead when `now − last_beat > TTL` (≈4 missed beats). | OpenHands 45 s; Conductor responseTimeout; Sidekiq TTL 60 s. |
| Reclaim grace | **2 × TTL** | Reconciler releases a lease only when expired > TTL past expiry (tolerates GC/clock skew); a mid-reclaim heartbeat wins. | Beads `ReclaimExpiredLeasesInTx` grace = 2×TTL. |
| Reconcile tick / resync | **30 s** | Full level-triggered converge every tick regardless of events. | K8s resync; Sidekiq orphan checker; TTL/2. |
| Per-attempt wall clock | **20 min** | Kill + fail the attempt even if heartbeating. | Temporal Start-To-Close; Conductor timeoutSeconds. |
| Per-task wall clock | **5 × per-attempt** | Task → parked when cumulative attempt time exceeds it. | Temporal Schedule-To-Close. |
| Infra retry cap | **5** (hard ceiling 10) | Worker-died / infra errors only; backoff `min(30 s × 2^(n−1), 15 min) + jitter`. | Conductor 3/10; Sidekiq formula + jitter; NOT Temporal's unlimited (each attempt costs money). |
| No-progress continuation cap | **3** | Clean-exit continuations that produce no new commits/evidence; separate counter from infra retries. | Sidekiq super_fetch 3-crash breaker; opencode doom-loop threshold 3; goose/Aider retry=3. |
| Re-dispatch cooldown | **10 min** | Minimum gap between re-dispatches of the same task. | Gas Town `deacon redispatch --cooldown 10m`. |
| Progress gate | commits or new accepted evidence | A continuation is only "progress" if the attempt changed the world; agent self-report doesn't count. | vibe-kanban `changes_committed` gate; goose objective shell success checks. |
| Per-attempt token budget | **50 M input / 500 k output** | Daemon kills the run on breach (usage events are already ingested; enforcement is a comparison). | OpenHands `max_budget_per_run` checked every step. |
| Per-task token budget | **5 × per-attempt** | Task → parked on breach. Would have tripped the 88-attempt incident at ~15 attempts. | OpenHands; Temporal Schedule-To-Close analogy. |
| Global daily ceiling | config, default on | Dispatcher circuit-opens (stops new dispatch, drains running, red banner in serve UI) — never silent overspend. | systemd StartLimit as breaker template; no surveyed system has this — Tusker leads. |
| Daemon self-supervision | launchd `KeepAlive` + `ThrottleInterval ≥ 10 s` (macOS); systemd `Restart=on-failure`, `StartLimitBurst=5/60 s`, `WatchdogSec=30` (Linux) | Watchdog beat driven by the reconcile tick, so "alive" means "reconciling", not "process exists". | systemd; Gas Town's Boot-checks-Deacon. |

Terminal states (new, loud, distinct): `parked_no_progress`, `parked_retry_exhausted`, `parked_budget`, `parked_wallclock`. All require explicit `tusker redrive <TASK-ID>` (records who/why) to become dispatchable again. Parked ≠ failed: evidence and worktree are retained.

---

## 3. Mechanism catalog — adopt / adapt / reject

**Adopt wholesale:**
1. **Lease with generation fencing** (OpenHands `conversation_lease.py`): lease carries `{owner, generation, expires_at, host, pid}`; every runner-side ledger write is guarded by generation; a superseded owner gets a hard error instead of corrupting state. Also the **owner-scoped heartbeat as stop signal** (Beads `HeartbeatIssueInTx`): the heartbeat UPDATE is scoped `WHERE lease owner = me AND task dispatchable`; the moment a task moves to review, the runner's next beat hits zero rows and the runner halts itself — the loop dies from *both* ends.
2. **CAS claim** (Beads `ClaimIssueInTx`): dispatch = one conditional UPDATE inside the store transaction; idempotent for the same owner; hard error for a second claimant.
3. **Loaded-as-running-means-crashed** (OpenHands + vibe-kanban): at daemon boot, any run record claiming "running" whose lease is unverified is dead by construction → mark interrupted + append a synthetic "interrupted by restart" event to the attempt log so resume history stays coherent. Our D3 adoption remains, but adoption now requires *verified-alive* (pid + start_time + fresh beat), not record-says-so.
4. **Level-triggered reconcile loop** (K8s): replaces the event-reaction dispatch path. Desired-run-set derivation is the same shared eligibility function P2 uses.
5. **PID-reuse guard** (opencode `daemon.ts`): never signal a pid without verifying it is still the recorded `{pid, start_time}` process; never trust bare pid-exists as alive. Gas Town's sharper lesson: verify the *agent binary*, not its shell wrapper.
6. **Bounded re-dispatch with cooldown + escalation** (Gas Town `deacon redispatch`): attempt counter + 10 min cooldown + cap → escalate (park + surface in review UI), never re-sling.
7. **Budget bulkheads** (OpenHands per-run ceiling, generalized to the 4 layers in §2).

**Adapt:**
8. **Runner decoupling** (opencode detached spawn / claude-squad tmux / OpenHands log-files-not-pipes). We will not take a tmux dependency (single-binary philosophy). Near term: accept that codex app-server runners are stdio-pipe-coupled and *document* daemon-stop as deliberate runner shutdown (RUN-T-0010 A4). Target state: a thin runner-wrapper process that owns the app-server child, writes events to durable sinks (files/store), is spawned detached with stdio ignored in its own process group, and is discovered/adopted via the lease — daemon death then costs nothing. This is its own contract (below), not a rider.
9. **Stuck detection** (OpenHands 5-pattern detector, thresholds 4/3/3/6; opencode doom-loop). At Tusker's granularity this collapses to the **no-progress continuation cap + progress gate** — we do not rebuild in-run semantic loop detection; the runner harnesses own that layer.
10. **Self-fencing daemon** (opencode register-watchdog): our flock guard already ensures one daemon; add the self-check — the reconcile tick re-verifies it still holds the flock/registration and stands down loudly if superseded.

**Reject, with reasons:**
- Temporal's `maximumAttempts=0` (unlimited) and Sidekiq's 25-retry default — correct for cheap idempotent jobs, wrong when each attempt costs dollars. Cap 5.
- opencode's retry-forever-with-observable-status — keep the observability (retry state visible in UI with next-attempt time), reject the cap-less schedule.
- tmux as process supervisor — right idea (survive orchestrator death), wrong dependency for a single-binary tool; the runner-wrapper achieves the same property.
- vibe-kanban's "never auto-resume anything" — too conservative for us; adoption of *verified-alive* runners (D3) is strictly better and already e2e-tested. We keep auto-adoption, but only with verification.

---

## 4. Gap analysis — current Tusker vs the doctrine

Already built and aligned (wave 1+2): D1 recorded `{pid,pgid,start_time}`; D3 adoption-on-restart; D4 first-event deadline + heartbeat freshness tiers; D5 failure-retry circuit breaker; D6 flock single-instance + durable lastPollAt; crash-recovery e2e suite.

Confirmed gaps, in dependency order:
1. Dispatch is edge-triggered and does not consult task eligibility (the 88-attempt incident). **RUN-T-0010** (already contracted, p0) patches the known holes: plan-gated dispatch, continuation cap, stale-lease release, `daemon stop`.
2. No lease TTL/renewal/fencing semantics — leases are set-once flags; heartbeat is observational, not authoritative; no generation fence; no owner-scoped stop signal. No level-triggered reconcile. → **RUN-T-0011**.
3. No budgets of any kind. Usage events are already ingested (`turn_usage_updated`), so enforcement is comparisons + terminal transitions + UI surfacing. → **RUN-T-0012**.
4. Runners die with the daemon (pipe coupling). → **RUN-T-0013** (runner wrapper), after 0011 since the wrapper is adopted via the lease.
5. Daemon runs bare — no launchd/systemd supervision, no watchdog beat. → packaging task, after 0011 provides the tick to drive the watchdog.

Sequencing: **RUN-T-0010 → RUN-T-0011 → RUN-T-0012 → RUN-T-0013 → packaging.** 0010 is deliberately not widened — it stops the bleeding with the daemon down; 0011 subsumes its patches into the structural loop (0010's tests must keep passing unchanged, which is the proof the rewrite preserved behavior).

## 5. What Tusker does that nobody surveyed does

Worth stating so we don't cargo-cult ourselves out of it: executable task contracts with acceptance-mapped proof, risk-tiered close policy, wave-boundary batch review, and (once RUN-T-0012 lands) real spend governance. The survey says the last one is genuinely novel in this category — vibe-kanban, claude-squad, Conductor, opencode, goose: zero spend controls. The $100/hour data point from the Gas Town research is the market's way of asking for it.
