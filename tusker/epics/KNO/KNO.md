---
schema: "tusker.epic/v5"
id: "KNO"
title: "V6 Knowledge Graph"
type: "epic"
status: "done"
owner: "sarav"
summary: "Hard-break Tusker V6: product knowledge graph, task proof layer, progressive-disclosure CLI, and filtered publication."
doc_nodes:
  - "spec/v6-rfc"
created: "2026-05-12"
updated: "2026-05-12"
started: "2026-05-12"
completed: "2026-05-12"
transitions:
  - at: "2026-05-12T13:08:33Z"
    kind: "status"
    from: "ready"
    to: "done"
    actor: "codex"
    reason: "V6 knowledge graph implementation and dogfood completed"
  - at: "2026-05-12T13:55:29Z"
    kind: "status"
    from: "done"
    to: "active"
    actor: "codex"
    reason: "Review found V6 trust-gate gaps; reopening for rework"
  - at: "2026-05-12T14:29:32Z"
    kind: "status"
    from: "active"
    to: "done"
    actor: "codex"
    reason: "V6 rework gates implemented, verified, and dogfooded"
  - at: "2026-05-12T14:45:50Z"
    kind: "status"
    from: "done"
    to: "active"
    actor: "codex"
    reason: "P2 publish findings need rework"
  - at: "2026-05-12T14:49:55Z"
    kind: "status"
    from: "active"
    to: "done"
    actor: "codex"
    reason: "P2 publish rework closed"
tags:
  - "v6"
  - "knowledge-graph"
  - "breaking-change"
---

# KNO - V6 Knowledge Graph

## Thesis

Tusker V6 is a hard break from "docs attached to tasks" into a repo-local product knowledge graph. Tasks remain executable contracts and proof records. Current truth moves into first-class domain knowledge pages. The CLI owns routing, freshness, validation, graph edges, and publication projections.

## Scope

In:
- Rename the source-truth model from `docs` to `domains` and `knowledge`.
- Add V6 schemas for domains, knowledge pages, epics, and tasks.
- Replace authored docs-map semantics with generated knowledge indexes.
- Add deterministic routing, capsules, freshness fingerprints, wikilink resolution, backrefs, and filtered publish lanes.
- Dogfood the V6 vault inside Tusker itself.

Out:
- A custom Markdown renderer.
- MDX as canonical source.
- Embeddings or semantic search for V6.0.
- Publishing task records as reader-facing docs by default.
- Treating domain folders as task-history folders.

## Success metrics

- Fresh init creates a valid V6 vault with `tusker/SKILL.md`, `domains/codebase`, `domains/product`, and policy config.
- Tusker itself has domain `INDEX.md` and `CANON.md` files for the RFC's initial domain set.
- `tusker knowledge route` and `show --capsule` answer common agent routing questions without reading epics first.
- Knowledge nodes cannot rot silently: freshness indexes and close gates connect task proof to current truth.
- Publish commands generate site/LLM/skill projections without teaching the old `tusker docs ...` namespace.

## Canon

- `tusker/docs/spec/tusker_v6_rfc.md`
- `tusker/docs/spec/v5-overview.md`
- `tusker/docs/reference/docs-pipeline.md`

Locked decisions:

| Decision | Canon |
|---|---|
| Source truth path | `tusker/docs/**` becomes `tusker/domains/**`. |
| Vocabulary | `doc_nodes`/`docs_resolution` become `knowledge_nodes`/`knowledge_resolution`. |
| Command split | Source truth uses `tusker knowledge ...`; projection uses `tusker publish ...`. |
| Canonical source | Plain Markdown plus YAML frontmatter; no MDX-first corpus. |
| Graph grammar | Domains and knowledge nodes are first-class; tasks remain proof. |
| Project skill split | `skill/SKILL.md` operates Tusker; `tusker/SKILL.md` explains the repo. |

## Task stack

_No open tasks. Use `tusker list --epic KNO --type task --status done` for closed history._

## Open questions

- Should V6 ship compatibility aliases for `tusker docs ...`, or should aliases exist only in an unadvertised transition build?
- What exact compatibility behavior should migration use for older V5 tasks that only have `doc_nodes` and `docs_resolution`?
- How strict should V6.0 be about existing pages that lack `Read this when` and `Do not read this when` during migration?
