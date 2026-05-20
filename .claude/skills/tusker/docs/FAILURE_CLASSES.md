# Failure Classes

Runtime failures belong in runtime logs, attempt summaries, gates, or evidence. Do not hide them in protected task frontmatter.

| Class | Meaning | Task action |
|---|---|---|
| `transient` | network, rate limit, temporary tool outage | retry only if budget allows; otherwise gate/summarize |
| `deterministic` | repeatable test/type/assertion failure | keep/reopen as `rework` with exact acceptance gap |
| `blocked-by-human` | missing product/security/credential/manual decision | create/update human gate and stop |
| `blocked-by-external` | CI, service, device, or environment unavailable | create/update external gate and stop |
| `budget-exceeded` | token/time/cost cap hit | stop, summarize, split work or ask for human decision |
| `loop-detected` | same command/result/fingerprint repeated | stop; do not run the same check again |

When a failure changes future understanding, update evidence or knowledge delta concisely.
