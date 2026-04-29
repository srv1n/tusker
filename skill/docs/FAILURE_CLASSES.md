# Failure classes and recovery table

Every `tusker release --to failed|stalled|cancelled` takes a `--failure-class` that drives retry and escalation behavior. The enum is frozen in [schema.go](/Users/sarav/Downloads/tusker/schema.go:1).

| class                | meaning                                                                        | dispatcher action                                                                              | human action                                 |
| -------------------- | ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `transient`          | network blip, rate limit, API timeout, CI flake                                | re-enqueue with `config.retry.backoff_seconds[run_attempts]`; cap at `config.retry.max_attempts` | none until retry budget exhausted            |
| `deterministic`      | assertion failure, type error, test failure, impossible state                  | do NOT retry; leave `dispatch_state: failed`                                                   | triage; fix story or close as invalid        |
| `stuck`              | heartbeat expired; agent process alive but not progressing                     | kill tree; set `dispatch_state: stalled`; do NOT retry                                         | inspect `Attachments/<ID>/session-*.log`     |
| `blocked-by-human`   | agent explicitly requested human input and returned                            | do NOT retry; leave `dispatch_state: failed`; surface in inbox                                 | respond to the story's HumanRequest and re-pickup |
| `budget-exceeded`    | `config.budget.*` ceiling hit                                                  | refuse claim in `pickup`; no spawn                                                             | raise ceiling, or defer work                  |

## Retry budget

`run_attempts` is frontmatter on the story/bug; the dispatcher increments it on every release-to-failed/stalled. Once `run_attempts >= retry.max_attempts`, no further pickups fire for that record — it must be manually re-claimed (see `OPERATOR_INTERVENTION.md`).

Retries only apply to `transient`. Every other class requires human attention before another attempt.

## Escalation

On `failed` or `stalled`, the dispatcher fires `hooks.on_fail` with env vars:

- `TUSKER_ID` — story/bug id
- `TUSKER_EVENT=on_fail`
- `TUSKER_DISPATCH_STATE` — `failed` | `stalled`
- `TUSKER_ACTOR` — `dispatcher`

Typical on_fail hooks: post to Slack, open a GitHub issue, tail session log to ops channel.
