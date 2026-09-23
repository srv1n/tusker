---
subject: run-session-continuity
keywords: [run, messages, ACP, CLI, reconnect, resume, pause, force-close]
part_of: overview
describes: [cmd/tusker/serve_runs.go, cmd/tusker/run_runtime_commands.go]
status: canonical
created: 2026-09-22
last_verified:
read_when: Implementing readable run activity and safe session continuation.
skip_when: Working on unrelated wave admission or task authoring.
sources: []
updates: [execution-observability]
decisions_locked: true
---

# Readable activity and safe session continuation

## Outcome

An operator can read recent public agent messages and tool activity, inspect
the last known operation, and reconnect or continue after closing the UI or
losing a worker. A heartbeat is not evidence of forward progress.
This is a target contract, not a claim of installed behavior.

## Locked behavior

- Reuse existing run history, RuntimeStore, provider adapters and admission
  authority. No second scheduler, transcript store or independent UI state machine.
- Keep job, attempt and native session identities distinct. Separate capture
  and provider timestamps; never invent timestamps.
- Default to recent public messages and tools. Preserve multiline text, redact
  secrets, exclude hidden reasoning, expose truncation/unavailable capture,
  and retain raw diagnostics as a secondary view.
- Show heartbeat, last message and last tool progress separately. Silence means
  no observed progress, not definitely hung. Show permission waits when exposed
  by the provider; unknown remains unknown.
- Reconnect only observes the surviving attempt: zero launches.
- Continue creates a new attempt of the same job using the same native session
  only when the adapter supports it and identity/material fences match.
- Recover from saved context explicitly creates a different native session;
  never silently substitute it for native continuation.
- Pause requires genuine acknowledged provider support. Otherwise offer Stop
  and resume later when recovery is possible, or Stop. No OS freeze masquerading
  as durable pause. Existing wave Pause remains admission-only.
- Stop persists intent, suppresses automatic retry and completes only after
  ownership/process settlement. Start fresh explicitly creates a new session
  after settlement, preserving history, authority and budgets.

## Reopening and uncertainty

Read canonical attempt, lease, process start identity and provider receipts.
A live matching owner is reconnectable; a recorded completion is terminal.
Missing or contradictory evidence remains outcome unknown. Neither a heartbeat
nor an expired lease alone proves safe continuation.

Retain the last completed action and unresolved operation using existing
checkpoint/coordination data. Before retry, reconcile provider receipts, process
outcome, outputs and relevant Git state. An unresolved external effect requires
a specific operator decision, not blind replay. A displayed message is not a
checkpoint or an exact-resume guarantee.

Actions are fenced by job/attempt/generation/material and durably idempotent.
Two windows, lost responses or restart must not create two workers. Pending
intent survives restart until canonical readback resolves it; stale responses
cannot replace newer state.

## Existing work and boundaries

Local message-feed changes exist in run_activity.go, runner_acp.go, serve_runs.go
and the run-detail UI. Focused Go checks, 21 UI tests and both builds passed
before authoring: historical local evidence, not new-task or installed proof.
Reuse TSK-T-0031 recovery lineage, TSK-T-0032 unknown-outcome admission and
TSK-T-0034 refresh. Do not duplicate TSK-T-0009/0045 wave/task UI work.
Serialize shared ownership; preserve unrelated dirty files and retained state.
Task creation does not authorize installation, interruption, daemon launch or
dispatch.

## Qualification and handoff

Exercise actual adapters and durable stores for force-close, restart, duplicate
actions, stale identities, native/context recovery, uncertain effects and lost
acknowledgements. Browser checks use an isolated disposable runtime and actual
readback. Label source, fixture, browser, installed and live-provider proof
separately. Unsupported capability is honest; fabricated pause/resume is not.

Author/origin: local native conversation
01a0c8f4-04c9-7b22-9e97-26eb27a6f404, unbound provenance, not a verified registered
execution route. Return conflicts or changes to locked decisions to the operator
with task/acceptance IDs, facts, exact decision needed and recommendation.
