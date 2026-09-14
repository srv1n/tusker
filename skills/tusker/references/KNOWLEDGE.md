# Knowledge

One visible corpus serves humans and agents: `docs/system/` describes current
behavior, `.tusker/specs/` proposed change, and `.tusker/specs/decisions/`
recorded rationale. Do not create an agent-only memory corpus.

For a known document or packet reference, read its governing section directly.
Otherwise use `tusker docs find <query>` and the matching `read_when` section.
Expand to `docs browse` or `docs backlinks` only if that leaves a specific gap;
check targeted help when syntax is uncertain. A documented answer comes from a
current doc read now, or is **not in canon**.

## Truth labels

| Label | Authority |
|---|---|
| current docs | documented current contract |
| inspected source | implementation read now, not executed proof |
| observed behavior | exact command, environment, and build observed now |
| historical rationale | past decision, not current behavior |
| inference | conclusion from cited inputs, uncertainty retained |

## Resume and resolve gaps

Use the capsule for task status and the full packet for implementation. Preserve
acceptance, conditions, non-goals, and ownership. Follow its exact references;
a domain index helps only when the owning document is not already identified.

Inspect source to establish implementation facts; read decisions or bounded
history when rationale is needed. Cite the authority supporting the claim and
retain unknowns. A source read can answer a code question without establishing
runtime behavior or the author's intent.

Old discussion conflicting with current canon is history. Code conflicting
with docs is **code/doc drift**: source is the best inspected implementation
fact but still not runtime proof. Fix an owned stale doc; otherwise return it
to its owner. With no recorded rationale, say `rationale not in canon`; never
present reconstructed intent as a decision.

Distinguish **changed docs** (contract moved), **broken harness** (the check
could not observe the product), and **product regression** (a valid check
observed the wrong result). See `RUN.md` when recording executed proof.

For authoring, use `SPECS.md`. Keep current behavior and rationale in their
owning documents; scratch and execution transcripts are not another canon.
