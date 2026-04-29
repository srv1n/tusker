# 06. Review, Rework, Retry

Status: Draft
Depends on: [01-vault-tracker.md](./01-vault-tracker.md), [03-daemon-and-registry.md](./03-daemon-and-registry.md), [05-runner-and-session-protocol.md](./05-runner-and-session-protocol.md)

## Decision

This spec defines policy only.

- canonical durable note fields and state machine live in Spec 01
- daemon runtime states live in Spec 03 and Spec 05
- this file defines when those states should change

If this spec starts inventing its own state machine, it is wrong.

## Rules

1. a successful attempt never skips straight to `done`; it goes to `in_review`
2. human-requested changes create a new `work_revision`
3. automatic retries are recovery, not product feedback
4. retry budget is per `(record_id, work_revision, runner)`
5. resume is only for the same attempt and same intent
6. continuation turns happen inside one attempt and do not increment retry budget

## Successful Attempt Handoff

When an attempt ends successfully after all continuation turns and review-packet generation:

1. mark the attempt `succeeded` in SQLite
2. release the runtime lease
3. update the note durably:
   - `status=in_review`
   - `review_state=verification_requested`
   - `review_requested_at=<now>`
4. stop dispatching until verification promotes the review round to `requested`

That pause is mandatory.

`verification_requested` is intentionally distinct from `requested`. The first means "the worker claims it is ready"; the second means "a verifier checked the claims and human review can decide." Collapsing those two states makes evidence theater too easy.

The daemon must not move a note to review after an individual turn if workflow policy says the worker should continue. The review handoff happens after the attempt, not after every turn.

## Human Review Verbs

Suggested CLI:

```text
tusker review approve --id <ID> --by <human> [--summary "..."] [--details-file path]
tusker review request-changes --id <ID> --by <human> --summary "..." [--details-file path]
tusker review comment --id <ID> --by <human> --summary "..." [--details-file path]
```

### `review approve`

Durable note effects:

- `review_state=approved`
- `reviewed_by=<human>`
- `reviewed_at=<now>`

If all attestation and signoff gates are satisfied, the next durable step is:

- `status=done`

If not, the item may remain `in_review` or move to `merging` depending on workflow.

### `review request-changes`

Durable note effects:

- `status=rework`
- `review_state=changes_requested`
- `reviewed_by=<human>`
- `reviewed_at=<now>`
- `work_revision += 1`

Runtime effects:

- clear any retry schedule for the old revision
- next orchestration attempt is a fresh attempt for the new `work_revision`

### `review comment`

No durable state transition. Append comment record only.

## Rework Vs Retry

These are not the same thing.

| Case | Durable note change | Runtime change |
|---|---|---|
| human asks for different work | yes | new attempt on new revision |
| runner/network crash | no | retry same revision |
| daemon restart | no | reconcile and resume same attempt if possible |
| normal continuation turn | no | next turn in same attempt |

Without this split, audit trails turn to mush.

## Retry Policy

Only these classes may auto-retry by default:

- `transient`
- `runner_crash`
- `infrastructure`
- `stalled`

Everything else stops for human judgment.

Non-retryable by default:

- `auth`
- `sandbox`
- `approval_needed`
- `budget_exceeded`
- `deterministic`
- `blocked_by_human`
- `tracker_conflict`
- `unknown`

Special handling:

| Failure class | Rule |
|---|---|
| `context_window` | retry only if workflow can produce a tighter prompt or split follow-up |
| `workspace_dirty` | retry only after workspace reset or explicit reuse decision |

Automatic retry effects:

- note `status` stays `active`
- note `work_revision` does not change
- SQLite lease state becomes `retry_queued`
- retry timer is scheduled from workflow backoff

When retry budget is exhausted:

- runtime lease is released
- attempt is terminally `failed`
- note stays in `active` until a human decides next action or explicitly marks `blocked`

Do not silently spin forever. Retry storms are dumb and expensive.

## Resume Semantics

Resume the same attempt only when all are true:

1. same `attempt_id`
2. same `work_revision`
3. adapter can prove session continuity
4. tracker still makes the attempt valid
5. workspace metadata still matches the same `record_id`

Create a new attempt when:

- review changes were requested
- acceptance criteria materially changed
- session cannot be safely resumed
- prior attempt already ended terminally

Fork a new thread under the same ticket when:

- the task intent and branch remain valid
- context pressure or conversation drift makes continued turns risky
- the workpad/review packet can seed the new session without losing auditability

Create a separate branch under the same ticket when:

- the current implementation path should be isolated from an alternative approach
- the work is still inside the same acceptance criteria
- the branch decision records parent attempt, parent session, reason, and merge/discard rule

Do not create a new story just because a separate branch is useful. Create a follow-up story only when the work is out of scope for the current ticket.

## Reconciliation After Daemon Restart

On restart the daemon reconciles SQLite runtime state, not note frontmatter.

Rules:

- live resumable session -> reattach and continue
- missing session with live lease -> mark attempt `abandoned`
- retry timer due -> enqueue fresh attempt for same revision
- note moved to `done` or `cancelled` while runtime existed -> terminate runtime and release lease
- note moved to `in_review` or `blocked` -> do not dispatch

Never launch a new attempt while an old resumable session is still alive.

## Go File Mapping

| File | Responsibility |
|---|---|
| `review_types.go` | review enums and structs |
| `review_store.go` | review record persistence |
| `review_commands.go` | review CLI |
| `retry_policy.go` | backoff and retry eligibility |
| `retry_commands.go` | manual retry commands |
| `reconcile_resume.go` | same-attempt resume vs new-attempt logic |

## Acceptance Criteria

- successful execution always lands in `status=in_review`
- human `request-changes` always increments `work_revision`
- automatic retries never mutate `work_revision`
- this spec does not redefine the durable tracker state machine
