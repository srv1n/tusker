# Risk And Evidence

## Proof Modes

| Mode | Use |
|---|---|
| `none` | Only for tasks that explicitly waive proof. Rare. |
| `inline` | Default for normal code tasks. Verification rows live in the task. |
| `card` | Medium/high tasks needing a concise evidence object. |
| `artifact` | UI screenshots, traces, videos, benchmark files, or other durable artifacts. |
| `audit` | Critical/security/release tasks. Requires stronger review and usually human acceptance. |

## Evidence Discipline

- Proof must cover acceptance ids.
- Store raw logs in runtime/scratch, not task markdown.
- Promote only bounded summaries or curated artifact references.
- High and critical risks require human acceptance unless explicitly waived by policy.
