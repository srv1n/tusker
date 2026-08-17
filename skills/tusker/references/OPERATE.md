# Operate

Use this guide only to diagnose task-tracker state. It does not authorize
automation, background execution, repository operations, releases, provider
calls, or spending.

## Read-only diagnosis

```bash
tusker capabilities --json
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID>
tusker closeout status <TASK-ID> --json
```

For execution strands, use `tusker execution show|inbox|list`.
Provider observations remain authority-neutral; recovery procedures live in
`docs/runbooks/execution-observability.md`.

Prefer the smallest command that identifies the task, lifecycle state, open
gate, or missing proof. Do not inspect raw histories, attempts, attachments,
generated indexes, or logs unless the request is specifically about those
records.

## Recovery boundary

Use ordinary CLI lifecycle commands only when the requested correction is
clear. Do not hand-edit protected fields or run setup repair, daemon, dispatch,
service, or fleet operations as a side effect of task management.

If a task-management command is absent, incompatible, or broken, report the
command, concise failure, and supported task state. Keep tracker failure
separate from implementation or test results. Escalate Tusker repair into its
own task only when the user asks for it.
