---
title: "05. Runner And Session Protocol"
description: "Status: Draft Scope: daemon-run execution for codex and claude-code"
tusker:
  audience: "developer"
  canonical_status: "historical"
  deprecated: true
  owner_epic: "ORC"
  publish_path: "developer/specs/05-runner-and-session-protocol"
  publish_section_title: "Specs"
  route: "/developer/specs/05-runner-and-session-protocol/"
  source_kind: "repo_doc"
  source_path: "docs/specs/05-runner-and-session-protocol.md"
  summary: "Status: Draft Scope: daemon-run execution for codex and claude-code"
  superseded_by: "/user/start-here/agent-workflow/"
  tags:
    - "specs"
  updated: "2026-04-29"
  verified_at: "2026-04-28"
---

# 05. Runner And Session Protocol

Status: Draft
Scope: daemon-run execution for `codex` and `claude-code`

## Decision

The runner/session layer owns execution protocol, not tracker state.

- durable tracker state stays in notes per Spec 01
- live execution state stays in SQLite per Spec 03
- raw runner artifacts live under the daemon state root, not in frontmatter
- one attempt is one worker session
- one attempt may contain many turns
- Codex app-server is the first-class orchestration runner
- Claude Code is supported only to the level its adapter can honestly observe and resume

## Goals

- start, monitor, reconcile, stop, and resume unattended runner sessions
- normalize Codex and Claude Code into one execution protocol
- survive daemon restarts and machine reboots
- keep high-volume run detail out of markdown

## Non-Goals

- arbitrary runner plugin marketplace
- fake feature parity between Codex and Claude Code
- storing transcripts or heartbeats in notes

## Ownership Split

| Concern | Owner |
|---|---|
| durable ticket state | vault note |
| lease state | `daemon.db` |
| attempts, turns, and sessions | `daemon.db` |
| raw event streams and logs | `<state-root>/runs/` |
| review policy | Spec 06 |

## Core Objects

### Attempt

One worker session against one frozen `work_revision`.

Fields:

- `attempt_id`
- `project_id`
- `record_id`
- `item_id`
- `work_revision`
- `runner`
- `lease_state`
- `outcome`
- `workspace_path`
- `session_ref`
- `started_at`
- `finished_at`

### Turn

One model turn inside an attempt.

Fields:

- `turn_id`
- `attempt_id`
- `turn_index`
- `runner_turn_id`
- `status`
- `failure_class`
- `input_tokens`
- `output_tokens`
- `total_tokens`
- `latest_event_at`
- `started_at`
- `completed_at`

### Session

Runner-native conversational/execution context attached to one attempt.

### Artifact bundle

Files written under:

```text
<state-root>/runs/<project_key>/<record_id>/
```

Typical files:

```text
attempt-0001.events.jsonl
attempt-0001.raw.log
attempt-0001.prompt.md
attempt-0001.summary.json
```

## Internal Execution States

These are runtime states, not note states.

### Lease state

| State | Meaning |
|---|---|
| `unclaimed` | no active execution |
| `claimed` | reserved before confirmed launch |
| `running` | session live |
| `retry_queued` | waiting for retry backoff |
| `released` | no longer held by daemon |

### Attempt outcome

| Outcome | Meaning |
|---|---|
| `none` | no terminal result yet |
| `succeeded` | attempt completed and handed off to review |
| `blocked` | waiting on human or external unblock |
| `failed` | terminal failure for this attempt |
| `cancelled` | intentionally stopped |
| `abandoned` | lost session or expired lease |

## Runner Interface

```go
type Runner interface {
    Name() RunnerName
    Capabilities() RunnerCapabilities
    Start(ctx context.Context, req StartRequest) (*StartResult, error)
    Resume(ctx context.Context, req ResumeRequest) (*ResumeResult, error)
    Reconcile(ctx context.Context, req ReconcileRequest) (*ReconcileResult, error)
    Interrupt(ctx context.Context, req InterruptRequest) error
    Collect(ctx context.Context, req CollectRequest) (*CollectResult, error)
}
```

### Capabilities

```go
type RunnerCapabilities struct {
    StructuredEvents    bool
    ResumeSession       bool
    ExplicitApprovals   bool
    Heartbeats          bool
    MachineFinalStatus  bool
    UsageMetrics        bool
    ArtifactEnumeration bool
}
```

### Capability matrix

| Capability | Codex | Claude Code | Rule |
|---|---|---|---|
| structured events | primary path | adapter-generated or parsed | normalized events are mandatory |
| resume same ticket/session | yes | required where native session refs are valid | if unsupported, resume becomes a new supervised attempt with explicit reason |
| explicit approvals | yes | partial | degrade to blocked/human-needed cleanly |
| heartbeats | yes | wrapper-generated if needed | daemon never trusts silence |
| machine final status | yes | partial | adapter must classify exit honestly |

Codex and Claude Code both need honest same-ticket resume semantics. Codex has the stronger app-server protocol today. Claude Code may need adapter-generated events and stricter proof before the daemon trusts a resume. Fake parity is not allowed.

## Adapter Responsibilities

Each adapter owns:

- spawning the runner with workspace + prompt
- persisting runner-native session refs
- translating raw output into normalized events
- emitting synthetic heartbeats if needed
- classifying final result into `succeeded`, `blocked`, `failed`, `cancelled`, or `abandoned`
- collecting raw artifacts

The daemon owns:

- lease bookkeeping
- attempt creation
- retry scheduling
- review packet generation
- durable tracker-state writes

## Start / Resume Contract

### `StartRequest`

| Field | Meaning |
|---|---|
| `ProjectID` | owning project |
| `RecordID` | immutable work item id |
| `ItemID` | current human id |
| `AttemptID` | new attempt id |
| `WorkRevision` | durable work revision |
| `WorkspacePath` | isolated workspace |
| `PromptPath` | rendered prompt packet |
| `EventSinkPath` | normalized JSONL sink |
| `RawLogPath` | stdout/stderr capture |
| `Budget` | token/cost/time caps |

### `ResumeRequest`

Resume is allowed only when:

1. same `attempt_id`
2. same `work_revision`
3. same intended task, no review-driven scope reset
4. same runner-native session can be proven compatible

Resume examples:

- daemon restart
- machine reboot with resumable session intact
- cleared approval blocker for same attempt

New-attempt examples:

- human requested rework
- acceptance criteria materially changed
- session cannot be safely resumed

## Supervisor / Fork Contract

The daemon owns the run-supervisor decision. A model auditor may assist when workflow policy asks for judgment, but the auditor does not become a second scheduler.

| Decision | Runner adapter requirement |
|---|---|
| `continue_thread` | Start next turn on the same compatible native session. |
| `resume_session` | Prove native session continuity for the same `record_id`, `work_revision`, runner, and workspace metadata. |
| `fork_thread` | Start a fresh native session for the same ticket and branch, seeded by workpad/review packet/artifacts. |
| `new_branch` | Start a fresh branch/workspace lineage under the same ticket and record parent attempt/session. |
| `new_revision` | Refuse old-session resume; the daemon increments or observes a new `work_revision`. |
| `stop_for_audit` | Return a blocked/cancelled/released status with a human-readable reason. |

Use `fork_thread` when the issue is context pressure or conversation confusion but the implementation path is still valid. Use `new_branch` when the implementation path itself needs isolation. Use `new_revision` only when the durable task intent changed.

The runtime decision record must be specific enough to audit later. For branch decisions, record parent attempt/session, target attempt/session when known, branch name, workspace path, reason, validation delta, merge/discard rule, and token/context signal if relevant. A missing branch decision is a bug; guessing from workspace paths after the fact is not acceptable.

## Continuation Contract

Continuation is not resume.

Continuation means: same attempt, same runner process or session, same Codex thread, next turn.

Rules:

- Turn 1 receives the full rendered `WORKFLOW.md` body.
- Later turns receive continuation guidance only.
- The daemon checks tracker state after each completed turn.
- If the note remains dispatchable and `agent.max_turns` is not exhausted, start another turn.
- If the note is no longer dispatchable, stop the attempt and reconcile.
- If `agent.max_turns` is exhausted, stop and classify according to workflow policy.

Resume means reattaching to an existing attempt after daemon restart, machine sleep, process interruption, or approval unblock.

Continuation is the steady-state loop. Resume is recovery.

## Reconciliation

On each poll tick the daemon asks the adapter to reconcile running attempts:

- session still alive -> keep `running`
- session blocked on human -> outcome `blocked`, lease released or parked
- session vanished -> outcome `abandoned`
- session finished cleanly -> outcome `succeeded`
- session failed -> outcome `failed`

## Go File Mapping

| File | Responsibility |
|---|---|
| `runner.go` | common interface |
| `runner_codex.go` | Codex adapter |
| `runner_claude.go` | Claude Code adapter |
| `session_store.go` | session and attempt persistence helpers |
| `turn_store.go` | turn persistence helpers |
| `event_log.go` | normalized event stream writer |
| `reconcile.go` | adapter reconciliation loop |
