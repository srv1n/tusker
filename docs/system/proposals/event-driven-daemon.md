---
kind: proposal
subject: event-driven-daemon
title: "Event-driven daemon: work happens when something changes"
keywords: [poll, reconcile, reconciliation, wake, fingerprint, hash, cpu, frontier, watcher]
part_of: overview
describes: [cmd/tusker/daemon.go, cmd/tusker/adaptive_reconcile.go, cmd/tusker/direct_wave_authority.go, cmd/tusker/review_verification_workspace.go, cmd/tusker/daemon_external_loop.go, cmd/tusker/review_pass.go]
status: accepted
code_conformance: unverified
created: 2026-10-19
last_verified:
read_when: "Changing what makes the daemon do work: polling cadence, wake-ups from the CLI or runners, full reconciliation, or when proof and wave material are re-checked."
skip_when: "Changing what a run or check does once started; see process-containment."
updates: [process-containment, direct-wave-authoring]
sources: []
decisions_locked: true
---

# Event-driven daemon: work happens when something changes

## Why

On 2026-10-19 the resident daemon held one to two CPU cores for an hour.
After the log-redaction fix, most of what remained was the daemon
re-checking the same things every five seconds while any run was live: it
reloaded every task file several times, and for each member of an armed
wave it read and hashed every file in that member's workspace up to four
times. Nothing had changed between those checks. The operator's point:
every state change already goes through the Tusker CLI or through a process
the daemon itself started, so the daemon already knows when something
changed. It should not keep rediscovering the world on a timer.

## The promise

1. **Work is triggered by change.** A CLI command, a runner reporting
   progress or finishing, or a process exiting wakes the daemon, and it
   handles what changed. Nothing else makes it do real work.
2. **A live run costs a heartbeat, not a full pass.** While agents run, the
   daemon checks they are alive and on budget. It does not reload the vault
   or re-hash code to do that.
3. **Idle projects cost nothing.** A project is active only while it has a
   live or retry-queued run, an armed wave with members left, or recent
   operator interaction (a CLI command in it, or opening or clicking it in
   the UI). Being registered or shown in the sidebar does not make it
   active. The daemon does no background work for an idle project: no
   rescan, no timer, no file checks.
4. **Rescans happen on interaction, not on a clock.** There is no periodic
   full reconcile. A project is fully rescanned at daemon startup only if it
   has live work, when the operator interacts with it, and when an active
   project's task files changed outside the CLI. Hand edits to an idle
   project (for example in Obsidian) are picked up the next time the
   operator touches that project.
5. **Proof is checked when it is used.** Whether passing checks still match
   the code is decided at review, landing and closeout, not on every poll.
   The test is git's own: the commit the proof ran on is still the commit
   being landed, and nothing in the task's files is uncommitted.

Safety does not get weaker. Authorized work still cannot run or land on
material different from what was authorized; the check moves to the moment
it matters.

## What already exists (verified 2026-10-19)

- The CLI already wakes the daemon over its control socket with the files
  it changed. Runners already report status to the daemon.
- There is no file watcher. A full reread is forced every five minutes as
  the "external edit deadline".
- Any live run puts its project on a five-second full pass.
- `TUSKER_RECONCILIATION_MODE=event` already turns periodic passes off.

## Decisions (planner defaults; operator may adjust)

| # | Decision | Default |
|---|---|---|
| D1 | Live-run cadence | Heartbeat only (liveness, budget, silence) every 5 s; no vault load, no hashing. |
| D2 | Full reconcile | Never on a timer. At startup only for projects with live work; on operator interaction with the project (CLI command, opening or clicking it in the UI, debounced); on outside edits to an active project. Scheduled wake-ups (retry backoff, budgets) stay as exact timed events. |
| D3 | Outside-edit detection | Active projects only: a stat-only signature (file count and newest modification time of task, wave and spec files, plus git ref files) checked on the heartbeat. No file reads, no watcher, no new dependency. Idle projects: none; interaction triggers the rescan. |
| D6 | Active project | Live or retry-queued run, armed wave with unfinished members, or interaction within the last 10 minutes. |
| D4 | Proof freshness test | Commit unchanged and task scope clean in git. A content hash is kept only for ignored generated outputs, which git does not track. |
| D5 | Wave frontier advance | On wave, task or spec change, on a run finishing or landing, and on full reconcile. Never on a timer. |

## For developers: ordered changes

Measured or estimated costs come from the read-only audit of 2026-10-19
(`TestPerfPollOnce` with `TUSKER_PERF=1` measures each step).

1. **Frontier off the poll.** `daemon.go:1532` advances every wave every
   poll; `buildDirectWaveReview` (`direct_wave_authority.go:454`) reloads
   the vault, fingerprints the wave three times and hashes every member.
   Run it only on D5 events. Largest saving.
2. **Proof material by git.** Delete the immediate re-hash in
   `review_verification_workspace.go:145`. Compare `HEAD` to the submitted
   `EndState.HeadSHA` plus `git status --porcelain --untracked-files=all
   -- <scope>`, as `reviewWorktreeMatchesSubmission` already does
   (`:110-119`). Keep `gate_ledger` hashing for generated roots only.
3. **Lazy review-task check.** `daemon_external_loop.go:864-885` hashes the
   workspace before checking that any close event exists. Check events first.
4. **No vault load per review candidate.** `review_pass.go:102` calls
   `resolveV7Note` per task; use the notes the pass already loaded.
5. **UI refresh only on real change.** `daemon.go:2063-2068` invalidates
   the serve snapshot on every non-warm pass; invalidate only when canonical
   state changed.
6. **Activity gate and heartbeat tier.** Replace the warm/cool/cold
   periodic tiers in `adaptive_reconcile.go` with active vs idle (D6); split
   the live tier into heartbeat-only; remove the five-minute external-edit
   deadline (`daemon.go:1104,1179`) in favor of D3; serve requests for a
   project send an interaction wake (debounced) over the existing control
   path.
7. **Cross-poll caches.** Key wave fingerprints on `state_rev` plus spec
   modification times, and armed-wave projections on the branch tip commit.

Acceptance for the set: with one live run and a 20-member armed wave, a
heartbeat pass spawns no git process and reads no task body; an idle daemon
with the UI open stays under 2% CPU averaged over five minutes; an idle
registered project causes no file reads or git processes at all; every
existing safety test (authorized material, proof reuse, wave drift) passes
unchanged.

## Deferred (not now)

- A file watcher (FSEvents). The stat signature on active projects and
  rescan-on-interaction cover it without a dependency.
- Run triage (judging a run by its messages). Separate spec.
- Moving the daemon out of TuskerBar so closing the app does not stop work.
  Separate decision.
