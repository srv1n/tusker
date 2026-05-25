## Contribution workflow snippet

Keep `AGENTS.md` short.

Point contributors and agents to:
- the public issue templates for intake (a change issue maps to the intent, scope, acceptance, and evidence sections of a task)
- the PR template for evidence
- the project repo docs for architecture or policy

For token discipline, keep repo-specific command wrappers, build locks, and
forbidden expensive probes in `tusker/SKILL.md` or routed runbooks. Root
`AGENTS.md` should stay a bootstrap pointer, not a command diary.

Do not turn `AGENTS.md` into a giant encyclopedia.
