# Workflow

## Durable Task Statuses

```text
idea -> backlog -> ready -> review -> done
                    |       ^
                    v       |
                  rework ---+
```

Terminal statuses are `done`, `cancelled`, and `superseded`.

Do not use `active` as a durable task state. `claimed`, `running`, `leased`, and `interrupted` are runtime states on runs, leases, sessions, attempts, and workspaces.

Human-only review becomes `readiness: waiting_on_human` with `next_owner: human:<name>` and `agent_action: stop_until_human_response`.

## Dispatch Predicate

A task can dispatch only when all are true:

```text
kind = task
status in [ready, rework]
readiness = ready
next_owner = agent or agent:<enabled-runner>
agent_action != stop_until_human_response
no open blocking human gate
no unsatisfied blocking dependency
acceptance is concrete
verification maps to acceptance
proof_mode and proof_required are valid
runner is enabled
no active run already owns the task
concurrency/workspace limits allow dispatch
```

Use `tusker automation plan <task> --json` instead of guessing.
