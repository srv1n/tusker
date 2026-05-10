# Workflow

Short reference for the V5 task lifecycle.

## Lifecycle

```text
draft -> backlog -> ready -> active -> review -> done
                      |         |
                      v         v
                   blocked    rework -> active

active|review|blocked|rework -> cancelled
```

Status contract:

| Status | Meaning |
|---|---|
| `draft` | Not fully shaped, not pickable. |
| `backlog` | Shaped future work, not current release, not pickable. |
| `ready` | Shaped current work, pickable only if unblocked. |
| `blocked` | Current work waiting on Tusker dependencies or an external blocker. |
| `active` | Claimed/in progress. |
| `review` | Worker claims implementation is ready for verification. |
| `rework` | Review found changes needed before another verification pass. |
| `done` | Accepted and closed. |
| `cancelled` | Intentionally abandoned. |

Every transition writes a row to `transitions[]`:

```yaml
transitions:
  - from: "backlog"
    to: "ready"
    at: "2026-04-29T14:02:11Z"
    by: "sarav"
    reason: null
```

Never hand-edit `transitions[]`. Use:

```bash
tusker status <ID> <state> [--reason "..."]
```

## Status Stamps

- `started`: first entry to `active`
- `review_requested_at`: entry to `review`
- `completed`: entry to `done`
- `cancelled_at`: entry to `cancelled`
- `blocked_since`: entry to `blocked`

## Gates

- `validate` is the full integrity pass.
- `review` means the worker claims implementation is ready for verification.
- `verify` records that the verifier checked the claims against the current tree.
- `close` records acceptance and moves the task to `done`.
- `doc_nodes` requires docs check/apply/waive before close.
- `risk >= high` requires a real `## Knowledge delta`.
- Epic `done` refuses unfinished child tasks.
- If `WORKFLOW.md` enables the reviewer lane, an independent agent reviewer may auto-close low/medium tasks after verification, but high/critical tasks remain human-gated.

## Dependencies

- `blocked_by`: this task depends on these tasks.
- `blocks`: downstream tasks depend on this task.
- `block_reason`: human-readable reason for an external or dependency blocker.

State rules:

- Unshaped work stays in `draft`.
- Shaped future work goes to `backlog`.
- Current-release work moves to `ready` only when it is shaped.
- Ready work with unresolved Tusker dependencies or an external blocker moves to `blocked`.
- Active work that cannot continue moves to `blocked`.
- Blocked tasks should show `blocked_by` and/or `block_reason`.
- Wire dependency links when tasks are created.

## Review And Rework

End-of-run proof belongs under `## Evidence`: changed files, diff summary, commands/results, artifacts, risks, and follow-ups.

Agent reviewer pass:

- The lane is runner-neutral. Codex is the default live runner today, but `reviewer.runner` can point at any enabled runner adapter.
- Review only; do not edit implementation files.
- Check acceptance, scope, evidence, verification, docs resolution, and caveats.
- For low/medium tasks, the configured reviewer may run `verify` and `close` when every gate passes.
- For high/critical tasks, leave advisory evidence and keep the task in `review` for human verification.
- If review fails, move to `rework` with the specific unmet acceptance item.

PR feedback sweep:

1. Classify unresolved feedback as `must-fix`, `question`, `nit`, or `follow-up`.
2. Fix in-scope `must-fix` items before requesting review again.
3. Draft out-of-scope work as follow-up tasks.

Rework reset:

- Keep prior evidence.
- Update the plan around the new scope.
- Move back to `active` when work resumes.

## Knowledge Delta

When a task changes durable understanding, fill:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|
