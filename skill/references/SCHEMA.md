# Schema

## Task waiting for human

```yaml
schema: tusker.task/v7
kind: task
status: review
readiness: waiting_on_human
next_owner: human:sarav
agent_action: stop_until_human_response
next_action: Complete the named human gate or proof item.
```

Do not use `active` as a V7 task status. Runtime activity is represented by run leases, sessions, and attempts.

## Dispatchable task

```yaml
status: ready
readiness: ready
next_owner: agent:codex_app_server
proof_mode: inline
proof_status: pending
```

## Gate

```yaml
kind: gate
gate_kind: auth
status: open
owner: human:sarav
blocking: true
blocks:
  - APP-T-0001
why_agent_cannot: Human account access is required.
action: Complete OAuth login and approve the callback.
verification: Human confirms the app reaches the dashboard.
```
