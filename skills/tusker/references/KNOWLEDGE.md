# Knowledge

One visible corpus serves humans and agents: `docs/system/` describes current
behavior, `.tusker/specs/` proposed change, and `.tusker/specs/decisions/`
recorded rationale. Do not create an agent-only memory corpus.

Start with `tusker docs find <query>`. Read the top matching front matter and
section whose `read_when` applies; use supported `docs read`, `browse`, or
`backlinks` after checking help. One question normally costs one or two file
reads. An answer comes from a current doc read now, or is **not in canon**.

## Truth labels

| Label | Authority |
|---|---|
| current docs | documented current contract |
| inspected source | implementation read now, not executed proof |
| observed behavior | exact command, environment, and build observed now |
| historical rationale | past decision, not current behavior |
| inference | conclusion from cited inputs, uncertainty retained |

## Resume recipe

1. Read `tusker show <TASK-ID> --capsule`, then the runnable packet. Preserve
   full acceptance, conditions, non-goals, and ownership.
2. Follow only its domain route and exact owning sections.
3. Inspect narrow source only for an unresolved implementation fact. Read a
   decision section or bounded history only when rationale is requested and
   still missing.
4. Label every answer with the authority above. Missing intent stays unknown.

Old discussion conflicting with current canon is history. Code conflicting
with docs is **code/doc drift**: source is the best inspected implementation
fact but still not runtime proof. Fix an owned stale doc; otherwise return it
to its owner. With no recorded rationale, say `rationale not in canon`; never
present reconstructed intent as a decision.

Distinguish **changed docs** (contract moved), **broken harness** (the check
could not observe the product), and **product regression** (a valid check
reached it and observed the wrong result). Use existing contacts/messages only
after confirming the command is supported. Never claim an unobserved live wake
or resume.

For authoring, use the documentation route. Keep current behavior, decisions,
rationale, and source references in one durable account; scratch and execution
transcripts are not another knowledge system.
