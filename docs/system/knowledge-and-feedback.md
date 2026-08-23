---
title: "Knowledge and feedback"
subject: knowledge-and-feedback
part_of: overview
status: canonical
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

`tusker docs map` builds `docs/system/INDEX.md`, `docs/system/graph.json`, and
the graph block in the overview. Do not edit those outputs by hand.

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

## Code sources

- `cmd/tusker/v7_domain_cmd.go`
- `cmd/tusker/v7_feedback_cmd.go`
- `cmd/tusker/docs_cmd.go`
- `internal/docgraph/`
- `.tusker/SKILL.md`
