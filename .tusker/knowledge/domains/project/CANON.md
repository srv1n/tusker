---
schema: "tusker.domain-canon/v7"
kind: "domain_canon"
id: "project/canon"
project: "tusker"
domain: "project"
title: "Project Canon"
status: "current"
summary: "Current durable rules for Tusker's own repository."
source_of_truth:
  - ".tusker/SKILL.md"
  - ".tusker/WORKFLOW.md"
  - "tusker.yaml"
created_at: "2026-06-04 00:00:00 +0000 UTC"
updated_at: "2026-07-10T04:45:40Z"
state_rev: "sha256:dc7c3334d4cc2ed5aab79acf0f3560b29f4223eecb52b3d94e26646f7ecb29ba"
---

# Project Canon

## Current Truth

- V7 is the only product surface in this repository.
- Durable task status never uses `active`.
- Dispatchable task states are `ready` and `rework`.
- Runtime activity is represented by run leases, attempts, sessions, and workspaces.
- Canonical task state owns runtime liveness: when a task is closed, cancelled, superseded, or moved back to backlog, every non-terminal runtime row for that task is retired with an actor/reason audit stamp.
- Every attempt-creating path uses the shared attempt-cap guard before dispatch. Fresh dispatch, failure retry, continuation retry, reclaim replacement, and redriven retry-queued dispatch all count against the active redrive window; reclaim-caused replacements are not free attempts.
- Runner exit classification is tracker-aware on every outcome write path, including daemon status observation and wrapper direct-store recording: exit code 0 with tracker state still in `ready` or `rework` is attempt outcome `early_exit`, not clean completion, and counts against the no-progress continuation cap.
- Codex exec owns the inner agent loop for the local Codex lane: Tusker launches one `codex exec --json` process per attempt, ingests `thread.started` and `turn.*` JSONL events, resumes later attempts with `codex exec resume <session-id>` when safe, and treats `max_turns` and budget as process governors.
- Codex exec event-stream silence is not evidence of runner death while raw JSONL shows a command execution started and not completed; the heartbeat watchdog uses `codex.turn_timeout_ms` as the in-flight command cap and reserves the shorter idle heartbeat reap for true silence.
- Human-owned gates set `agent_action: stop_until_human_response` and `readiness: waiting_on_human`.
- Tags are projections; typed frontmatter is source of truth.
- Obsidian Bases and dashboards are generated views, not canonical state.
- Browser-backed ChatGPT work is a runner result source, not a direct state writer.
- Waves are first-class V7 batch records; membership is canonical on `kind: wave`, task `wave:` is a reconcile-maintained back-pointer, and wave `status` is derived from member task closure.
- Merge landing is wave-scoped: `tusker wave create` cuts `integration/W-####`, wave task worktrees branch from that integration branch as `task/<TASK-ID>`, `tusker land` serializes batch merges through a gated staging worktree, and completed waves land to the configured default branch as one merge commit.
- Terminal task state is monotone under merge and reconcile. A task in `done`, `cancelled`, or `superseded` may leave terminal state only through an explicit Tusker control operation that mints a fresh `state_rev`; stale branch content or stale object-rev repair must fail with a CAS conflict instead of certifying a non-terminal rewind.
- Project registration quarantine is a loader property: entry points that scan registered projects use the shared loader, which records failed enabled registrations as `health: error` with `last_error` and continues loading unrelated healthy projects.
- Repository validation is serialized across linked worktrees by one validation gate. Makefile Go build, vet, and test phases default to two Go scheduler threads and one package/test lane; the `cmd/tusker` TestMain also acquires the gate so raw focused tests cannot overlap a broad shared-state suite. Helper test re-execs inherit the held lease.

## Invariants

- A ready/rework task must have concrete acceptance and verification proof.
- Placeholder acceptance must block dispatch.
- A task in canonical terminal/backlog state must not retain a non-terminal runtime row; the invariant circuit is last-resort containment for rows the close/status/reconcile retirement sweep failed to reach.
- A run at its attempt cap parks before any new attempt is created. `attempt_count_within_caps` is a corruption/operator-surgery sentinel, not a normal dispatch control path.
- Raw CLI output belongs in runtime scratch/logs, not task markdown.
- `tusker automation plan <task> --json` is the canonical pre-dispatch explanation.
- High and critical risk closeout requires human acceptance.
- Legacy V5/V6 docs, publication manifests, site export state, and checked-in event history are not default read paths.
- Targeting a quarantined project may fail loudly, but unrelated quarantined registrations must not fatal daemon run/resume, status/list, automation, all-project listing, or serve paths.

## Verification

Use focused proof first:

```bash
go test ./cmd/tusker -run <test-name> -count=1
```

Use broad proof before closing cross-cutting changes:

```bash
make test
```
