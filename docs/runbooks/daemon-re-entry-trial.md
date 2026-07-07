# Daemon re-entry trial runbook

Re-certification procedure for the resident daemon after any change to the dispatch core, lease semantics, or runner process model. Run it on the real machine with real runners and a bounded low-risk cohort. The daemon is authorized as executor only after a clean end-to-end pass. First executed 2026-07-07 (RUN-T-0017); observed results from that run are noted per step.

## Setup

1. Cohort: 2–3 real tasks, p2/low risk, small — plus two disposable probes:
   - **Budget probe**: task with frontmatter `budget: {per_attempt_input_tokens: 5000, per_attempt_output_tokens: 2000}`; any real attempt trips it in the first turn.
   - **Poison probe**: task whose contract instructs the runner to end its turn without any action ("no-progress" by construction).
   Probe verification tables need exact commands or the plan gate refuses dispatch (`verification missing exact command or manual proof` — observed).
2. Fence the cohort: `tusker status <id> backlog` every other dispatchable task (record the list for restoration).
3. Caps: `max_active_runs_per_project: 2` in `.tusker/WORKFLOW.md`, **and** `tusker daemon limits --max-active-runs 2` — the global limit is separate and wins silently (observed: a leftover global cap of 1 serialized all dispatch; only the plan-gate blocker text revealed it).
4. Config sanity: `codex.max_turns` must allow real work (observed: `max_turns: 1` manufactures continuation loops by construction).
5. Check `tusker projects list --json` for stale registrations first. Broken registrations are quarantined instead of fatal (RUN-T-0021), but a clean trial should still fix or disable them before starting.

## Steps

Each step records an evidence card: command, expected, observed.

### 1. Cold start
`tusker daemon run` (background). Expect: boots serving the healthy project; legacy ghost leases released within one tick; sentinel green or honestly open on real dirt; serve up at `127.0.0.1:7420` (hosted inside the daemon since SRV-T-0007).
2026-07-07 observed: fatal exit on a stale project registration (→ RUN-T-0021); after disabling it, boot clean; the sentinel opened the circuit on seven legacy over-cap records — correct honesty; paved repair is now `tusker runs retire <id> --reason <why>` for each stale row, then `tusker daemon resume` (RUN-T-0022); `/api/runs` displayed 15 unclaimed placeholder rows as "running" (→ SRV-T-0013).

### 2. Kill-9 mid-run
Wait until two runs are mid-attempt (fresh heartbeats). `kill -9 <daemon pid>`. Expect: wrappers survive (reparented to pid 1), heartbeats keep advancing daemon-less; on `tusker daemon run` the daemon adopts both without interrupting them (same lease generation, same attempt count).
2026-07-07 observed: survival PASS — both wrappers heartbeated the store directly through the outage (RUN-T-0013/0018 proven live). Adoption FAIL — the daemon bumped lease generations (fence-advance), the wrappers correctly stopped, fresh attempts dispatched (attempt_count 1→2): fence-and-replace, safe but wasteful (→ RUN-T-0025). Note: serve goes dark with the daemon; that is expected during this step.
RUN-T-0025 repair: restart adoption now restores the verified wrapper identity from the wrapper-spawn event before liveness checks, so a child pid in the run row no longer causes fence-and-replace. A dead wrapper still fences and redispatches when retry budget remains.

### 3. Review-flip under a live run
`tusker status <live-task> review` while its run is mid-attempt. Expect: run halts within one heartbeat via the stop-signal, lease released by the reconciler, no re-dispatch, no continuation retry.
2026-07-07 observed: halt in ~21s and no loop (the July-6 incident scenario is dead), but the halt came from the sentinel (`held_lease_dispatch_eligible` → circuit) — the last line of defense, not the first-line reconciler release (evidence attached to RUN-T-0023). The sibling live run continued untouched under the open circuit — correct containment.

### 4. Budget trip
Let the budget probe dispatch. Expect: run killed at the ceiling, attempt records `budget_exceeded`, caps count it.
2026-07-07 observed: PASS clean — `attempt input token budget exceeded: 14361 > 5000`, terminal on attempt 1.

### 5. Poison park
Let the poison probe dispatch. Expect: continuations consume the cap (3) then park as `parked_no_progress`; one `tusker redrive` restores dispatchability exactly once.
2026-07-07 observed: FAIL — the cap queued a 4th continuation instead of parking; the sentinel circuit stopped the loop at 4 attempts (→ RUN-T-0023, both runner lanes). Separately, `redrive` re-queued without resetting the attempt window, instantly re-tripping the sentinel (→ RUN-T-0024).

### 6. Circuit and gated resume
With a violation live and unrepaired: `tusker daemon resume` must refuse, naming the violation. After repair, resume must verify and close.
2026-07-07 observed: PASS both directions — refuse-while-dirty with exact record and counts (`attempt_count: 4, cap: 3`), close-after-repair verified. Exercised five times on real dirt during this trial; the sentinel is the component that repeatedly turned silent failure modes into loud stops.

### 7. Drain stop
`tusker daemon stop --drain` with a run live. Expect: bounded wait; honest `drained` flag; plain `stop` leaves wrappers alive for adoption.
2026-07-07 observed: PASS — 30s bounded wait, `{"drained": false, "stopped": true}`, wrapper alive and heartbeating after daemon exit.

## Teardown

1. Cancel the probes (`tusker status <probe> cancelled`) and retire their run rows with `tusker runs retire <probe> --reason <why>`.
2. Restore the fenced tasks to `ready` from the recorded list; `tusker reconcile`.
3. Kill e2e leftovers: `pkill -f tusker-crash` (the crash-recovery suite leaks `fake-runner --mode hold` processes on red runs).
4. File every deviation as a task with the step's evidence; the trial repeats after fixes until a clean pass. Only then: `tusker daemon limits --max-active-runs 5` and record the authorization in RUN-T-0017's closeout.

## Trial run #1 verdict (2026-07-07)

Not clean — re-trial required after fixes. Deviations: RUN-T-0021 (fatal boot on broken registration), RUN-T-0022 (no `runs retire`), RUN-T-0023 (p0 — clean-finish lease leak + cap queues instead of parking, both lanes), RUN-T-0024 (redrive doesn't reset attempt window), RUN-T-0025 (adoption fence-and-replaces), SRV-T-0013 (phantom "running" rows). Validated live: spend governor, sentinel + gated resume, wrapper survival + direct heartbeats, drain stop, Ralph worker protocol (fresh sessions, flat per-attempt input, PLAN.md continuity), plan-gated dispatch honesty.
