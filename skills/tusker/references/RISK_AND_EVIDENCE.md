# Risk And Evidence

## Proof Modes

| Mode | Use |
|---|---|
| `none` | Only for tasks that explicitly waive proof. Rare. |
| `inline` | Default for normal code tasks. Verification rows live in the task. |
| `card` | Medium/high tasks needing a concise evidence object. |
| `artifact` | UI screenshots, traces, videos, benchmark files, or other durable artifacts. |
| `audit` | Critical/security/release tasks. Requires stronger objective proof and independent review. |

## Evidence Discipline

- Proof must cover acceptance ids.
- Store raw logs in runtime/scratch, not task markdown.
- Promote only bounded summaries or curated artifact references.
- Keep evidence, verification rows, and attempt summaries terse: command plus PASS/FAIL and the first actionable failure. Task contracts and specs stay in full prose.
- Do not require changelog/docs/canon updates unless the task contract names `doc_nodes`; low/medium tasks with no `doc_nodes` should keep `Knowledge delta: None expected.`
- Route repeated lessons through `tusker feedback promote` instead of appending long per-task knowledge deltas.
- High and critical risks require stronger proof, reviewer depth, and landing
  safeguards. Independent reviewers may objectively accept them only through a
  typed result; deterministic Tusker handlers own integration and closure.
  Risk alone never supplies human authority.
- Explainer/understanding packets help humans build a mental model, but they do not satisfy proof by themselves.
