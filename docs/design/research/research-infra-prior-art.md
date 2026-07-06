# Prior Art for Tusker: Proven Semantics from Job-Queue / Workflow / Controller Systems

Research date: 2026-07-06. Goal: import the *already-solved* semantics for the five failure classes Tusker hit, with exact constants, formulas, and source pointers.

**Tusker failure classes (targets):**
- **F1** — worker dead but lease still held (stuck lease / orphaned work).
- **F2** — poison-pill work re-dispatched forever (88 attempts; clean-exit continuation loop, no cap).
- **F3** — supervisor died silently; nobody supervised the supervisor.
- **F4** — no spend/budget bulkheads.
- **F5** — event-reaction logic acting on stale partial state.

---

## 1. Amazon SQS + Dead-Letter Queue

The canonical "lease + poison quarantine" primitive. A message is *leased* (made invisible) on receive; if not deleted before the lease expires it returns to the queue; after N failed receives it is quarantined in a DLQ.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Visibility timeout** (the lease) | On `ReceiveMessage`, the message stays in the queue but becomes invisible to other consumers. If the consumer does not `DeleteMessage` before the timeout expires, the message becomes visible again and is re-delivered. Timer *starts at receive*. | Default **30 s**; min **0 s**; max **12 h (43200 s)**. At-least-once delivery — no guarantee against duplicate delivery within the window. | F1 |
| 2 | **ChangeMessageVisibility** (heartbeat / lease extension) | Consumer of a long task calls `ChangeMessageVisibility` to *extend* the lease while still working ("implement a heartbeat mechanism to periodically extend the visibility timeout"). Setting it to `0` immediately releases the message (voluntary abort). | The 12 h cap is **absolute from first receive** — extending does not reset it. | F1 |
| 3 | **maxReceiveCount → DLQ** (poison quarantine) | "The number of times a consumer can receive a message from a source queue before it is moved to a dead-letter queue." On exceeding it, SQS moves the message to the configured DLQ instead of redelivering forever. | Range **1–1000**. No infinite redelivery. Standard-queue quirk: if `maxReceiveCount > 3` and a msg is received ≥3× without delete, SQS moves it to the *back* of the queue. | **F2** |
| 4 | **RedrivePolicy** (JSON config) | Attached to the *source* queue: `{"deadLetterTargetArn":"arn:aws:sqs:...:dlq","maxReceiveCount":"10"}`. DLQ must be same account+region. | — | F2 |
| 5 | **RedriveAllowPolicy** (which sources may use a DLQ) | `allowAll` (default), `byQueue` (up to **10** source ARNs), or `denyAll`. | Bulkheads which queues can dump into a shared DLQ. | F4 (isolation) |
| 6 | **Message retention** | DLQ retention should exceed source retention; for standard queues the *original* enqueue timestamp is preserved on move (so a msg can expire out of the DLQ based on original age). | Default **4 days (345600 s)**; range **60 s – 14 days**. | F2 (bounded quarantine) |

Sources: [Using DLQs in SQS](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-dead-letter-queues.html) · [Visibility timeout](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-visibility-timeout.html)

**Tusker takeaway:** lease = visibility timeout; heartbeat = `ChangeMessageVisibility`; the 12 h absolute cap = a per-attempt wall clock; `maxReceiveCount → DLQ` = the exact fix for F2 (a hard receive/dispatch counter that ends in a terminal parked queue, not infinite redelivery).

---

## 2. Sidekiq (`github.com/sidekiq/sidekiq`)

Ruby/Redis job queue. Gives the retry-backoff formula, the "Dead set" terminal state, process heartbeats, and (Pro) `super_fetch` orphan recovery + a built-in poison-pill circuit breaker.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Retry with exponential backoff + jitter** | On job raise, re-enqueued to the `retry` sorted set with a computed delay. Formula (seconds):<br>`delay = (count**4) + 15 + (rand(10) * (count + 1))` | `DEFAULT_MAX_RETRY_ATTEMPTS = 25` (~20 days / "3 weeks"). Produces ~15, 16, 31, 96, 271 s … The `rand` term is the anti-thundering-herd jitter. | F2 |
| 2 | **Dead set** ("Dead Job Queue" — terminal state) | After the 25th retry, `send_to_morgue` does `conn.zadd("dead", now, payload)`. "A holding pen for jobs which have failed all their retries" — needs manual intervention; retriable from Web UI. | Bounded: `dead_max_jobs = 10_000`, `dead_timeout_in_seconds = 180 days (6 months)`; trimmed via `zremrangebyrank`. | **F2** |
| 3 | **Process heartbeat** | Each worker process writes a heartbeat to Redis on an interval; the process key has a TTL. Missing heartbeat past TTL ⇒ process presumed dead. | Heartbeat every **5 s**; process key **TTL 60 s** (ratio **1:12**). | F1 |
| 4 | **Basic fetch vs super_fetch** | Basic fetch = `BRPOP` — job removed from Redis the instant it's fetched; "if Sidekiq crashes while processing that job, it is lost forever." `super_fetch` (Pro) uses `LMOVE`/`RPOPLPUSH` into a *private working queue* per process and does **not remove the job until it completes**. | — | F1 |
| 5 | **Orphan checker** (super_fetch recovery) | When a process's heartbeat has expired, its private working queue is orphaned; the orphan checker re-enqueues those in-flight jobs. Runs when (a) a heartbeat has expired **and** (b) ≥1 min since the last check; plus a full Redis `SCAN` once **hourly** to catch stragglers. | dead-after ≈ **60 s** (heartbeat TTL). | **F1** |
| 6 | **super_fetch poison-pill breaker** | "If a recovered job crashes the process **3 times within 72 h**, super_fetch automatically moves it to the **Dead set**, preventing infinite crash loops." | 3 crashes / 72 h ⇒ Dead. | **F2** |

Sources: [job_retry.rb](https://raw.githubusercontent.com/sidekiq/sidekiq/main/lib/sidekiq/job_retry.rb) · [Error Handling wiki](https://github.com/sidekiq/sidekiq/wiki/Error-Handling) · [Reliability wiki](https://github.com/sidekiq/sidekiq/wiki/Reliability) · [How Sidekiq works](https://www.mikeperham.com/how-sidekiq-works/)

**Tusker takeaway:** Sidekiq is the closest analog to Tusker's exact bugs. `super_fetch`'s "keep the work claimed until complete, and re-enqueue only after the owner's heartbeat expires" is the orphan-lease design (F1). The "3 crashes in 72 h → Dead" breaker is a direct template for capping the *reprocess* count independent of the normal retry count — precisely the missing guard behind the 88-attempt continuation loop (F2).

---

## 3. Temporal (`temporal.io`, `github.com/temporalio`)

Durable workflow engine. Two ideas Tusker should steal wholesale: (a) the **workflow/activity split** with **event-sourced replay** (crash recovery is free), and (b) **activity heartbeats + a heartbeat timeout** as the liveness contract for long side-effecting work.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Workflow vs Activity split** | *Workflow* = deterministic orchestration; its history is persisted as events and can be **replayed** to reconstruct state exactly. *Activity* = the non-deterministic side effect (I/O, subprocess); at-least-once, retried independently, must be idempotent. | — | F5 (deterministic state), F1/F2 (activities carry the retry/timeout) |
| 2 | **Event-sourced replay** (crash recovery) | Full event history is durable; on worker crash the workflow is resumed on another worker by replaying events → deterministic state reconstruction. No "stale partial state" — the log *is* the state. | — | **F5** |
| 3 | **Activity Heartbeat + Heartbeat Timeout** | Heartbeat = "a ping from the Worker executing the Activity to the Temporal Service." If no heartbeat arrives within **Heartbeat Timeout**, the activity task fails and retries per policy. This is how a *stuck/crashed* worker is detected (server can't otherwise tell). | No default heartbeat timeout — must be set for long activities; recommended for anything long-running. | **F1** |
| 4 | **Four activity timeouts** | **Schedule-To-Start** (queue→worker pickup; detects "no worker/capacity", default ∞), **Start-To-Close** (per-attempt execution cap; *strongly recommended to set*; default = Schedule-To-Close), **Schedule-To-Close** (overall across all retries; default ∞), **Heartbeat** (max gap between heartbeats). | Start-To-Close = per-attempt wall clock; Schedule-To-Close = total-across-retries wall clock. | F1, F4 |
| 5 | **Default Retry Policy** (activities) | `delay = min(initialInterval × backoffCoefficient^(attempt-1), maximumInterval)`, retry until `maximumAttempts`. | initialInterval **1 s**, backoffCoefficient **2.0**, maximumInterval **100 × initial = 100 s**, maximumAttempts **0 = unlimited**, nonRetryableErrors **[]**. Workflows have **no** default retry (don't retry by default). | F2 |
| 6 | **Task-queue lease + Sticky Execution** | Worker long-polls a task queue for a task (the lease). *Sticky execution*: after first task, the workflow is cached in that worker's memory so subsequent tasks skip full replay; the sticky task has a short schedule-to-start. If the worker doesn't pick up the sticky task in time, stickiness is **disabled** and the task is rescheduled on the general queue for *any* worker (which replays from history). **Workflow Task Timeout** detects a worker that died mid-task. | Sticky schedule-to-start **5 s** default; Workflow Task Timeout default **10 s** (max **2 min**). | F1, F5 |

Sources: [Retry policies](https://docs.temporal.io/encyclopedia/retry-policies) · [Detecting activity failures](https://docs.temporal.io/encyclopedia/detecting-activity-failures) · [The four types of activity timeouts](https://temporal.io/blog/activity-timeouts) · [Sticky execution](https://docs.temporal.io/sticky-execution)

**Tusker takeaway:** map an agent run to an **activity**: it's a non-deterministic subprocess that must **heartbeat** (progress/liveness) and is governed by a **Start-To-Close** (per-attempt wall clock), **Schedule-To-Close** (total budget across retries), and **Heartbeat Timeout** (stuck detection = F1). Tusker's ledger of attempts/evidence should behave like Temporal's event history so recovery reads truth, not partial in-memory state (F5). Note: unlike Temporal's default `maximumAttempts = 0` (unlimited), Tusker *must* cap attempts because each attempt costs real money.

---

## 4. Kubernetes controller pattern + client-go workqueue

The definitive answer to F5. Controllers are **level-triggered**: each reconcile reads *desired* and *actual* state fresh and converges; events are only hints that wake the loop, never the source of truth.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Level- not edge-triggered** | "If an API object appears with a marker value of `true`, you can't count on having seen it turn from `false` to `true`, only that you now observe it being `true`." A controller offline for a while must still converge from *current* state. Never act on the *transition*; act on the *observed level*. | — | **F5** |
| 2 | **Reconcile contract** | "It watches some object for the world's desired state, and watches the world's actual state too. Then it sends instructions to try and make the current state be more like the desired state." Loop: read desired → read actual → compute diff → apply. | — | **F5** |
| 3 | **Idempotency + multiple actors** | "There are other actors in the system. Just because you haven't changed an object doesn't mean somebody else hasn't." Reconcile must be safe to run repeatedly with the same desired state. | — | F5 |
| 4 | **Periodic resync** | Informers periodically resync — re-fire Update for every cached object — giving level-driven reconciliation that compensates for any missed event. | Resync period is configurable (commonly 30 s – 10 min). | F5 (missed-event safety net) |
| 5 | **Requeue with backoff on error** | "Percolate errors to the top level for consistent re-queuing. We have a `workqueue.RateLimitingInterface` to allow simple requeuing with reasonable backoffs." A failed reconcile returns an error → item is requeued with rate-limited backoff. | — | F2 (backoff), F5 |
| 6 | **DefaultControllerRateLimiter** (client-go) | `MaxOf(` per-item exponential **`NewItemExponentialFailureRateLimiter(5*time.Millisecond, 1000*time.Second)`** , overall token bucket **`BucketRateLimiter{rate.NewLimiter(rate.Limit(10), 100)}`** `)`. The per-item limiter doubles the delay each consecutive failure (5 ms → … → cap 1000 s); the bucket caps *aggregate* requeue throughput. `MaxOf` returns whichever delay is larger. | base **5 ms**, cap **1000 s**, bucket **10 qps / burst 100**. | F2, F4 |

Sources: [Writing controllers (sig-api-machinery)](https://raw.githubusercontent.com/kubernetes/community/master/contributors/devel/sig-api-machinery/controllers.md) · [client-go workqueue default_rate_limiters.go](https://raw.githubusercontent.com/kubernetes/client-go/master/util/workqueue/default_rate_limiters.go)

**Tusker takeaway:** Tusker's dispatcher should be a **level-triggered reconcile loop** over the ledger, not an event-reaction handler. Each tick: read runnable tasks (desired) + live leases/subprocesses (actual), converge (reap expired leases, dispatch up to concurrency, quarantine poison). Events (`tusker next`, task edits, exit signals) only *wake* the loop; the loop re-derives truth. This structurally eliminates F5 and gives F2's backoff for free via a per-task exponential + a global dispatch token bucket.

---

## 5. Netflix Conductor (OSS task orchestrator)

Poll-based worker leasing with a two-clock timeout model that maps cleanly to "worker went silent" vs "task took too long", plus bounded retry → workflow failure.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Poll-based worker leasing** | Workers *poll* the task queue, ACK, then must keep reporting status. The server owns the lease; a silent worker's task is reclaimed. | — | F1 |
| 2 | **responseTimeoutSeconds** (lease-renewal / heartbeat) | "If no status update from the worker within this interval, the task is **rescheduled**." This is the heartbeat/lease-renewal deadline — the worker must report progress or lose the task. | Default **600 s**. | **F1** |
| 3 | **timeoutSeconds** (execution deadline) | Task marked `TIMED_OUT` if it hasn't reached a terminal state after first entering `IN_PROGRESS`. The total-execution wall clock (distinct from #2). | `0` = no timeout. | F1, F4 |
| 4 | **pollTimeoutSeconds** (queue-wait deadline) | Task marked `TIMED_OUT` if **not polled** by any worker in time (nobody picked it up — capacity problem). | — | F1 |
| 5 | **timeoutPolicy** | On `timeoutSeconds` expiry: **RETRY** / **TIME_OUT_WF** (fail+terminate workflow, default) / **ALERT_ONLY** (metric only). | Default **TIME_OUT_WF**. | F2 |
| 6 | **retryLogic + retryCount** (bounded retry → failure) | `retryLogic` ∈ **FIXED** (`retryDelaySeconds`), **EXPONENTIAL_BACKOFF** (`retryDelaySeconds × 2^attempt`), **LINEAR_BACKOFF** (`retryDelaySeconds × backoffScaleFactor × attempt`), plus `maxRetryDelaySeconds` cap and `backoffJitterMs`. At exhaustion the task is **FAILED** → workflow FAILED (unless optional). | `retryCount` default **3**, hard cap **10**. | **F2** |

Sources: [Conductor task definitions (taskdef)](https://conductor-oss.github.io/conductor/documentation/configuration/taskdef.html)

**Tusker takeaway:** Conductor's two clocks are exactly what F1 needs, kept separate: **responseTimeoutSeconds** = "renew your lease or I reclaim you" (heartbeat), **timeoutSeconds** = "this attempt has run too long" (per-attempt wall clock). And note the sane bounded defaults: retry **3**, hard cap **10** — vs Tusker's observed 88.

---

## 6. systemd unit supervision (supervise the supervisor)

The daemon-level circuit breaker for F3.

| # | Mechanism | Exact semantics | Defaults / constants | Kills |
|---|-----------|-----------------|----------------------|-------|
| 1 | **Restart=** | When to auto-restart: `no` (default), `on-success`, `on-failure`, `on-abnormal` (signal/timeout/watchdog), `on-abort`, `on-watchdog`, `always`. `on-failure` covers crash/nonzero-exit/watchdog. | default `no` — must opt in. | **F3** |
| 2 | **RestartSec** | Sleep before restarting (prevents hot-loop). | default **100 ms**. | F3 |
| 3 | **StartLimitIntervalSec / StartLimitBurst** (restart circuit breaker) | If the unit is (re)started more than `StartLimitBurst` times within `StartLimitIntervalSec`, systemd **stops restarting** and puts the unit in **failed** state (requires `systemctl reset-failed`). Prevents an infinite crash-restart loop. | default **10 s** window, burst **5**. (`DefaultStartLimitIntervalSec` in system.conf = 10 s.) | **F3** |
| 4 | **WatchdogSec** | Process must call `sd_notify(WATCHDOG=1)` within this interval or systemd considers it **hung** and kills+restarts it (with `Restart=on-watchdog`/`on-failure`). Detects a *hung* daemon, not just a crashed one. | Recommended notify at `WatchdogSec/2`. | **F3** |

Sources: [systemd.service(5)](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html) · [Self-healing services with systemd (Red Hat)](https://www.redhat.com/en/blog/systemd-automate-recovery)

**Tusker takeaway:** run the Tusker daemon under systemd (Linux) / launchd (macOS `KeepAlive`+`ThrottleInterval`) with `Restart=on-failure`, a `RestartSec` floor, and a `StartLimitBurst`/`Interval` circuit breaker so a crash-looping daemon *stops and alerts* instead of thrashing. Add a `WatchdogSec` heartbeat driven by the reconcile loop so a *hung* (deadlocked, not crashed) daemon is detected and restarted — the "who supervises the supervisor" answer to F3.

---

## Synthesis — recommended semantics table for Tusker

Numbers chosen for **long, expensive, non-deterministic agent runs** (minutes–tens-of-minutes, real $/token cost per attempt). Every row cites the system it's derived from. Treat the numbers as defaults to config, not laws.

### Core constants

| Concern | Recommended value | Formula / rule | Justified by |
|---|---|---|---|
| **Heartbeat interval** (agent → ledger liveness) | **15 s** | Agent writes a cheap progress/liveness beat every 15 s. | Sidekiq 5 s beat; Conductor status updates; Temporal heartbeat. |
| **Lease TTL** (reclaim if no beat) | **60 s** (miss ~4 beats) | `TTL = 4 × heartbeat`. Reaper reclaims leases whose `last_beat > TTL`. | Sidekiq 5 s:60 s = **1:12** (conservative); we use **1:4** for faster reclaim while tolerating transient stalls (long tool calls). SQS visibility-timeout model. |
| **Reaper scan period** | **30 s** (`TTL/2`) | Level-triggered sweep for expired leases + orphaned subprocesses; re-enqueue their tasks. | Sidekiq orphan checker; K8s resync; SQS redelivery. |
| **Per-attempt wall clock** (Start-To-Close) | **20 min** (config) | Kill + fail the attempt if a single run exceeds it, even while heartbeating. | Temporal Start-To-Close; SQS 12 h abs cap; Conductor `timeoutSeconds`. |
| **Total wall clock per task** (Schedule-To-Close) | **~2 h** or `5 × per-attempt` | Task → DEAD when cumulative time across attempts exceeds it. | Temporal Schedule-To-Close; Conductor `totalTimeoutSeconds`. |
| **Retry cap (infra failures)** | **maxAttempts = 5**, hard ceiling **10** | Worker-died / infra-error retries. Terminal state at exhaustion, never infinite. | Conductor default 3 / cap 10; SQS `maxReceiveCount`; **not** Temporal's unlimited (cost!). |
| **Continuation / no-progress cap** (the F2 fix) | **3 consecutive no-progress continuations → DEAD** | Separate counter from infra retries: if N consecutive clean-exit continuations produce **no new diff/evidence**, quarantine. | Sidekiq super_fetch "3 crashes / 72 h → Dead". This is the missing guard behind the 88-attempt loop. |
| **Retry backoff** | `delay = min(30 s × 2^(n-1), 15 min) + rand(jitter)` | Exponential + full jitter. | Temporal (initial 1 s, coeff 2.0, cap 100 s) scaled up for expensive runs; Sidekiq's `+ rand()` jitter; Conductor EXPONENTIAL_BACKOFF. |
| **Dispatch rate limiter** | per-task exponential (base **1 s**, cap **15 min**) **MaxOf** global token bucket (**e.g. 2 qps / burst 8**) | Reconcile requeue backoff; caps aggregate dispatch churn. | client-go `MaxOf(ItemExponential(5ms,1000s), Bucket(10,100))`, scaled down for agent scale. |

### Structural rules

**Poison-pill terminal state (F2).** A real terminal `DEAD`/`PARKED` state, reached by *any* of: infra `maxAttempts` hit, continuation no-progress cap (3) hit, or total wall clock exceeded. DEAD tasks are **not** re-dispatched — they wait for explicit **redrive** (a `tusker redrive <TASK-ID>` command, like SQS DLQ redrive / Sidekiq Web-UI retry). Retain DEAD tasks with their evidence (bounded like Sidekiq's Dead set: cap count or age, e.g. keep last 10 000 / 30 days). Distinguish two counters explicitly on each task: `receive_count` (dispatches) and `no_progress_streak` (consecutive continuations with zero forward progress) — the second is what the 88-attempt bug lacked.

**Reconcile-loop shape (F5).** Replace event-reaction with a **level-triggered reconcile loop**:
1. Read *desired*: runnable tasks from the ledger (source of truth).
2. Read *actual*: live leases + running subprocesses.
3. Converge, idempotently: reap expired leases → re-enqueue; dispatch runnable up to concurrency; quarantine poison; renew healthy leases.
4. Requeue failed reconcile with the per-task exponential backoff above.
5. Run on **every event *and* on a 30 s timer** (resync) so a missed/duplicate/out-of-order event never leaves stale state. Events only *wake* the loop; they are never trusted as state. (K8s controller contract verbatim.)

**Budget bulkheads (F4)** — layered, each an independent circuit:
- **Per-attempt** token/$ cap (spend analog of Start-To-Close): kill the run when exceeded.
- **Per-task cumulative** $ cap = `5 ×` per-attempt (spend analog of Schedule-To-Close / `totalTimeoutSeconds`): → DEAD when exceeded.
- **Per-project concurrency** cap (bulkhead): max N concurrent leases per project (e.g. 4), so one project can't starve others. (SQS in-flight quota; RedriveAllowPolicy isolation.)
- **Global daily $ ceiling** → **circuit-open**: when the day's spend crosses the ceiling, the dispatcher stops dispatching new attempts and alerts (running work drains). Mirror of systemd `StartLimitBurst` — a spend circuit breaker, not a silent overspend.

**Daemon self-supervision (F3).** Run the daemon under an OS supervisor, never bare:
- systemd (Linux): `Restart=on-failure`, `RestartSec=1s` (floor above the 100 ms default to avoid hot-loop), `StartLimitIntervalSec=60`, `StartLimitBurst=5` → 5 crashes/60 s ⇒ unit goes **failed** and alerts (circuit breaker, no thrash). Add `WatchdogSec=30` with the reconcile loop calling `sd_notify(WATCHDOG=1)` each tick, so a **hung** (deadlocked, still-alive) daemon is detected and restarted — not just a crashed one.
- macOS (dev): launchd `KeepAlive=true` + `ThrottleInterval` (min 10 s between restarts) as the equivalent.
- The watchdog heartbeat should be driven by the *reconcile loop*, so "daemon alive" means "loop is actually reconciling", not merely "process exists".

### One-line mapping recap

- **F1** (stuck lease) ← lease TTL 60 s + 15 s heartbeat + 30 s reaper + per-attempt 20 min wall clock. *(SQS visibility timeout, Sidekiq super_fetch orphan checker, Temporal heartbeat timeout, Conductor responseTimeoutSeconds.)*
- **F2** (poison loop) ← `receive_count` cap 5/10 **and** `no_progress_streak` cap 3 → terminal DEAD w/ manual redrive. *(SQS maxReceiveCount→DLQ, Sidekiq 25→Dead + super_fetch 3-crash breaker, Conductor retryCount, Temporal maximumAttempts.)*
- **F3** (dead supervisor) ← systemd `Restart=on-failure` + `StartLimitBurst=5/60s` + `WatchdogSec`. *(systemd.)*
- **F4** (no budget) ← 4-layer bulkhead: per-attempt / per-task / per-project-concurrency / global-daily circuit breaker. *(SQS in-flight quota, systemd StartLimit as breaker template, Temporal Schedule-To-Close.)*
- **F5** (stale state) ← level-triggered reconcile loop, idempotent converge, 30 s resync, requeue-with-backoff; events only wake the loop. *(K8s controller + client-go workqueue; Temporal event-sourced replay for state-as-log.)*
