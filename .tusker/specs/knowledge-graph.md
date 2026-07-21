---
title: "Knowledge graph: self-scaffolding, self-checking documentation"
subject: knowledge-graph
keywords: [knowledge graph, documentation, docs]
part_of: overview
status: canonical
created: 2026-07-21
read_when: "Implementing or reviewing Stream E (KNW epic) work; wiring docs behavior into init, validate, or the spec skill; asking how docs stay findable and true."
skip_when: "Reading or writing an individual doc — follow the front-matter contract in docs/system/00-overview.md instead."
decisions_locked: true
updates:
  - docs/system/00-overview.md
  - docs/system/cli.md
sources:
  - "Operator discussion 2026-07-21 (post-factory-grill) — full Q&A in [[2026-07-21-knowledge-graph-grill]]"
---

# Knowledge graph: self-scaffolding, self-checking documentation

## Why we are building this

The documentation system in this repo (canonical docs, specs, decision logs,
tickets linking back) exists only because the operator demanded it mid-flight.
The rules for maintaining it live in one repo's skill files and one session's
memory. Set Tusker up in another repository and none of it comes along. Worse,
the two chronic failure modes of all documentation — an agent can't find the
right doc quickly, and stale or duplicate copies accumulate until someone
reads the wrong one — are not prevented by anything mechanical.

This spec makes the documentation system part of Tusker itself: scaffolded by
`tusker init`, navigated by the CLI, checked by `tusker validate`, and kept
true by the task lifecycle. Truth still comes from humans and agents reading
the docs against the code; the machine's job is to make the skeleton — one
doc per subject, findable, current, connected — impossible to break silently.

## The shape (plain language)

Three kinds of knowledge nodes, three kinds of declared edges:

| Node | Lives in | Answers | Key edges (front-matter) |
|---|---|---|---|
| Canonical system doc | `docs/system/` | how it works today | `subject` (unique key), `part_of` (parent doc), `describes` (code paths) |
| Spec | `.tusker/specs/` | what is changing and why | `subject`, `updates` (docs it will obsolete), `sources` |
| Decision log | `.tusker/specs/decisions/` | what was said, what got locked | `decides_for` (its spec) |

Tickets point up with `spec_refs`. Humans and agents write only front-matter
and prose; every map, index, and diagram is **generated output** — never
hand-maintained, because a hand-drawn map is just another document that rots.

## Locked decisions

1. **Retrieval is deterministic, not semantic.** No embedding database. The
   corpus is small by design (one doc per subject); the problem is routing to
   exactly one right answer, which keyword matching over front-matter does
   deterministically with zero infrastructure. `tusker docs find <query>`
   matches `subject`, `keywords` (aliases), and headings via ripgrep-style
   search, ranked canonical doc → spec → decision log (decision logs ARE
   indexed, ranked last). If the corpus ever outgrows keywords, an embedding
   layer can sit behind the same command; the interface does not change.
2. **Duplication is prevented at write time.** `subject` is a unique key;
   validate fails on duplicate subjects, version-suffix filenames
   (`-v2`, `_new`, `-final`, `(1)`), or missing front-matter.
   `tusker docs new <subject>` refuses existing subjects and points at the
   file to update in place. Replacement is explicit: the old file becomes a
   tombstone (`status: superseded`, `superseded_by: <subject>`, one-line
   body) so stale keywords resolve forward, never to stale content.
3. **The map is generated, and generation is the enforcement.**
   `tusker docs map` walks front-matter edges and emits: the mermaid DAG into
   a fenced generated region of `docs/system/00-overview.md`, a generated
   `docs/system/INDEX.md` (subject → file → description → freshness), and a
   JSON graph for the serve UI. The generator refuses malformed graphs —
   orphan doc (no `part_of`), duplicate subject, dangling edge, cycle — with
   the specific defect named. `tusker validate` regenerates in memory and
   diffs against the committed artifacts; a stale map is a red validate
   (the gofmt pattern: generated output is checked in, source of truth is
   front-matter, the machine proves they agree).
4. **The doc-touch rule catches drift when code changes.** Each canonical doc
   declares coarse `describes:` paths. At task close, the validator
   intersects the task diff with all `describes:` paths; each implicated doc
   needs either a doc edit in the same diff or a one-line waiver row
   (`doc_unchanged | <doc> | waived | <reason>`). The reviewer checklist
   verifies edited docs tell the truth and waivers are honest.
   **Rollout: warning first** — validate flags drift for a probation period
   while `describes:` paths get tuned — **then flips to a close blocker.**
   Uncovered code paths trigger nothing; `docs status` lists them as gaps.
5. **Freshness is stamped, not assumed.** Docs carry
   `last_verified: <date> @ <commit>`. Any agent that reads a doc against the
   code and finds it accurate bumps the stamp. `tusker docs status` sorts
   docs by staleness (commits touching `describes:` paths since
   `last_verified`) — the one-screen answer to "which docs should I
   distrust?" and the input to a wave-end/nightly verifier chore.
6. **A locked spec must land its doc updates.** A spec with
   `decisions_locked: true` whose `updates:` targets were never edited (and
   no doc-update task exists in its epic) fails validate.
7. **`tusker init` scaffolds all of it** in any repo: `docs/system/` with an
   overview stub carrying the front-matter contract, `.tusker/specs/` +
   `decisions/`, and the spec skill — so the system exists on day zero
   without a human demanding it.
8. **Readable shape is machine-checked.** Front-matter complete →
   plain-language opening (the existing plain-language lint extends to doc
   openings) → diagrams/tables where shape matters → sparse backlinks.
   Depth stays human judgment; presence of the shape is linted.
9. **One corpus for humans and agents.** Front-matter serves the machine, the
   body serves the reader; there is never a separate "docs for agents" fork.

10. **Brownfield adoption is incremental with a hard dedup floor.**
    `tusker docs adopt` handles repos with existing code and scattered docs:
    inventory every markdown file outside the managed tree; an agent proposes
    a per-doc disposition — promote (feeds a canonical doc), merge (folded
    into an existing subject), tombstone (stale duplicate, signpost forward),
    or leave (READMEs, vendored docs, licenses) — and the operator approves
    the triage table, not each file. The canon seeds top-down (overview plus
    one doc per major subsystem, coarse `describes:` paths); coverage grows
    organically as the doc-touch warning surfaces undescribed areas tasks
    actually touch. No big-bang migration is ever required — but from the
    first adopt run, every subject has exactly one findable answer, because
    unpromoted legacy docs become tombstones and `docs find` always routes
    forward.

## The agent habit (bound into skills)

Before reading or writing any documentation: `tusker docs find <query>` —
never create a doc except through `tusker docs new`. The tusker and spec
skills state this as a hard rule; validate's write-time checks are the
backstop when the habit fails.

## Deferred (explicitly not now)

- Embedding-backed retrieval behind `docs find`.
- Serve-UI interactive graph rendering beyond consuming the JSON artifact.
- Auto-generating `describes:` suggestions from git history.
