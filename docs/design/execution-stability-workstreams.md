# Execution stability: process ownership contract and parallel workstreams

Date: 2026-07-06 · Status: proposed, operator review pending · Author: planner session

## 1. Why the same failure keeps recurring

Three incidents in one week, superficially different:

1. **2026-07-06, this repo**: the tusker daemon process died silently around 07:29Z. Its four runs kept `lease_state: running` with dead pids for 4+ hours; the queue stayed blocked at 4/4 capacity by ghosts; the reviewer lane never fired; SRV-T-0002 (p0) never dispatched. Nothing alerted.
2. **Adjacent project, same week**: a Codex lane sat "active" for 3.5 hours while the spawned process was blocked on stdin and had never started — worktree untouched, no compiler running, record green the whole time.
3. **This repo, before the daemon died**: RUN-T-0001 reached 72 attempts and RUN-T-0007 reached 38 — retry loops with no circuit breaker, churning time and tokens without converging.

These are one defect, not three: **process state is asserted at spawn time and never verified afterward.** A lease says "running" because we wrote "running" when we dispatched, not because anything checked a process since. There are four failure classes and today's system has no defense against any of them:

| Class | Example | Root gap |
|---|---|---|
| Dead but marked running | Today's four ghost runs | No reconciler comparing claimed state to observed processes |
| Alive but stuck | The 3.5h stdin wedge | Liveness conflated with progress; no first-event deadline, no heartbeat enforcement |
| Unbounded retry | 72 attempts on RUN-T-0001 | No terminal state, no backoff cap |
| Supervisor unsupervised | Daemon died, nothing noticed | No single-instance guard, no daemon heartbeat, no external restart |

Each past fix was applied at the incident site — kill and relaunch, plug the stdin pipe — without installing the invariant that would prevent the class. That is why it keeps coming back. The remedy is a small set of invariants enforced in one place, written down first so every runner adapter and every future feature is built against them.

## 2. The process ownership contract

These are the decisions. They answer "if the daemon dies, who owns the processes, do they die too, can we recover by respawning." Adjustable at review; after that they are law.

- **D1 — Ownership is recorded, never assumed.** Every spawned runner is recorded as a `(pid, pgid, start_time)` triple in the run row. A pid alone is never trusted (pid reuse). The daemon is the supervisor of record for everything it spawns.
- **D2 — Runners survive daemon death.** Runners are spawned in their own process group, stdio redirected to files, no TTY inheritance, stdin from `/dev/null` unless the adapter explicitly wires a pipe (and an adapter that needs a handshake must fail fast if it doesn't complete). They write events to their durable per-run event sinks regardless of whether the daemon is alive. Rationale: in-flight agent work is expensive; the supervisor dying must not destroy it.
- **D3 — Recovery is adoption, not amnesia.** On startup (and on every poll, not just startup), a reconciler scans every lease claiming `running` and checks process identity: live and matching → re-attach and resume supervision; dead or mismatched → mark `interrupted`, preserve the attempt history, free the capacity slot. A restarted daemon therefore recovers the fleet instead of stacking ghosts.
- **D4 — Liveness is not progress; both are enforced.** Two deadlines: a **first-event deadline** (no first event within N minutes of spawn → kill the process group, mark failed with error "never started") and **heartbeat freshness** (fresh < 60s, stale 60–120s, dead ≥ 120s — the same thresholds the serve UI displays). The watchdog kills alive-but-silent runs past the dead threshold. This deadline alone would have caught the 3.5-hour stdin wedge in minutes.
- **D5 — Retry has a circuit breaker.** Bounded attempts with exponential backoff; exceeding the bound sets `terminal: true` with the final error text preserved. Terminal runs stop consuming capacity and surface to the human (needs-me signal 4). No more 72-attempt loops.
- **D6 — The daemon supervises itself.** Single-instance guard (flock/pidfile — a second `tusker daemon` exits loudly). A durable daemon heartbeat (`lastPollAt` written every poll) so any external observer — CLI, serve UI, launchd — can distinguish alive from wedged by age, never by a stored boolean. Optional launchd KeepAlive plist for auto-restart on the operator's machine.

## 3. Workstreams

Three streams, disjoint file ownership, all dispatchable today in parallel Codex desktop sessions. Each stream keeps its own slice compiling and skips the full suite; the planner session runs one central gate after convergence (per the fan-out doctrine). The tusker daemon stays **down** until Stream A lands — Tusker remains the ledger (contracts, status, proof) but not the executor during this phase.

### Stream A — Supervision and lifecycle (the contract, implemented)

- **Maps to ledger tasks**: RUN-T-0004 (lifecycle hardening), RUN-T-0008 (single-instance guard), RUN-T-0007 (retry capacity). Read those contracts first; this doc's §2 is the binding design they implement.
- **First act — salvage, don't restart**: the dead runs left real uncommitted work in their worktrees under `~/Library/Application Support/tusker/workspaces/tusker/` (RUN-T-0004: 27 dirty files, RUN-T-0007: 16, RUN-T-0008: 18). Diff each against main, keep what is sound, discard retry-loop churn. Also reconcile the ledger: mark the four ghost runs interrupted so records match reality.
- **Owns**: daemon, runtime-store, and runner-adapter packages in the Go tree, plus their tests.
- **Off-limits**: `internal/serve/**`, `docs/**` (except a knowledge-delta note), any new serve API files.
- **Deliverables**: D1–D6 implemented; the ghost-run reconciler; first-event deadline; heartbeat watchdog; retry circuit breaker with terminal state; single-instance guard; stdin/`/dev/null` spawn rule in every runner adapter.

### Stream B — Serve read-only backend (real data behind the UI)

- **Maps to ledger task**: SRV-T-0002 — the contract is final and binding as written (acceptance A1–A7). Interim API contract: `internal/serve/ui/BACKEND-GAPS.md` §4 endpoint table + `internal/serve/ui/src/types/domain.ts` shapes + `docs/design/serve-ui-supplement.md`.
- **Owns**: a new serve package, its cmd wiring, go:embed of built assets, and `internal/serve/ui/src/lib/api.ts` seam flips (+ `src/types/domain.ts` only where BACKEND-GAPS §3 requires new fields).
- **Off-limits**: daemon lifecycle internals (read-only queries against the runtime store are fine); no mutating endpoints.
- **Note on coupling**: fields that Stream A creates (terminal flag, heartbeat timestamps) are already named in the contract — serve them as explicit nulls until A lands. Never invent values. The streams stay parallel.

### Stream C — Crash-recovery end-to-end suite (prove §2, black-box)

- **Purpose**: the operator's stability questions become executable scenarios. Written against the §2 contract, through the public CLI surface only (`tusker automation status/queue --json`, spawning and killing real processes) — never against daemon internals, so it cannot collide with Stream A's files.
- **Owns**: new e2e/chaos test files and any test-fixture harness they need (a fake runner binary that can be told to wedge, heartbeat, or exit is in scope).
- **Off-limits**: all production source files.
- **Scenarios (minimum)**: (1) `kill -9` the daemon mid-run → runner survives → restarted daemon adopts the live run and collects its result; (2) runner dies → next poll marks it interrupted and frees capacity — no ghost; (3) runner never emits a first event (stdin-wedge simulation) → killed at the deadline, marked failed "never started"; (4) second `tusker daemon` start → exits loudly, first instance unaffected; (5) retries exceed the cap → terminal flag set, error preserved, capacity freed.
- These tests are expected to be **red until Stream A lands**; they run at the central gate and become the permanent regression wall for this failure family.

## 4. Sequencing, gate, and review

1. Dispatch A, B, C now, in parallel, one Codex desktop session each (§6 prompts).
2. Streams report converged (compiling slice + file list). No stream runs the full suite.
3. **Central gate, run once by the planner session**: `go build ./... && go vet ./... && make check`, `bun --cwd internal/serve/ui run build`, then Stream C's e2e suite against the new daemon.
4. One wave-boundary review batch for the operator — not per-task gates.
5. End-to-end trial: start the daemon under the new lifecycle, dispatch a real task, `kill -9` the daemon mid-run, restart, watch adoption; open the serve UI on real data.
6. Only after the trial does the daemon return as executor.

## 5. Ledger and operating rules during this phase

- The four ghost runs are marked interrupted as part of Stream A's first act (or by the planner before dispatch if capacity accounting matters sooner).
- RUN-T-0001 (Claude runner parity, 14 dirty files in its worktree) is **off the stability path**: salvage its diff to a branch, park the task. It resumes after the trial.
- CLN-T-0001 (in review, low risk) is handled by the planner under the batch-review policy; CLN-T-0002 stays parked.
- Status flips continue through the tusker CLI; contracts stay the source of truth. The daemon being down changes who executes, not how work is recorded.

## 6. Dispatch prompts (one per Codex desktop session)

Common rules for all three (include in each prompt): work in this repo (`/Users/sarav/Downloads/side/tusker`); if your environment gives you an isolated worktree, commit to a branch named for your stream; if you are in the main checkout, leave changes uncommitted and report your full file list. Make your slice compile (`go build ./...` or the UI build); do not run the full test suite — a central gate runs once after all streams converge. Never add AI attribution to any commit, message, or metadata. Do not touch files outside your ownership list.

### Prompt A

> Read `docs/design/execution-stability-workstreams.md` §2 and §3 Stream A, then the task contracts `.tusker/work/tasks/RUN-T-0004.md`, `RUN-T-0007.md`, `RUN-T-0008.md`. Implement the process ownership contract D1–D6 in the daemon/runtime-store/runner-adapter packages. First: salvage the dead worktrees under `~/Library/Application Support/tusker/workspaces/tusker/` (RUN-T-0004, RUN-T-0007, RUN-T-0008) — diff against main, reuse sound work, discard churn — and mark the four ghost runs (RUN-T-0001/0004/0007/0008) interrupted so the ledger matches reality. You own the daemon-side Go packages and their tests; `internal/serve/**` and docs are off-limits. Deliver: recorded (pid,pgid,start_time) ownership, runner survival of daemon death, adoption-on-restart reconciler (also every poll), first-event deadline, heartbeat watchdog, retry circuit breaker with terminal state, single-instance guard, stdin-from-/dev/null spawn rule. Make it compile; skip the full suite; report your file list.

### Prompt B

> Execute `.tusker/work/tasks/SRV-T-0002.md` exactly as written (acceptance A1–A7). The binding API contract is `internal/serve/ui/BACKEND-GAPS.md` (§4 endpoints, §3 missing fields) with `internal/serve/ui/src/types/domain.ts` as wire shapes, aligned with `docs/design/serve-ui-supplement.md`. Read-only JSON API over the SQLite runtime store and vault markdown; `/api/needs` computed server-side with the closed five-signal rule; go:embed the built SPA; flip `src/lib/api.ts` per endpoint in the §4 flip order. Fields the daemon does not yet supply (terminal flag, heartbeat timestamps) are served as explicit nulls — never invented. You own the new serve package, its cmd wiring, and the UI seam files; daemon lifecycle internals are off-limits (read-only store queries are fine). Zero mutating endpoints, localhost bind, no runtime CDN. Make Go and the UI build; skip the full suite; report your file list.

### Prompt C

> Read `docs/design/execution-stability-workstreams.md` §2 and §3 Stream C. Build a black-box crash-recovery e2e suite for the tusker daemon, driven only through the public CLI (`tusker automation status/queue --json`) and real process manipulation — no imports of daemon internals. Build a fake-runner test binary that can be instructed to wedge before first event, heartbeat normally, or exit with a code. Cover at minimum: daemon kill -9 mid-run with runner survival and adoption on restart; dead runner marked interrupted on next poll; never-started runner killed at the first-event deadline; second daemon instance exiting loudly; retry cap producing a terminal run with preserved error. You own only new test files and the fixture harness; all production source is off-limits. The suite is expected red until Stream A lands — structure it so each scenario maps to one §2 decision (D1–D6). Make it compile; report your file list.
