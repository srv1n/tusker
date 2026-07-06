# Process supervision, loop-prevention, and budget governance in OSS coding-agent harnesses

Design research for Tusker. Method: shallow-cloned the four target repos and read the actual
source. All file paths below are repo-relative to each project. Constants are quoted from the
source as of the `main`/`HEAD` I cloned (July 2026).

Tusker's five production failure classes, referenced throughout as **F1–F5**:

- **F1** runner process dead but marked running
- **F2** unbounded clean-exit continuation retries (task went to review, run lease stayed active,
  daemon re-dispatched forever — 88 attempts / 1.63B input tokens)
- **F3** daemon died silently leaving ghost runs
- **F4** runners pipe-coupled to daemon stdio; died when the daemon was SIGTERMed
- **F5** no spend/budget governance at all

---

## 1. OpenHands (OpenHands/software-agent-sdk — the V1 SDK)

Important: the classic `OpenHands/OpenHands` repo has been gutted into an `app_server` shell; the
real agent runtime now lives in the separate **`OpenHands/software-agent-sdk`** monorepo, published
as the pip packages `openhands-sdk` and `openhands-agent-server` (v1.31.1 pinned by the app).
Everything below is from that SDK repo. The two packages inside it are:
`openhands-sdk/` (the agent loop + conversation state) and `openhands-agent-server/` (the resident
HTTP server that owns conversations, with a **lease** for multi-instance ownership).

### State model
- **Event-sourced.** Canonical state is `ConversationState`
  (`openhands-sdk/openhands/sdk/conversation/state.py`), an append-only event log optionally
  persisted to disk (`persistence_dir`; file-backed events, `cache_limit_size=max_iterations`).
- A single enum drives all control: `ConversationExecutionStatus` with values
  `IDLE, RUNNING, PAUSED, FINISHED, STUCK, ERROR, WAITING_FOR_CONFIRMATION`
  (`state.py:59` etc.).
- **Control is recomputed-from-state each cycle, not event-reactive.** The run loop re-reads
  `execution_status` at the top of every iteration under a state lock (see below). The stuck
  detector re-derives "am I stuck" by re-scanning the tail of the event log every step.

### Dispatch / loop eligibility
- Single loop: `LocalConversation.run()` in
  `openhands-sdk/openhands/sdk/conversation/impl/local_conversation.py:~1638-1759`. It is a
  `while True:` that **acquires the conversation state lock each iteration** (`with self._state:`)
  and recomputes termination from `execution_status`. The lock is what lets concurrent
  `send_message()` / `pause()` interleave safely.
- Termination conditions, all checked every cycle (in order):
  1. `PAUSED` or `STUCK` → break.
  2. `FINISHED` → run stop hooks; if a hook denies stopping, inject feedback and continue,
     else break (`:1653-1681`).
  3. **Stuck check**: `if self._stuck_detector and self._stuck_detector.is_stuck(): status=STUCK;
     continue` (which breaks next cycle) (`:1683-1692`).
  4. `agent.step()` runs one LLM turn; `iteration += 1`.
  5. `WAITING_FOR_CONFIRMATION` → break.
  6. **Budget check**: `_budget_exceeded_detail()` → emit `ConversationErrorEvent`
     code `"MaxBudgetReached"`, status `ERROR`, break (`:1729-1735`).
  7. **Iteration cap**: `if iteration >= self.max_iteration_per_run:` → error event code
     `"MaxIterationsReached"`, status `ERROR`, break (unless already `FINISHED`) (`:1737-1759`).
  8. Any exception → status `ERROR` + `ConversationErrorEvent` (`:1760-1776`).
- `max_iterations` default = **500** (`state.py:108-113`, `gt=0`).

### Stuck detection (their headline anti-loop mechanism)
- `openhands-sdk/openhands/sdk/conversation/stuck_detector.py`. Runs on the event log **every step**.
- Scans only the **last `MAX_EVENTS_TO_SCAN_FOR_STUCK_DETECTION = 20` events** of the active
  branch, and only events **after the last user message** (`stuck_detector.py:21`, `:71-83`) — so a
  human turn resets the window.
- Five patterns (`is_stuck()`):
  1. repeating identical action→observation
  2. repeating identical action→error
  3. agent monologue (consecutive agent messages, no user)
  4. alternating A/B/A/B action-observation loop
  5. context-window-error loop (currently stubbed, returns False — `:265-274`)
- **Thresholds** (`openhands-sdk/openhands/sdk/conversation/types.py:150-161`,
  `StuckDetectionThresholds`, all `ge=1`):
  - `action_observation = 4`
  - `action_error = 3`
  - `monologue = 3`
  - `alternating_pattern = 6`
- Equality is **semantic, not object identity**: `_event_eq()` (`:276-321`) compares source +
  thought + action + tool_name and **ignores ids, metrics, timestamps, tool_call_id**. This is the
  key trick — a naive `==` would never catch loops because each event has fresh ids/timestamps.
- `stuck_detection` defaults to **True** (`state.py:114-117`).

### Budget / spend governance (directly answers F5)
- `max_budget_per_run: float | None` — "Hard cost ceiling (USD) for a run; None disables the budget
  check" (`local_conversation.py:391-392`).
- `_budget_exceeded_detail()` (`:530-544`): compares
  `conversation_stats.get_combined_metrics().accumulated_cost` (summed across **all** LLMs — agent,
  condenser, critic) to the ceiling **every step**. On breach it produces
  `"Agent reached maximum budget limit ($X); accumulated cost $Y."` → `ConversationErrorEvent`
  code `MaxBudgetReached`, status `ERROR`. So budget is a per-run USD hard stop, independent of the
  iteration cap ("complementing the iteration cap which only bounds step count").

### Supervision, liveness & crash recovery (directly answers F1/F2/F3/F4)
- **`ConversationLease`** — `openhands-agent-server/openhands/agent_server/conversation_lease.py`.
  This is the single most directly-applicable file for Tusker. It coordinates ownership of a
  conversation directory across multiple server instances via a lease file.
  - Lease file `owner_lease.json` (`LEASE_FILE_NAME`) guarded by a `FileLock` on
    `.owner_lease.lock`. Payload: `{owner_instance_id, generation, expires_at, owner_host, owner_pid}`.
  - **`DEFAULT_LEASE_TTL_SECONDS = 45.0`** (`:20`).
  - **`generation` is a monotonic fencing token.** `claim()` (`:122-164`): if another owner holds
    the lease and it is not expired **and** the owner is not dead → raise
    `ConversationLeaseHeldError`. Otherwise take over with `generation + 1`.
  - **Liveness = TTL + PID check.** `_owner_is_dead()` (`:166-185`) does `os.kill(pid, 0)`
    (`_is_pid_alive`, `:47-68`) — but **only trusts the pid check when `owner_host` matches this
    host**; cross-host or legacy leases fall back to pure TTL expiry. It conservatively treats
    "can't determine" as alive so it never steals a live lease.
  - **`renew(generation)`** (`:187-194`) extends `expires_at` keeping the same generation — this is
    the heartbeat. A live owner must renew within 45s or be fenced.
  - **`guarded_write(generation)`** (`:196-201`) holds the lock and calls `_assert_owner_locked()`
    which raises `ConversationOwnershipLostError` if the generation no longer matches — so a
    **superseded owner physically cannot write state** even if its process is still alive. This is
    exactly the fence that stops a zombie owner from corrupting state (F1/F4).
  - `release(generation)` only unlinks the lease if this instance still owns that generation.
- Note: the lease is per-conversation ownership fencing; there is no separate "reaper" that scans
  for dead pids — takeover is lazy (the next instance that wants the conversation does the pid/TTL
  check on `claim()`).

### Retry / poison-pill
- The SDK itself does not "re-dispatch" a finished task; termination is a terminal `execution_status`
  (`ERROR`/`FINISHED`/`STUCK`) with a typed `ConversationErrorEvent`. There is no unbounded
  continuation loop — every exit path in `run()` sets a terminal status and breaks. (Contrast with
  Tusker F2, where the dispatcher kept re-entering.)

---

## 2. opencode (sst/opencode)

TypeScript monorepo (`packages/*`) on Effect + Bun. Cleanly split into a **detached HTTP server**
(`packages/server`) and thin clients (`packages/cli`, `packages/tui`). The most relevant files are
`packages/cli/src/services/daemon.ts` (process supervision), `packages/core/src/session/*`
(state + loop).

### State model
- **Event-sourced → projected to SQLite.** An append-only event log (`EventV2`) is projected into
  SQLite tables via `SessionProjector` (`packages/core/src/session/projector.ts`) and
  `packages/core/src/session/sql.ts` (drizzle-orm: `SessionTable`, `MessageTable`, `PartTable`,
  `SessionInputTable`, …). The runner **recomputes** history each turn from the projection
  (`SessionHistory.entriesForRunner`, used in `runner/llm.ts:200`).
- Control is recompute-from-state: each turn re-reads the session, model, and history.

### Dispatch / loop eligibility (concurrency guard against double-dispatch — relevant to F2)
- **`SessionRunCoordinator`** — `packages/core/src/session/run-coordinator.ts`. A generic
  "serialize per key, run different keys concurrently" primitive over Effect fibers. Per session
  key it keeps at most one owner fiber:
  - `run(key)` — start if idle, **else join the active execution** (returns the same Deferred).
    So two dispatch requests for the same session collapse into one run. This is the structural
    answer to "daemon re-dispatched forever": you literally cannot have two concurrent runs of one
    session key.
  - `wake(key)` — registers **exactly one** coalesced follow-up after the current run
    (`pendingWake` boolean, not a queue) — bounded, not unbounded.
  - `interrupt(key)` — sets `stopping=true` and `Fiber.interrupt`s the owner, then waits for cleanup.
- Step loop: `packages/core/src/session/runner/llm.ts`. Per-turn `runTurnAttempt` (`:173`). The loop
  continues only while the model keeps calling tools (`needsContinuation = true` on each tool-call,
  `:248`); if the model returns text with no tool call, the turn ends. **This is the natural
  completion signal** — no explicit "is it done" check needed.
- Max steps = **soft landing, not a hard kill**:
  - `isLastStep = agent.info?.steps !== undefined && currentStep >= agent.info.steps` (`:202`).
    `steps` is a per-agent optional positive int (`packages/core/src/config/agent.ts:22`).
  - On the last step they **remove all tools**, set `toolChoice: "none"`, and inject
    `MAX_STEPS_PROMPT` as an assistant message (`runner/max-steps.ts`; used at `llm.ts:211-213`).
    The prompt tells the model "MAXIMUM STEPS REACHED … tools are disabled … respond with text
    only." If the model still emits a tool call, `failUnsettledTools("Tools are disabled after the
    maximum agent steps")` (`llm.ts:245`).
- Context overflow → **bounded controlled retry**, not infinite: a `TurnTransitionError`
  (`ContinueAfterCompaction` / `ContinueAfterOverflowCompaction`, `llm.ts:152-166`) triggers a
  compaction pass and rebuilds the request once.

### Supervision, liveness & crash recovery (the strongest F3/F4 material anywhere in this survey)
`packages/cli/src/services/daemon.ts`:
- **Registration file** `<state>/server.json` = `{id, version, url, pid}`, written atomically
  (temp + `rename`, `:166-173`). This is the discovery record clients use to find the server.
- **Liveness = HTTP health probe, not pid.** `healthy()` (`:66-72`) reads the registration, then
  calls `client.v2.health.get({ signal: AbortSignal.timeout(2_000) })` — **2-second** timeout — and
  requires `healthy === true`. A registered-but-hung server fails this.
- **PID-reuse guard** (this is subtle and important for F1): `stop()`/`stopProcess()` only send a
  signal to `pid` **after** re-authenticating via `healthy()` + `sameRegistration()` that the process
  at that pid is *actually the registered server*. Comment at `:156-158`: "A stale registration may
  point at a PID that has since been reused by another process. Only signal the PID after
  authenticating the server." Tusker's F1 (dead-but-marked-running) has the dual risk of *killing a
  reused pid* — this guard is the fix.
- **Graceful termination escalation** `stopProcess()` (`:91-108`): SIGTERM → poll
  `process.kill(pid,0)` every 50ms up to **100 times (~5s)** → if still alive and still the same
  server, SIGKILL → poll again.
- **Server survives client death (direct fix for F4).** `start()` spawns the server **detached with
  stdio ignored and unref'd**: `spawn(process.execPath, ["serve", "--register"], { detached: true,
  stdio: "ignore" }).unref()` (`:120-128`). The server is not pipe-coupled to the launching client,
  so killing the client (or the client dying) does not take the server down. Tusker's F4 is exactly
  the opposite arrangement.
- **Self-fencing watchdog (direct fix for F3 — two servers / ghost).** `register()` (`:164-186`):
  after writing its registration with a unique `id`, the server forks a loop that **re-reads
  server.json every 10 seconds** and **SIGTERMs *itself*** if the file's `id` is no longer its own
  (`:174-179`) — i.e. a newer server registered and superseded it, so the stale one self-terminates.
  A finalizer removes the registration on clean shutdown only if it still owns it (`:180-185`).
- **Password file** (`:44-56`) persists one credential across server restarts so a client can
  reconnect to a restarted server without re-auth ceremony.
- **Client restarts a dead server.** `start()` (`:110-135`): if no healthy/compatible server is
  found, the client spawns one and retries `compatible()` every 50ms up to 100×.
- **TUI degradation** `packages/cli/src/tui.ts`: `gracefulFetch` intercepts 404s and returns
  hardcoded legacy default payloads for a few endpoints so a version-skewed TUI doesn't hard-crash
  against an older/newer server. (This is version-skew tolerance, not full server-death survival —
  if the server is truly gone the client goes back through `daemon.start()` which relaunches it.)

### Budget
- No hard USD budget ceiling in the run loop that I found; usage/cost is *tracked* per session
  (`Usage = {cost, tokens:{input, output}}` in the projector) but the stopping control is the
  per-agent `steps` cap plus the "model stopped calling tools" completion signal. State this
  honestly: **opencode is thin on spend governance** compared to OpenHands.

---

## 3. goose (block/goose, Rust)

Cargo workspace. Runtime lives in `crates/goose/src/agents/`; there is a resident HTTP server
`crates/goose-server` (`goosed`, Axum) that the CLI and desktop talk to.

### State model
- Session messages persisted via `SessionManager` (`session_manager.add_message(...)` throughout
  `agents/agent.rs`); config via a global `Config`. The agent loop itself holds transient state
  (turn counter, compaction attempts) in the streaming closure; durable state is the message log.
- Control is recompute-ish inside one long `async_stream` loop rather than a re-entrant dispatcher.

### Dispatch / loop eligibility
- Main loop: `agents/agent.rs:~1896-2010`, an `async_stream::try_stream!` `loop { … }`.
- **Turn cap**: `DEFAULT_MAX_TURNS = 1000` (`agent.rs:69`). Resolved as
  `session_config.max_turns` → env `GOOSE_MAX_TURNS` → `DEFAULT_MAX_TURNS` (`:1898-1902`).
  On `turns_taken > max_turns` it yields `MAX_TURNS_MESSAGE` ("I've reached the maximum number of
  actions I can do without user input. Would you like me to continue?", `:72`) and breaks — a **soft
  landing** like opencode, not an error (`:1983-1987`).
- **Cancellation** checked at the top of every loop iteration: `if is_token_cancelled(&cancel_token)
  { break; }` (`:1914-1916`).
- **Stop-hook denial poison-pill cap** (relevant to F2-style infinite continuation): a stop hook can
  deny the agent stopping; `consecutive_stop_hook_blocks` is counted and if it exceeds
  `stop_hook_block_cap` the agent emits a warning and **breaks anyway** (`:1959-1967`). So even a
  misbehaving "keep going" hook cannot loop forever.
- Completion: a `FinalOutputTool` produces the terminal answer; stop hooks gate it, then `break`.

### Stuck detection
- `crates/goose/src/tool_monitor.rs` — `RepetitionInspector` (`:34`). `max_repetitions: Option<u32>`;
  it fingerprints tool requests and if the same tool request repeats **more than `max_repetitions`**
  it returns a "Tool '{}' has exceeded maximum repetitions" inspector result that halts execution
  (`:59-126`). Coarser than OpenHands' 5-pattern detector (single-tool identical repetition only),
  and the cap is configurable/optional rather than a fixed default.

### Retry / poison-pill (this is recipe/task-level, and is a good dead-letter model)
- `crates/goose/src/agents/retry.rs` — `RetryManager` + `RetryResult` enum
  `{ Skipped, MaxAttemptsReached, SuccessChecksPassed, Retried }` (`:22-31`).
- `handle_retry_logic()` (`:114-158`):
  1. Run **success checks** = shell commands (`execute_success_checks(&retry_config.checks…)`,
     `:125`). If they pass → `SuccessChecksPassed` (done).
  2. **Hard cap**: `if current_attempts >= retry_config.max_retries` → push an assistant message
     "Maximum retry attempts (N) exceeded. Unable to complete the task successfully.", emit telemetry
     `retry_max_exceeded`, return **`MaxAttemptsReached`** (terminal) (`:132-149`).
  3. Otherwise optionally run an `on_failure` command, reset state, `increment_attempts()`, retry.
- Defaults/constants (`crates/goose/src/agents/types.rs`): `max_retries` default **3**
  (`:104`), validated `> 0` (`:42-43`); `DEFAULT_RETRY_TIMEOUT_SECONDS = 300` (`:16`);
  `DEFAULT_ON_FAILURE_TIMEOUT_SECONDS = 600` (`:19`). The attempt counter lives in `RetryManager`
  (`Arc<Mutex<u32>>`, `:42`) with explicit `reset_attempts()`/`increment_attempts()`.
- This is the pattern Tusker's F2 needed: retries are **capped, and the cap has a terminal state**
  (`MaxAttemptsReached`) with an explicit dead-letter message, gated on an **objective success check**
  (did the tests pass?) rather than blind re-dispatch.

### Supervision (extensions)
- `crates/goose/src/agents/extension_manager.rs` spawns MCP extensions as subprocesses. It
  **restarts an extension when its config changes** (`:941-950`, "extension config changed,
  restarting with updated config"). I did **not** find a strong dead-process auto-restart supervisor
  (heartbeat/liveness reaper) for extensions beyond MCP transport errors surfacing to the caller —
  state this honestly: goose's extension supervision is config-change driven, not liveness-driven.

### Budget
- Turn cap only; no USD ceiling found in the agent loop. Thin on spend governance, like opencode.

---

## 4. Aider (Aider-AI/aider)

Single Python process, no daemon/server — so the "supervision/crash-recovery/lease" rubric rows are
**N/A** (there is nothing to supervise; if the process dies the work dies). Its relevant contribution
is the **reflection limit** that stops infinite self-fix loops.

### Loop termination / reflection limit
- `aider/coders/base_coder.py`:
  - Class attrs: `num_reflections = 0`, `max_reflections = 3` (`:100-101`).
  - The run loop is driven by `self.reflected_message`: after a turn, if the coder set a
    `reflected_message` (from lint errors `:1606`, test errors `:1622`, malformed edit responses
    `:2315/:2327`, or "add these files" prompts `:1563-1566`), it re-runs with that message as input.
  - **The cap**: `if self.num_reflections >= self.max_reflections: io.tool_warning(f"Only
    {self.max_reflections} reflections allowed, stopping."); break` (`:939-944`). Otherwise
    `num_reflections += 1` and loop. So the "fix the lint/test errors you just introduced" loop is
    bounded to 3 rounds.
  - Related counters (diagnostics / poison-pill signals): `num_exhausted_context_windows`
    (`:97`, incremented at `:1546`) and `num_malformed_responses` (`:98`, incremented at `:2306`).
- State model: in-memory conversation + files on disk; no event log, no persistence layer relevant
  to Tusker.

---

## Steal this — concrete mechanisms mapped to Tusker's failure classes

Ordered by impact for Tusker.

1. **Lease with TTL + monotonic generation fence + guarded writes** — fixes **F1, F2, F3, F4**.
   Source: OpenHands `openhands-agent-server/openhands/agent_server/conversation_lease.py`
   (`DEFAULT_LEASE_TTL_SECONDS=45`, `generation` fencing token, `renew()` heartbeat,
   `guarded_write()`/`_assert_owner_locked()` that makes a superseded owner unable to write).
   Map to Tusker: a run lease keyed by task-id that (a) has a TTL the runner must **renew** or be
   declared dead (kills F1's "dead but marked running"); (b) carries a **generation** so the daemon
   fences a stale runner's writes/continuations (kills F2's "lease stayed active, re-dispatched
   forever" — a superseded generation simply can't re-enter); (c) survives daemon restart because
   ownership is a *file* the next daemon reconciles via TTL+pid, not in-memory state (F3).

2. **Detached, stdio-decoupled runner processes** — direct fix for **F4**.
   Source: opencode `packages/cli/src/services/daemon.ts:120-128`
   (`spawn(..., { detached: true, stdio: "ignore" }).unref()`). Tusker's runners are pipe-coupled to
   daemon stdio and die on daemon SIGTERM; opencode deliberately severs that coupling so the child
   outlives the parent. Runners should write to their own log files (Tusker already has
   `.tusker/scratch/<TASK-ID>/`) and be `setsid`/detached, never inherit the daemon's stdio.

3. **PID-reuse-safe liveness = authenticated health probe, not bare pid check** — hardens **F1/F3**.
   Source: opencode `daemon.ts` `healthy()` (2s HTTP probe) + the `sameRegistration()` guard before
   ever signaling a pid (`:156-158`). Tusker must not treat "pid exists" as "runner alive" (pid may
   be reused) nor kill a reused pid; require the runner to answer a liveness challenge (or match a
   recorded `{pid, start-time, instance-id}` tuple) before declaring it alive **or** killing it.

4. **A single recompute-from-state loop with all termination checks inline, gated by one lock** —
   structural fix for **F2**. Source: OpenHands `local_conversation.py:1638-1759` — one `while True`
   under `with self._state:` that re-derives stuck/budget/iteration/finished each cycle and always
   exits to a **terminal `execution_status`**. Tusker's F2 came from the dispatch decision and the
   lease being separate/inconsistent; collapse "should this task run another step?" into one shared
   eligibility function that reads current state (status + lease + attempt count + spend) and can
   only return run-or-terminal. opencode's `SessionRunCoordinator` (`run-coordinator.ts`) is the
   complementary guarantee: **at most one active run per key, follow-ups coalesced to one** — makes
   double-dispatch structurally impossible.

5. **Budget as a per-run hard ceiling checked every step, across all model calls** — fixes **F5**.
   Source: OpenHands `local_conversation.py:530-544, 1729-1735` (`max_budget_per_run` USD;
   `accumulated_cost` summed across agent+condenser+critic; breach → terminal `MaxBudgetReached`
   error event). Tusker should carry a per-task (and ideally global) USD/token ceiling in task state,
   check it after each attempt/step against summed spend, and transition to a terminal
   `budget-exceeded` state — not just an iteration count. Pair with a coarse **iteration/attempt
   cap** (OpenHands `max_iterations=500`; goose `max_turns=1000`; goose recipe `max_retries=3`;
   aider `max_reflections=3`) as a cheap backstop.

6. **Semantic stuck detection over the recent event log** — additional defense for **F2**.
   Source: OpenHands `stuck_detector.py` (scan last 20 events after the last user msg; 5 patterns;
   `_event_eq` ignores ids/metrics/timestamps so real repetition is detected) with thresholds
   action_observation=4 / action_error=3 / monologue=3 / alternating=6. goose's `RepetitionInspector`
   is a lighter single-tool-repetition variant. Tusker already has an event log per task; a
   stuck-detector that fires when the last N attempts produce semantically identical
   action→error/observation cycles would have caught the 88-attempt runaway long before 88.

7. **Capped retry that terminates in an explicit dead-letter state, gated on an objective success
   check** — reinforces **F2**. Source: goose `agents/retry.rs:114-158` (`handle_retry_logic`:
   run shell success checks; `attempts >= max_retries` → `MaxAttemptsReached` terminal +
   user-visible "Maximum retry attempts (N) exceeded" message + telemetry). Tusker's continuation
   retries should (a) only re-dispatch if an **objective gate** (build/test) still fails, and
   (b) hit a hard cap that moves the task to a **terminal dead-letter/needs-human** state rather than
   re-queuing.

8. **Self-fencing on supersession** — extra insurance for **F3**. Source: opencode `daemon.ts`
   `register()` self-watchdog (re-read registration every 10s; SIGTERM self if `id` changed). A
   Tusker daemon that discovers a newer daemon owns the repo lock should stand down rather than run
   ghost dispatches.

### Honest gaps
- **opencode and goose have no USD budget governance** in the loop — only step/turn caps. OpenHands
  is the only one of the four with a real spend ceiling (F5).
- **goose extension supervision is config-change-driven, not liveness-driven** — no heartbeat reaper
  that auto-restarts a silently-dead MCP subprocess.
- **Aider has no supervision/crash-recovery story at all** (single process); its only relevant
  contribution is the `max_reflections=3` loop cap.
- OpenHands' lease has **no active reaper**; takeover is lazy (checked on the next `claim()`). If no
  one tries to claim, a dead owner's lease just sits until TTL — fine for their model, but Tusker's
  daemon should actively reconcile on a timer.
