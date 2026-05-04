---
schema: tusker.doc/v5
id: spec/migration-v5
title: Tusker v5 migration plan
type: doc
node: spec/migration-v5
audience: developer
kind: guide
domains: [migration, schema, docs]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: spec/migration-v5
publish_description: Explicit migration plan from legacy Tusker to v5.
created: 2026-04-29
updated: 2026-04-29
---

# Tusker v5 migration plan


This is the explicit migration plan from legacy Tusker to the v5 model.

## Why migration is first-class work

Legacy Tusker already has:

- `story / bug / doc` note types
- large frontmatter
- lifecycle state on work notes
- D-note publication metadata
- docs export outputs like canon manifest and llms text
- a daemon and runtime store
- Obsidian dashboards and templates

That is not throwaway scaffolding. The migration must preserve the useful parts.

## Legacy → v5 mapping

| Legacy | v5 |
|---|---|
| `type: story` | `type: task, kind: <legacy change type or feature>` |
| `type: bug` | `type: task, kind: bug` |
| `MEM-S-0001` | `MEM-T-0001` if unused |
| `MEM-B-0007` | `MEM-T-0007` if unused; otherwise first available `MEM-T-####` |
| epic `epics/MEM/index.md` | `epics/MEM/MEM.md` |
| D-note under epic | doc page under `docs/` with `type: doc` |
| human-authored docs enum | derived from `doc_nodes` presence |
| huge frontmatter | minimal frontmatter + `_system/*` split |

## ID collision policy

Unifying `story` and `bug` into a single `task` namespace can collide.

Use this deterministic rule:

1. Read legacy work items in stable order: epic, then numeric suffix ascending, with `story`
   before `bug` when both claim the same numeric suffix.
2. The first item that claims `ABC-T-####` keeps it.
3. Any later collision gets the next available `ABC-T-####`.
4. Write the old→new mapping into the generated links index and the migration report.

This avoids random churn.

## Doc page migration policy

For legacy D-notes:

1. If `publish_path` exists, use it to derive the `node` and output path.
2. Else if the doc is canon for an epic, derive `<epic-lower>/overview`.
3. Else if it is a companion to a task or story, derive `<epic-lower>/tasks/<legacy-id-lower>`.
4. Else derive `misc/<slug(title)>`.
5. If the node collides, append a deterministic suffix and record the alias mapping.

Preserve where available:

- `audience`
- `canonical_status`
- `publish_path`
- `publish_url`
- `published_at`
- `redirect_from`
- `verified_at`
- `deprecated`
- `superseded_by`

Drop:

- work-lifecycle fields like `status: intake|ready|active|review|done`

## Migration command

```text
tusker migrate v5 --dry-run
tusker migrate v5 --apply
```

## Migration requirements

- idempotent
- report all rewrites
- rewrite wikilinks and in-repo markdown references
- generate alias map for old IDs and old doc paths
- support rollback by writing a snapshot/report before apply
- fail clearly on ambiguous cases that need human choice

## Expected outputs

```text
tusker/_system/generated/migration-v5-report.json
tusker/_system/generated/id-aliases.json
tusker/_config/docs-map.yaml
```

## Definition of done

Migration is not done when files move.
Migration is done when:

- old and new note models both parse during the transition window
- the docs exporter still emits valid outputs
- wikilinks resolve
- the validator understands the new model
- a sample legacy fixture migrates cleanly and repeatably
