---
schema: tusker.doc/v5
id: spec/v5-overview
title: Tusker v5 implementation spec
type: doc
node: spec/v5-overview
audience: developer
kind: canon
domains: [schema, workflow, docs, migration]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: spec/v5-overview
publish_description: Transitional implementation spec for Tusker v5.
created: 2026-04-29
updated: 2026-04-29
---

# Tusker v5 implementation spec


Tusker v5 is the **transitional** redesign of Tusker for agent-first software teams.
It is transitional because it does not pretend the current repository is greenfield. It
keeps the pieces that already work, especially repo-local Markdown, Obsidian readability,
and the existing docs export/publication pipeline, while replacing the parts that are
clearly wrong for the new operating model.

## Thesis

The unit of work is no longer a story or a PR. It is an **executable task contract**.

The task contract answers four separate questions:

1. **What must be true?** → acceptance contract
2. **How will we check it?** → verification plan
3. **What proof was produced?** → evidence
4. **What durable understanding changed?** → knowledge delta

## Final model

```text
Epic = workstream boundary + canon + success metrics
Task = executable change contract
Bug  = task(kind: bug)
Doc  = durable knowledge page
```

## Final decisions

### 1. Rename `story` to `task`
Do it everywhere: IDs, templates, CLI, README, views, skill, and docs.

### 2. `bug` is not a top-level execution type
Use `type: task, kind: bug`. Keep `bug.md` only as an ergonomic creation template.

### 3. Keep `status` canonical in task frontmatter
Tusker is deliberately Markdown-first and daemon-optional. If current task state is
event-derived, the Markdown file becomes a lying cache. That is the wrong trade.

### 4. Do not introduce per-task sidecars
Use central `_system/events`, `_system/runs`, and `_system/generated`. Do not mirror
task state in `.json` files next to every task.

### 5. Keep `type: doc`, but move docs into `tusker/docs/`
This is the key transitional compromise. Durable docs are still first-class pages with
frontmatter because the current publication pipeline already depends on metadata like
`publish_path`, `canonical_status`, and llms/canon outputs. But docs are no longer
work-lifecycle artifacts living inside epic folders.

### 6. Drop human-authored docs enums
Do not ask humans to keep `docs: check|update|new|deprecate` in sync with `doc_nodes`.
If `doc_nodes` is non-empty, the docs-impact hook runs. If it should not change a page,
that becomes an explicit waiver or no-op verification.

### 7. Use `domains + doc_nodes`
`domains` are broad, human-facing groups.
`doc_nodes` are exact automation targets from `_config/docs-map.yaml`.

### 8. Separate acceptance, deliverables, verification, and evidence
These are different concepts and must stay different.

### 9. Add `knowledge delta`
This is not a vanity field. It is the payload the docs hook actually needs.

### 10. Risk tiers decide ceremony
A typo should not cosplay as an architecture review.

## Repository layout

```text
tusker/
├── epics/
│   └── NXT/
│       ├── NXT.md
│       └── NXT-T-0001.md
├── docs/
│   ├── spec/
│   └── reference/
├── _config/
│   ├── docs-map.yaml
│   └── WORKFLOW.md
└── _system/
    ├── generated/
    ├── runs/
    └── events/
```

## Task frontmatter

```yaml
---
schema: tusker.task/v5
id: NXT-T-0002
title: Introduce the v5 note model
type: task
kind: migration
epic: NXT
status: ready
priority: p0
risk: high
size: l
domains: [schema, migration]
doc_nodes: [spec/v5-overview, spec/migration-v5]
blocked_by: [NXT-T-0001]
created: 2026-04-29
updated: 2026-04-29
---
```

## Doc frontmatter

```yaml
---
schema: tusker.doc/v5
id: spec/v5-overview
title: Tusker v5 implementation spec
type: doc
node: spec/v5-overview
audience: developer
kind: canon
domains: [schema, workflow, docs]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: spec/v5-overview
publish_description: Transitional implementation spec for Tusker v5.
created: 2026-04-29
updated: 2026-04-29
---
```

## Task body contract

Every medium+ task should expose this split:

```text
Intent
Scope
Acceptance contract
Canon
Code/system anchors
Constraints
Escalate if
Deliverables
Verification plan
Knowledge delta
---
Execution plan
Evidence
Verification log
Work log
```

## Knowledge delta shape

Use a structured table:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|

This is much better than a vague “change” sentence because it forces the mental-model delta.

## Validator rollout

Phase 1 hard failures only:

1. `UNKNOWN_DOC_NODE`
2. `DOCS_IMPACT_UNRESOLVED`
3. `MISSING_KNOWLEDGE_DELTA` at `risk >= high`

Everything else can begin as a warning until the team sees the failure mode often enough
to justify hard enforcement.

## Primary CLI surface

```text
tusker new <kind>
tusker status <id> <new>
tusker evidence <id> <kind> <path>
tusker docs check <id>
tusker docs apply <id>
tusker docs waive <id> <node>
tusker verify <id>
tusker close <id>
tusker validate
tusker reindex
```

Compatibility shims are fine. They are not the public face of v5.

## Migration principle

Migration is not a footnote. Tusker already has a working publication pipeline keyed off
legacy D-note metadata. The migration must explicitly carry that state forward instead of
pretending it does not exist.
