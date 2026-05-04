# Orchestration Runbook

The public V5 CLI tracks task truth. Runtime orchestration is an operator/internal surface and must not be presented as the normal task workflow.

## Current Contract

| Surface | Status | Operator truth |
|---|---|---|
| Markdown task lifecycle | Shipped | `active` and `rework` are runnable states in `WORKFLOW.md`; `ready` is shaped work, not dispatch. |
| Obsidian task visibility | Shipped | `Tasks.base`/`BugTasks.base` show `Active`, `Blocked`, `Review`, and `Follow-up` (`rework`) views. |
| Dashboard live-run panel | Shipped as generated markdown | `tusker reindex` renders live runs from the runtime store when the vault has a registered project; no live runs renders an explicit empty state. |
| Runtime store | Internal code exists | Projects, runs, attempts, turns, sessions, events, usage, and supervisor decisions are modeled outside task frontmatter. |
| `daemon`, `projects`, `runs`, `refresh` CLI commands | Shipped as operator/internal commands | They register projects, run or tick the daemon, inspect attempts/turns/events/logs, interrupt runs, and manage concurrency limits. |
| Codex-only status-to-review daemon loop | Shipped for local operator use | A registered project plus `daemon run` or `refresh` can pick up `active`/`rework` tasks, run Codex, and write review packets when Codex moves the task to `review`. |

## Durable State

- Task status lives in markdown: `draft`, `backlog`, `ready`, `active`, `blocked`, `review`, `rework`, `done`, `cancelled`.
- Runtime state lives outside markdown under the default state root: registered projects, run leases, attempts, turns, sessions, event tails, raw logs, prompt packets, status files, and supervisor decisions.
- Do not put leases, process ids, retry timers, token totals, session refs, or raw transcript state into task frontmatter.

## Honest Codex Loop

```text
ready --human claim/status--> active
active/rework --daemon eligible state--> runtime run
runtime run --Codex changes task to review--> review packet + review
runtime run --task remains active/rework--> continuation retry
review --human verify--> done
review --needs more work--> rework
```

What the code supports:

- The workflow default runner is `codex`, with `codex app-server` in `WORKFLOW.md`.
- Dispatch eligibility is driven by `tracker.active_states`, currently `active` and `rework`.
- A task with unresolved blockers or `risk: critical` is not dispatched automatically.
- If a running task leaves `active`/`rework` without a completed runner status, reconciliation releases the run.
- If Codex writes a completed status file after moving the task to `review`, reconciliation records the run as succeeded and writes the review packet instead of abandoning it as inactive.
- If the runner exits cleanly while the task is still `active`/`rework`, the daemon queues continuation instead of pretending the work is reviewable.
- Review packet generation is supported after a clean run has a non-active task state; the packet summarizes attempts, turns, artifacts, supervisor decisions, and residual verification risk.

What is deliberately not automated:

- Automatic close. Humans must still verify and close.
- Blind promotion from `active` to `review`. Codex or a human must move the task state when the work is actually ready.
- Ignoring stale tracker truth. Non-active/non-review state changes while a run is live release or interrupt the run for audit.

## Operator Commands

```bash
tusker projects add --repo . --vault ./tusker
tusker daemon status
tusker refresh --quiet
tusker daemon run
tusker runs inspect <TASK-ID> --json
tusker runs events <TASK-ID> --lines 50
tusker runs logs <TASK-ID> --lines 80
tusker runs interrupt <TASK-ID>
```

Use `refresh --quiet` for smoke tests and `daemon run` for a long-lived local loop.

## Human Loop

Use the shipped task lifecycle when the runtime surface is unavailable:

```bash
tusker status <TASK-ID> active --actor <name>
tusker evidence <TASK-ID> packet <file-or-url> --note "<summary>"
tusker status <TASK-ID> review --actor <name>
tusker verify <TASK-ID> --by <name>
tusker close <TASK-ID> --by <reviewer>
```

Raw logs are useful evidence only after they are summarized into a readable packet. The daemon-generated review packet is the preferred packet evidence for Codex runs.

## Obsidian Loop

Open `Dashboard.md`.

| Section | Source | Meaning |
|---|---|---|
| Active work | `Tasks.base#Active` | Tasks in `status: active`. |
| Live runs | generated markdown block | Active runtime leases for the registered project, if any. |
| Blocked | `Tasks.base#Blocked` | Tasks waiting on dependencies or an explicit blocker. |
| Review | `Tasks.base#Review` | Tasks waiting for human verification. |
| Follow-up | `Tasks.base#Follow-up` | Tasks in `status: rework`. |

There is no shipped `Orchestration.base`. Do not reference it in dashboards or operator instructions.
