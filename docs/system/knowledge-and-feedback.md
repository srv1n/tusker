---
title: "Knowledge and feedback"
subject: knowledge-and-feedback
part_of: overview
status: canonical
read_when: "Finding current documentation or recording product friction."
skip_when: "Reading task execution logs or checking a worker lease."
---

# Knowledge and feedback

Tusker has two current knowledge sets. They have different readers.

## Project canon

`.tusker/knowledge/domains/project/CANON.md` stores durable facts about this
repository. Its `INDEX.md` routes a task agent to the narrowest current file.
Do not put task progress, proof logs, attempts, or generated output in canon.

## System documents

`docs/system/` explains current product behavior. Use
`tusker docs find <query>` before you add a page. Update the existing subject
when it already owns the answer.

The managed document corpus has one route. Behavior pages live in
`docs/system/`; governing specs live in `.tusker/specs/`; and durable
decisions live in `.tusker/specs/decisions/`. A subject is the document's
stable identity, while its path is the stable file link. Preserve source
material outside these roots until a reviewed migration gives it one current
owner; an intake copy is not a governing spec.

Search includes `read_when` metadata. Results expose both `read_when` and
`skip_when` so readers can choose the right page without opening every body.
Skip guidance does not count as a positive search match.

Search resolves superseded subjects forward and reports the historical subject
that led there. It does not list the entire corpus. `tusker docs map` and the
document detail route use the same subject/path resolver, so semantic links and
backlinks point at the files that actually exist. A spec's `sources` may point
to a managed document and appears as a `source` edge; external source records
remain provenance without becoming governing documents.

Agents can use `tusker docs browse` for one bounded folder level, `tusker docs
read` for one document or exact Markdown section, and `tusker docs backlinks`
for incoming relationships. `tusker docs check` reports metadata and managed
link defects with repair guidance. These reads do not write the corpus; use
`tusker docs new --print` to inspect a validated scaffold before creating a
file.

`tusker docs map` builds `docs/system/INDEX.md`, `docs/system/graph.json`, and
the graph block in the overview. Do not edit those outputs by hand.

Use `aliases` in front matter when a preserved document has older wiki-link
names. Tusker resolves a unique alias to its canonical subject. It leaves a
duplicate alias unresolved.

A document is verified only when `last_verified` contains a date and the Git
commit that was checked. A date alone is not a verification stamp.

## Feedback

Feedback is an observation. It is not a task. `tusker feedback add` records the
observation. A person can review and promote it into tracked work.

## Language

- Use one idea in one sentence.
- Use active voice.
- Name the actor.
- Use one term for one idea.
- Keep exact paths, commands, states, and numbers.
- Lead with the reader's outcome. State the expected result and failure path.
- Preserve negation, permission boundaries, uncertainty, and decision links.
- Use an available external writing method when requested or installed; otherwise
  these local requirements still apply.

## Code sources

- `cmd/tusker/v7_domain_cmd.go`
- `cmd/tusker/v7_feedback_cmd.go`
- `cmd/tusker/docs_cmd.go`
- `internal/docgraph/`
- `.tusker/SKILL.md`
