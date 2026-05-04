# 03 - Runtime And Registry

Runtime orchestration is internal implementation. It executes eligible V5 tasks and records attempts, turns, sessions, events, and artifacts outside canonical markdown.

## Source-Of-Truth Boundary

| Data | Location |
|---|---|
| Task status and close state | Markdown frontmatter |
| Task contract and proof | Markdown body |
| Attempts, turns, sessions, retry decisions | Runtime store |
| Raw runner logs and normalized events | Runtime artifacts |
| Generated queues and dashboard data | `_system/generated/**` |

The runtime must not recreate old public command trees. Users interact through `status`, `evidence`, `docs`, `verify`, and `close`.

## Dispatch

Eligible work is `type: task` with status `active` or `rework`, subject to workflow policy, blockers, risk policy, and concurrency limits.

Tasks in `review`, `done`, `cancelled`, `blocked`, `draft`, or `ready` are not dispatched.

## Completion

When a worker proves the implementation is ready, runtime writes durable evidence and moves the task to `review`. It does not mark the task `done`; close is reserved for verification and docs impact resolution.

## Recovery

On restart, runtime reconciles its store against markdown:

- non-active task state wins
- blocked/cancelled/done tasks stop execution
- active/rework tasks can continue when policy allows
- missing or invalid tasks are released with a clear reason

## Operator Visibility

The public product surface stays task-centric. Runtime observability can be rendered into dashboards, review packets, generated indexes, or docs, but not as extra normal workflow commands.
