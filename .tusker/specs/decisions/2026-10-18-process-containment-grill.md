---
subject: process-containment-grill
title: "Process containment grill, 2026-10-18"
part_of: process-containment
decides_for: process-containment
status: canonical
created: 2026-10-18
read_when: "You want why process containment is shaped the way it is: the incident, the options weighed, and what the operator said."
skip_when: "You only need the current rules; read [[process-containment]]."
---

# Process containment grill, 2026-10-18

## Incident that started it

The operator saw dozens of `tusker.test` processes and several `tusker-daemon`
entries in Activity Monitor, with load average around 176. They had rebooted
and had not started any Tusker work. Investigation showed:

- The resident daemon ran this repository's landing gate (`go test ./...`)
  after the reboot.
- Daemon tests spawned detached runner-wrappers (`Setsid`) using the test
  binary. About 54 outlived the suite, some for two hours, each at ~4% CPU.
  They kept renewing their lease through an already-open database handle
  and polled every 100 ms for a status file whose directory was deleted.
- Polls slowed past three minutes; the watchdog killed the daemon with exit
  code 70 every ~3 minutes; launchd restarted it. Each restart reran the
  gate. The crash-loop breaker (5 restarts per 10 minutes) never tripped.
- The "multiple daemons" were overlapping old and new instances during restarts.

The operator wanted it fixed "once and for all", and framed Tusker against the
Anthropic talk "Why One AI Agent Isn't Enough (And How to Run 1,000)". From
that talk: Managed Agents keep brain and hands separate, and use declare,
contain, detect. Tusker is strong on declare and detect and weak on contain.

## Q1. What does Tusker promise about processes?

- Offered (recommended): every process is accounted for; only agent runners may
  outlive the daemon; everything else dies with its starter; restarts never
  repeat work.
- Alternative: nothing outlives the daemon; runners are killed and resumed
  by session ID.
- Operator: "agreed".
- Locked: the recommended promise.

## Q2. How does a surviving runner know it is orphaned?

- First offer: each runner re-checks its request file, state directory and
  record by path every heartbeat.
- Operator pushed back: per-process checks every 15 seconds sound like a
  nightmare. They asked to step back to a PM view: Tusker spawns work, chose
  detachment for resource reasons, and the issue is what happens when
  something hangs or restarts.
- Reframed: the problem is that Tusker keeps no inventory of what it starts.
  Proposed labels plus one janitor.
- Operator refined: a label is not enough; Tusker needs a real OS handle
  (PID) to control processes: kill when something has gone off, and inspect
  when it runs long. "Sometimes this might run for three hours. Doesn't mean
  we kill it."
- Locked: inventory rows with owner, kind, PID, process group and start time;
  one janitor at startup and on poll cadence; no per-process polling loops;
  no wall-clock kill for agent sessions. Running long is not broken; silence
  is the only time-based signal.

## Q3. What counts as stuck, and who decides?

- Offered: silence thresholds (agents flagged at 20 min, stopped at 60;
  checks stopped at 10).
- Operator: agent runs already expose their last messages and that should
  drive visibility. Silence or errors need judgment: an agent fixing errors is
  different from an agent stuck erroring, and "success" in text is different
  from real success. They proposed giving Tusker its own intelligence layer:
  rules first, and an LLM that reads the task and the run's messages and
  decides on the operator's behalf.
- Fact found: `run-session-continuity` already locks readable activity
  (last message, last tool, heartbeat shown separately; "silence means no
  observed progress, not definitely hung").
- Offered (recommended): split into Containment now (control: inventory,
  janitor, check limits, test guard, silent-agent signal) and Run triage next
  (judgment). Safety kills never depend on a model.
- Operator: "agreed on this. Let's build this out", and asked to also think
  about guardrails and the UI for configuration and viewing.
- Locked: the split. Agent sessions are flagged when silent and never
  auto-stopped by containment. The 60-minute auto-stop is dropped and left
  to triage. Checks stop after 10 minutes of silence with outcome
  "stuck, stopped".

## Defaults chosen by the planner (operator may adjust)

- Check silence limit 10 minutes; agent silence flag 20 minutes; both in
  project settings.
- One global landing-gate slot.
- An in-progress gate marker owned by the daemon instance ensures one copy of a
  gate at a time; the crash-loop breaker stops repeated restarts. Failed gates
  are deliberately not cached, because a flaky test would then block a commit
  permanently.
- The UI lives on the existing Execution Operations page, not a new screen.
