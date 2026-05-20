---
title: "Workflow"
description: "Short reference for the V7 task lifecycle."
tusker:
  audience: "user"
  publish_path: "user/reference/workflow"
  route: "/user/reference/workflow/"
  source_kind: "repo_doc"
  source_path: "skill/references/WORKFLOW.md"
  summary: "Short reference for the V7 task lifecycle."
  tags:
    - "reference"
  updated: "2026-05-18"
---

# Workflow

Short reference for the V7 task lifecycle.

## Lifecycle

```text
idea -> backlog -> ready -> review -> done
                  ^          |
                  |          v
                rework <-----+

any nonterminal state -> cancelled | superseded
```

`claimed`, `running`, `leased`, and `interrupted` are runtime states. They are not task lifecycle statuses.

## Status contract

| Status | Meaning | Agent-runnable? |
|---|---|---|
| `idea` | Captured but not shaped. | No |
| `backlog` | Shaped future work, not current release/work queue. | No |
| `ready` | Shaped current work. | Yes, only if `readiness: ready` and `next_owner` is agent-owned |
| `review` | Worker claims implementation is ready for independent verification or human gate. | No |
| `rework` | Review found changes needed. | Yes, only if `readiness: ready` and `next_owner` is agent-owned |
| `done` | Accepted and closed. | No |
| `cancelled` | Intentionally abandoned. | No |
| `superseded` | Replaced by another task/decision path. | No |

## Readiness contract

| Readiness | Meaning | Agent behavior |
|---|---|---|
| `ready` | No known blocker. | May claim/continue if owner is agent. |
| `blocked_by_gate` | One or more gates block progress. | Read the gate; continue only if the gate is agent-owned. |
| `blocked_by_dependency` | Another task/decision blocks progress. | Stop unless dependency became satisfied. |
| `waiting_on_review` | Reviewer lane owns the next action. | Do not implement. |
| `waiting_on_ci` | CI/external system owns the next action. | Stop after recording a link/status. |
| `held` | Deliberately paused; with `next_owner: human:*` it is human-wait. | Stop unless explicitly released. |
| `done`, `cancelled`, `superseded` | Terminal. | Stop. |

## Ownership contract

Use `next_owner` to decide who acts:

```yaml
next_owner: agent
next_owner: agent:codex
next_owner: reviewer:agent
next_owner: human:sarav
next_owner: external:ci
```

Agent-runnable work requires:

```text
status in ready|rework
readiness == ready
next_owner == agent or next_owner starts with agent:
```

Human-owned gates are terminal for agents until the human accepts, waives, or rejects.

## Worker finish contract

Implementation is not finished just because an attempt summary exists.

A worker must:

1. satisfy `proof_mode`;
2. add/update the attempt summary;
3. request review with `tusker finish <TASK-ID> --request-review --summary "<proof map>"` when possible;
4. otherwise use a proposal/control command to move to `review`;
5. create/propose a gate for human/device/env/CI/external blockers;
6. stop when only human/external gates remain.

Never leave completed implementation work in `ready` or `rework` with only a handoff summary. Handoff is attempt state; `review` plus proof/gates is task state.

## Review and close

- The implementation worker does not self-certify.
- Low/medium work may be closed by an allowed independent reviewer when proof, gates, docs impact, and policy pass.
- High/critical work may receive reviewer advice, but final close requires the configured human/reviewer policy.
- Failed review moves to `rework` with specific unmet acceptance items.
- Human-only review becomes `readiness: held`, `next_owner: human:*`, and `agent_action: stop_until_human_response`.

## Gates

A gate must include:

```yaml
kind: gate
status: open | satisfied | waived
owner: human:<name> | reviewer:<name> | agent:<name> | external:<service>
blocks:
  - <TASK-ID>
action: <specific action required>
verification: <what proves completion>
```

Use gates for:

- human signoff;
- manual smoke that requires judgement/device/env;
- credentials/access;
- product/security/release approval;
- unavailable external service;
- CI that cannot be completed inside the current run.

## Rework reset

When review sends work to `rework`:

- keep prior proof that remains valid;
- name the specific failed acceptance item;
- set `readiness: ready` and `next_owner: agent` only when the agent can act;
- clear or supersede stale closeout checkpoints;
- revalidate once after material changes.

## Knowledge delta

When a task changes durable understanding, fill:

| Topic | Before | After | Audience | Target knowledge |
|---|---|---|---|---|

`risk >= high` should require a real knowledge delta when durable understanding changed.
