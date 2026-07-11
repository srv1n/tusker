---
capsule:
  what: "V5 workflow contract spec for task states, agents, gates, and close."
  use_when:
    - "Understanding legacy workflow semantics or migration history."
  skip_when:
    - "Changing current V7 dispatch/readiness rules."
---

# 02 - Workflow Contract

`WORKFLOW.md` is the project-level policy and prompt contract. It is V5 by default and lives at the vault root.

## Frontmatter

```yaml
tracker_schema_version: 5
tracker:
  active_states: ["active", "rework"]
  review_states: ["review"]
  terminal_states: ["done", "cancelled"]
runtime:
  max_active_runs_per_project: 1
```

## Body

The body is load-bearing. Runtime dispatch renders it into the worker prompt with the current task, workspace, repo, attempt, workflow, and vault context.

## Status Ownership

| State | Owner |
|---|---|
| `draft`, `ready`, `blocked`, `cancelled` | human or agent through `tusker status` |
| `active`, `rework` | human, agent, or runtime when eligible |
| `review` | worker/runtime when evidence is ready |
| `done` | `tusker close` after verification and docs impact resolution |

Runtime can observe and update durable lifecycle states, but attempts, sessions, leases, retries, and event streams remain runtime data.

## Validation

`tracker_schema_version` must be `5`. Old workflow frontmatter is not accepted as current policy.
