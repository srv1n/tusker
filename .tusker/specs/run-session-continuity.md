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

## Harness routes and operations

Decided 2026-09-23 ([decision record](decisions/2026-09-23-harness-sessions-grill.md)).
Every harness runs headless; users pick a profile, never a transport.

| Harness | Route | Native thread identity | Resume |
| --- | --- | --- | --- |
| Claude Code | `claude -p` stream-json in and out, detached | Tusker assigns `--session-id <uuid>` | `claude -p --resume <id>` with the same MCP/settings flags |
| Codex | `codex exec --json`, detached | `thread.started.thread_id` | `codex exec <exec-flags> resume <id> -` (exec flags before `resume`) |
| Muse | `muse exec --json`, detached | Tusker assigns `--session-id <uuid>` | `muse exec --session-id <id>` |
| Devin | `devin acp`, detached | `session/new` result | `session/load` |

The long-running agent always runs under a detached wrapper that owns its
stdio and writes events to disk, so closing or restarting Tusker never ends
or blinds a run. The native identity is persisted before the first prompt
completes.

Six operations, identical across harnesses:

| Operation | Contract |
| --- | --- |
| Start | Launch detached; persist native identity early. |
| Watch | Normalize provider output into public messages, tool calls with command and result, plan/progress, typed errors. |
| Ask | Worker calls the injected Tusker MCP tool; the question lands in the mailbox. |
| Say (soft) | Pending messages reach a running worker between tool calls where the harness supports it (hook context or Claude stdin). |
| Say (hard) | Interrupt, settle, then resume the same native thread with the message as the prompt. |
| Continue | Resume the same native thread after failure/loss with a typed failure summary. Start fresh stays explicit. |

Unsupported operations are declared per driver and shown with the fallback
("delivered when the current turn ends"), never silently dropped.

## Run states shown to operators

One state model for every harness, derived from canonical run, lease and
event facts:

| State | Derived from | Allowed actions (server-declared) |
| --- | --- | --- |
| Working | live owner, events flowing | Say, Interrupt, Stop |
| Waiting on you | open question or permission request | Answer, Stop |
| Quiet | live owner, no events for the configured window, no tool running | Say, Interrupt, Stop |
| Blocked | typed reason: usage limit, auth expired, permission denied, sandbox denied, missing access | Continue after fix, Stop |
| Failed | terminal failure with typed reason and last events | Continue, Start fresh |
| Lost | owner gone without terminal status | Continue, Start fresh |
| Stopped | operator Stop or interrupt settled | Continue, Start fresh |

Queued (not yet started) and Finished (succeeded or waiting for review) are
shown without a problem reason.

Failure and blocked reasons are typed codes produced by drivers, not substring
matches on free-text errors. The server computes allowed actions with the
same preflight the action endpoint uses; the UI never guesses.

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
