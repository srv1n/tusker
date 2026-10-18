---
subject: process-containment
title: "Process containment: Tusker knows and controls everything it starts"
keywords: [process, orphan, janitor, inventory, gate, watchdog, containment, stuck, silence]
part_of: overview
describes: [cmd/tusker/runner_wrapper.go, cmd/tusker/v7_land_cmd.go, cmd/tusker/daemon.go, cmd/tusker/test_main_test.go]
status: canonical
created: 2026-10-18
last_verified:
read_when: "Changing how the daemon or CLI starts, tracks, stops or cleans up child processes (runners, landing gates, checks, test suites), or the watchdog and crash-restart behavior."
skip_when: "Judging whether a run's work is good or stuck by reading its messages; that is the run-triage layer, not containment."
sources: [decisions/2026-10-18-process-containment-grill.md]
updates: [run-session-continuity, runner-execution-boundary]
decisions_locked: true
---

# Process containment

## Why

On 2026-10-18, right after a reboot and with no work started by the operator,
the laptop's load average reached 176. The resident daemon had resumed
landing a passed review and ran this repository's full test suite as the
landing gate. The test suite started about 54 detached agent wrappers that
never exited. The machine slowed until the daemon's watchdog killed the
daemon every three minutes. Each new daemon had no record of the work the
old one started, so it ran the gate again and leaked another batch.

Nothing in that chain was exotic. Tusker starts work, then forgets it
started it. This spec makes Tusker keep an inventory of every process it
starts and gives it a reliable way to stop them.

## The promise (plain language)

1. **Every process Tusker starts is accounted for.** Either a live record
   owns it, or it is stopped within about a minute of the janitor noticing.
2. **Only agent sessions may outlive the daemon.** They hold paid context
   worth keeping across a daemon restart or upgrade. Everything else
   (landing gates, checks, test suites, helpers) ends with whatever started it.
3. **Running long is not the same as being broken.** A three-hour agent run
   that keeps producing work is left alone. Tusker never kills an agent
   session only because of how long it has run.
4. **A crash or restart never runs the same work twice.** A restarted daemon
   finds what the previous one started and either adopts it or stops it.
5. **The operator can see and stop all of it.** Every running Tusker process
   appears in the UI with what it is for, how long it has run and its last
   sign of life, with a Stop action.

## Two kinds of work

| Kind | Examples | Cost to lose | May outlive the daemon | Stopped automatically when |
| --- | --- | --- | --- | --- |
| Agent session | Codex, Claude, Devin, Muse runs | High: tokens and context | Yes | Its owner record is gone, or the operator stops it |
| Check | Landing gate, closeout check, verification command, build, test suite | Low: rerun it | No | Its starter exits, its owner record is gone, or it is silent past the check limit |

## How it works

### The inventory

Every process Tusker starts gets one inventory row, written before or
immediately after the process starts:

| Field | Purpose |
| --- | --- |
| Owner | What it is for: the attempt, gate run or check run that started it |
| Kind | Agent session or check, which decides the rules above |
| PID, process group and start time | Control: stop the whole tree with one signal; the start time makes sure a reused PID never gets killed by mistake |
| Starter | The daemon instance (or CLI process) that launched it |
| Started at, last sign of life | Whether it is working or silent |

Agent wrappers already record PID, process group and start time on their
run row. Checks record nothing today; that is the main gap.

Every started process also carries its owner identity in its environment
and command line, so the janitor can recognize a Tusker process even when
its inventory row was lost.

### Checks end with their starter

A check runs in its own process group. Its starter stops that group when:

- the check finishes or is cancelled,
- the starter shuts down, including the watchdog exit path,
- the check has been silent for longer than the check limit.

A check stopped for silence records the outcome **stuck, stopped**, which
is different from a failed check. A hung test must not read as a red test.

### One janitor

The daemon runs one reconciliation pass at startup and on its normal poll
cadence. It compares running Tusker processes against live owner records and
stops any process whose owner is gone. There is no per-process watching
loop. The startup pass matters most: that is the moment the 2026-10-18
incident went wrong.

The janitor stops a process only after its PID, process group and start
time all match the inventory. If identity cannot be confirmed, it reports
the process and leaves it alone.

### A restarted daemon never repeats a gate

A landing gate in progress is recorded with its owning daemon instance. A
restarted daemon that finds a gate whose owner is dead stops that gate's
process group before running it again, so at most one copy of a gate runs at
a time. If restarts keep interrupting the same gate, the crash-loop breaker
pauses automation rather than starting the gate again. A gate that finishes and
fails is not cached: the failure goes back to the worker, and a flaky test can
pass on the next attempt.

Gates get one global slot by default, so two projects cannot run full test
suites at the same time on one laptop.

### Silent agent sessions are flagged, not killed

An agent session with no sign of life (no new public message, tool activity
or run event) for longer than the silence limit is marked **silent** in the
UI and emitted as a signal. Containment never stops a silent agent session
by itself. Deciding whether a silent or erroring session is stuck belongs to
the run-triage layer, which reads the task and the session's messages.

### The watchdog

The watchdog keeps its job of restarting a wedged daemon. Two changes:

- Before exiting, it stops the process groups of the checks its daemon
  started.
- Every restart counts toward the crash-loop breaker, including restarts
  that happen while landing. When the breaker opens, automation for the
  affected work pauses and the UI says why, instead of restarting forever.

### Tests never leak

The test suite must not start real detached agent wrappers. Tests use a
test runner that stays in the test's process group. The suite ends with a
leak check that fails if any process it started is still alive.

## Defaults and settings

| Setting | Default | Where |
| --- | --- | --- |
| Check silence limit | 10 minutes | Project settings |
| Agent silence flag | 20 minutes | Project settings |
| Concurrent landing gates (global) | 1 | App settings |
| Janitor cadence | At startup and every daemon poll | Fixed |

Values are adjustable so a repository with a slow test suite can raise the
check limit. There is no wall-clock limit for agent sessions.

## Guardrails

- **Safety kills never depend on a model.** Owner-gone and silent-check
  stops are plain code. The run-triage layer may flag, nudge or recommend,
  but containment never waits on its answer.
- **Never kill on uncertain identity.** PID, process group and start time
  must all match. Otherwise the janitor only reports.
- **Never kill what Tusker did not start.** The janitor acts only on processes
  that carry a Tusker owner identity and appear in, or were recorded by, the
  inventory.
- **Every automatic stop leaves a durable event** naming the process, owner,
  reason and the rule that fired. The UI shows it.

## Operator view

The existing Execution Operations page gains a **Processes** section:

| Column | Shows |
| --- | --- |
| What | Task or gate title and kind (agent session or check) |
| State | Running, long-running, silent, stopping, stopped (with reason) |
| Runtime and last sign of life | Two separate clocks |
| Owner | Link to the task, attempt or gate run |
| PID and process group | For terminal use |
| Action | Stop (stops the whole process group, records operator intent) |

Recently stopped processes stay listed with their stop reason for 24 hours.
Project settings gain the two silence limits.

## Deferred (not now)

- **Run triage (next spec):** rules plus an LLM reading the task contract,
  last messages and logs to judge progressing, stuck, error-looping, blocked
  or false success, then leave, nudge, stop or escalate.
- Moving execution off the operator's laptop (remote box or container).
- CPU or memory limits per process.
- Pause and resume of checks.

## Related

[[run-session-continuity]] owns readable run activity, the source of an agent
session's sign of life. [[runner-execution-boundary]] owns how runners are
launched. [[tusker-trust-and-efficiency]] lists orphan workers as a trust risk;
this spec is the mechanism.
