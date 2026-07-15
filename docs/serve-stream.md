# Serve stream and summary contract

`GET /api/stream` is a server-sent event stream. Each `data:` message is a
JSON object with the existing `keys` invalidation array plus notification
metadata:

| Field | Type | Meaning |
|---|---|---|
| `id` | integer | Monotonic identifier within one serve process. Clients keep the highest value and drop replayed events after reconnect. |
| `kind` | string | `poll_tick`, `run_started`, `run_failed`, `run_completed`, `lease_transition`, `task_status_changed`, `task_review`, `task_waiting_human`, or `review_batch`. |
| `project` | string | Registered project identifier when the event belongs to a task/run. |
| `task_id` | string | Task identifier for task/run events. Human-gate and review events always provide it. |
| `title` | string | Task title. Human-gate and review events always provide it. |
| `status` | string | Current task status when known. |
| `urgency` | string | `info` for ordinary lifecycle updates, `attention` for review/failure work, or `critical` for a human gate. |
| `deep_link_path` | string | Same-origin UI path for the task. Human-gate and review events always provide `/p/<project>/work?task=<task_id>`. |
| `occurred_at` | RFC3339 string | Event creation time in UTC. |
| `keys` | string[] | Existing cache-invalidation keys; unchanged for current serve UI consumers. |

`GET /api/summary` returns the current badge projection:

```json
{"attention":2,"review":1,"running":1,"failed_recent":0,"generated_at":"2026-07-10T12:00:00Z"}
```

`attention` uses the same `/api/needs` derivation, `review` counts review
tasks, `running` counts live dispatching leases, and `failed_recent` counts
terminal failed runs within the configured retry policy. The endpoint keeps a
one-second warm projection, so repeated badge refreshes do not perform a full
vault scan for every request.

`GET /api/projects` also includes a `reconciliation` object per project with
the effective adaptive `tier`, `cadenceMs`, `lastActivityAt`,
`lastActivityReason`, `lastPollAt`, and `nextDueAt`. CLI/UI activity resets only
the affected project to `hot`; live runtime work uses the `live` safety tier.
