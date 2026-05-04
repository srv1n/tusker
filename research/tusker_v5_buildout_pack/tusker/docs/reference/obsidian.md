---
schema: tusker.doc/v5
id: reference/obsidian
title: Obsidian and view requirements
type: doc
node: reference/obsidian
audience: developer
kind: guide
domains: [obsidian, workflow]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/obsidian
publish_description: Dashboard, views, and vault readability requirements.
created: 2026-04-29
updated: 2026-04-29
---

# Obsidian and view requirements


Tusker still lives or dies on human readability in Obsidian.

## Required outcomes

- task files remain readable without a custom app
- epic pages clearly show task stack and status
- docs pages are global, not trapped inside a single epic
- dashboard and views move from story language to task language
- drag-and-drop / simple filtering should still be possible through frontmatter

## View changes required by v5

- replace story views with task views
- add docs catalog view from `_config/docs-map.yaml`
- make active/blocked/review lanes task-centric
- expose `kind`, `risk`, `doc_nodes`, and `blocked_by`
- stop showing daemon-only runtime sludge in card previews

## What not to do

Do not make Obsidian depend on a running reconciler just to know a task's current state.
