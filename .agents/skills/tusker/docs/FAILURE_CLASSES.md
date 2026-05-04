# Failure Classes

Runtime failures belong in runtime logs and evidence, not task frontmatter.

| Class | Meaning | Task Action |
|---|---|---|
| `transient` | network, rate limit, temporary tool outage | retry when infrastructure recovers |
| `deterministic` | repeatable test/type/assertion failure | keep task active or move to blocked with evidence |
| `blocked-by-human` | missing product/security/credential decision | `tusker status <TASK-ID> blocked --reason "<needed decision>"` |
| `budget-exceeded` | token/time/cost cap hit | split work or reduce scope before resuming |

When a failure changes what future readers need to know, add evidence and update the task's knowledge delta.
