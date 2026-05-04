---
schema: tusker.doc/v5
id: reference/docs-pipeline
title: Docs routing, impact hook, and publication model
type: doc
node: reference/docs-pipeline
audience: developer
kind: canon
domains: [docs, workflow, migration]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/docs-pipeline
publish_description: Docs routing, impact hook, waiver flow, and publication rules.
created: 2026-04-29
updated: 2026-04-29
---

# Docs routing, impact hook, and publication model


Tusker v5 treats documentation as a close gate, not as an optional cleanup pass.

## Routing model

```text
domains   = broad human grouping
doc_nodes = exact automation targets
```

`doc_nodes` must resolve through `_config/docs-map.yaml`.

## Docs impact hook

When a task reaches review/close and `doc_nodes` is non-empty:

1. read the task body
2. read the knowledge delta
3. inspect changed files / diff
4. read target doc pages
5. produce one of:
   - patch proposal
   - no-op verification
   - waiver required

This is an agentic run, not a deterministic linter.

## Waiver model

A waiver is explicit and per-node.

It records:

- task id
- node id
- actor
- date
- reason

## Publication model

The current repo already has a docs exporter that emits navigation, content manifest,
canon manifest, removed routes, and llms text. v5 keeps that capability alive.

The change is the source model:

```text
legacy:
  D-notes inside epic folders + publication metadata

v5:
  doc pages in tusker/docs/ + docs-map.yaml + retained publish metadata
```

## Periodic sweeper

A periodic docs sweeper is useful, but not required for phase 1.
The close-time gate is the mandatory part.
