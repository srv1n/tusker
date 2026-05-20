---
schema: tusker.epic/v5
id: SKL
title: Skill and templates
type: epic
status: active
domains:
  - skill
created: 2026-04-29
updated: 2026-04-29
---

# SKL · Skill and templates

## Narrative

Implement the v5 Tusker direction for **Skill and templates**.

## Success definition

This epic is done when all child tasks are `done`, relevant docs are verified through the latest task close event, and the v5 validator passes on a migrated sample vault.

## Scope

### In

- Implement the v5 behavior described in `SPEC/TUSKER_V5_FINAL_SPEC.md`.
- Update code, templates, skill docs, config, generated indexes, and tests.

### Out

- Replacing Obsidian as the reading/editing surface.
- Building a full project-management web app.
- Making Markdoc mandatory.

## Canon

- `SPEC/TUSKER_V5_FINAL_SPEC.md`
- `SPEC/IMPLEMENTATION_SEQUENCE.md`
- `SPEC/MIGRATION_PLAN.md`

## Invariants

- Markdown remains the source of truth.
- Task status remains in frontmatter.
- Runtime/audit/generated state is split into `_system/runs`, `_system/events`, `_system/generated`.
- Domains and doc_nodes are controlled by `_config/docs-map.yaml`.

## Task stack

See `BACKLOG_INDEX.md`.
