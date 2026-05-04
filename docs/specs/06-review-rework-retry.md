# 06 - Verification, Rework, And Close

V5 uses a simple lifecycle:

```mermaid
flowchart LR
  Draft[draft] --> Ready[ready]
  Ready --> Active[active]
  Active --> Review[review]
  Review --> Done[done]
  Review --> Rework[rework]
  Rework --> Active
  Active --> Blocked[blocked]
  Blocked --> Active
  Active --> Cancelled[cancelled]
  Review --> Cancelled
```

## Review

`review` means implementation is ready to verify. A worker or runtime path may move a task to `review` only after meaningful evidence exists.

## Verify

`tusker verify <TASK-ID> --by <name>` records:

- `verified_by`
- `verified_at`
- a `Verification log` row

Verification does not close the task.

## Rework

If verification fails, move the task back through:

```bash
tusker status <TASK-ID> rework --reason "what failed"
```

The task returns to `active` when the next pass begins.

## Close

`tusker close <TASK-ID> --by <name>` is the only normal path to `done`.

Close requires:

- task status is `review`
- verification exists
- every `doc_node` is applied, verified no-op, or waived

## Retry

Retry is runtime behavior, not a public lifecycle command. Durable truth remains the task status and work log.
