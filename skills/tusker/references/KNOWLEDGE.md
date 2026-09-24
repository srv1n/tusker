# Knowledge

One placement rule covers all product knowledge: it lives under `docs/system/`
and stays readable with ordinary Markdown links after Tusker is removed.
Current chapters live in `docs/system/` (per domain under
`docs/system/domains/<domain>/`), change specifications in
`docs/system/proposals/`, and product decisions in `docs/system/decisions/`.
`.tusker/` holds tracker state and thin pointers only: tasks, waves, work
decisions, evidence, events, runtime state, generated outputs, and forwarding
stubs for moved paths. It holds no duplicate product prose. Do not create an
agent-only memory corpus.

```
docs/system/00-overview.md            entry point, links to domains/proposals/decisions
docs/system/domains/<domain>/00-index.md   domain purpose, reading order, chapter links
docs/system/proposals/<subject>.md    one change specification and its lifecycle
docs/system/decisions/<subject>.md    one durable product decision and rationale
.tusker/                              tracker state and thin routing pointers
```

A document's `subject` is its stable identity across moves; its path is the
file link. Prefer normal relative Markdown links. Front-matter edges
(`updates`, `decides_for`, `sources`, aliases, supersession) enrich the same
documents but are never required to read them. A domain `00-index.md` names
the domain reading order. Lifecycle is kind-specific (docs:
`current|superseded`; proposals: `proposed|accepted|implemented|superseded`;
decisions: `proposed|accepted|superseded`) and says where a document stands;
`code_conformance` (`unverified|matches|drift|not_applicable`) says whether
the code was checked against it. Acceptance records intent, not
implementation proof. Product decisions live in `docs/system/decisions/`;
work and lifecycle decisions stay with the tracker.

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
