---
title: "Task Decomposition"
description: "Use this when a PRD/RFC/spec is too large for one agent pass and needs a task stack."
tusker:
  audience: "user"
  publish_path: "user/reference/task-decomposition"
  publish_section_title: "Reference"
  route: "/user/reference/task-decomposition/"
  source_kind: "repo_doc"
  source_path: "skill/references/TASK_DECOMPOSITION.md"
  summary: "Use this when a PRD/RFC/spec is too large for one agent pass and needs a task stack."
  tags:
    - "reference"
  updated: "2026-05-11"
---

# Task Decomposition

Use this when a PRD/RFC/spec is too large for one agent pass and needs a task stack.

## Goal

Turn broad intent into:

1. one epic with explicit canon,
2. durable docs where needed,
3. multiple tasks that are individually executable,
4. dependency links that make order obvious.

If the task title is "implement the RFC", the split failed.

## What makes a good task

A good task has:

- one clear acceptance contract,
- bounded deliverables,
- concrete verification,
- explicit `domains`,
- exact `doc_nodes` when docs are affected,
- a risk level that matches blast radius,
- a knowledge delta when future readers need to understand something differently.

## Decomposition flow

1. Pick or create the epic.
2. Pick canon: epic `## Design`, V5 doc under `tusker/docs/**`, or a repo file cited by doc `source_of_truth`.
3. Pull out decisions that tasks must not reopen.
4. Split by contract boundary, not by folder.
5. Create tasks with `kind`, `risk`, `size`, `domains`, and `doc_nodes`.
6. Wire `blocked_by` / `blocks`.
7. Leave tasks with unmet prerequisites in `draft` or `ready`.
8. Run `tusker validate`.

## Split by real boundaries

Prefer tracer-bullet vertical slices: one narrow behavior that cuts through all
layers needed to make it real, with its own verification. A completed slice
should be demoable or checkable without waiting for a later "wire it together"
task.

Good boundaries:

- data model or schema first,
- API/protocol contract before clients,
- migration before behavior that depends on migrated state,
- docs update as part of the task that changes the knowledge,
- cleanup task when two code paths would otherwise survive.

Weak boundaries:

- "backend" vs "frontend" when the real boundary is contract vs projection,
- "write all tests" followed by "write all implementation",
- one task for storage migration plus UX polish plus runtime cleanup,
- a task whose only acceptance criterion is "RFC implemented",
- `xl` as an excuse to keep a bloated task.

## Dependencies

Use task links:

```yaml
blocked_by:
  - "[[HIT-T-0001]]"
blocks:
  - "[[HIT-T-0003]]"
```

Wire both sides when practical. `blocked_by` tells the scheduler the truth; `blocks` makes the graph readable.

## Docs impact

Every task that changes durable understanding should carry:

```yaml
domains: [runtime, docs]
doc_nodes: [reference/runtime]
```

And body:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|

Do not hide docs work in a final "update docs" task unless the implementation tasks genuinely cannot know the docs delta yet. Most of the time, docs belong with the change that makes them true.

## Example stack

| Task | Why it exists | Size | Risk | Depends on |
|---|---|---:|---|---|
| `HIT-T-0001` — Freeze shared request types | locks nouns and transport shape | m | high | |
| `HIT-T-0002` — Persist pending requests | creates the source of truth | m | high | `HIT-T-0001` |
| `HIT-T-0003` — Project requests into UI state | client reads the new truth | m | medium | `HIT-T-0002` |
| `HIT-T-0004` — Add docs close gate coverage | makes the behavior teachable | s | medium | `HIT-T-0002` |
| `HIT-T-0005` — Remove old path | prevents two models from surviving | s | high | `HIT-T-0003` |
