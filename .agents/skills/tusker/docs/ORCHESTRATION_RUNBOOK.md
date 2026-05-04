# Orchestration Runbook

The public V5 CLI tracks task truth. Runtime orchestration is internal implementation detail; do not teach users daemon/project/run commands as the normal workflow.

## Durable State

- Task status lives in markdown: `draft`, `ready`, `active`, `blocked`, `review`, `rework`, `done`, `cancelled`.
- Runtime attempts live in generated/runtime stores and evidence artifacts.
- Do not put live process state into task frontmatter.

## Human Loop

```bash
tusker status <TASK-ID> active
tusker evidence <TASK-ID> packet <file-or-url>
tusker status <TASK-ID> review
tusker verify <TASK-ID> --by <name>
tusker close <TASK-ID> --by <reviewer>
```

Raw logs are useful evidence only when they are summarized into a readable packet.
