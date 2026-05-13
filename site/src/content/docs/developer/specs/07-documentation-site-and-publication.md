---
title: "07 - Documentation Site And Publication"
description: "Tusker V5 treats docs as durable pages, not work items."
tusker:
  audience: "developer"
  canonical_status: "historical"
  deprecated: true
  owner_epic: "ORC"
  publish_path: "developer/specs/07-documentation-site-and-publication"
  publish_section_title: "Specs"
  route: "/developer/specs/07-documentation-site-and-publication/"
  source_kind: "repo_doc"
  source_path: "docs/specs/07-documentation-site-and-publication.md"
  summary: "Tusker V5 treats docs as durable pages, not work items."
  superseded_by: "/user/start-here/agent-workflow/"
  tags:
    - "specs"
  updated: "2026-04-29"
  verified_at: "2026-04-28"
---

# 07 - Documentation Site And Publication

Tusker V5 treats docs as durable pages, not work items.

The current docs philosophy, layout, Diátaxis model, CLI inspection surface, freshness model, and generated-output contract are documented in [`../documentation-model.md`](/developer/documentation-model/). This older spec page remains a compact implementation note.

## Source Model

Docs live under `tusker/docs/**` and use:

```yaml
schema: tusker.doc/v5
type: doc
node: reference/cli
title: "CLI Reference"
audience: developer
kind: reference
domains: [cli]
canonical_status: draft
publish: true
publish_lane: internal
publish_path: reference/cli
publish_description: Primary V5 CLI surface.
```

`_config/docs-map.yaml` maps exact `node` values to source pages, domains, audience, kind, and publish metadata.

The CLI exposes the map and generated catalog directly:

- `tusker docs model`
- `tusker docs map`
- `tusker docs catalog`
- `tusker docs freshness`

## Task Integration

Tasks name broad `domains` and exact `doc_nodes`.

If `doc_nodes` is non-empty, `tusker close` requires every node to be resolved by:

- `tusker docs apply <TASK-ID> --node <node>`
- `tusker docs waive <TASK-ID> <node> --reason <reason>`
- an explicit verified no-op resolution

## Export

`tusker docs export` reads:

- V5 docs pages from `tusker/docs/**`
- repo publication registry from `docs/publication.yaml`
- generated publication index from `_system/generated/publication.index.json`

It emits:

- `site/src/content/docs/**`
- generated navigation/content/canon manifests
- `site/public/canon-manifest.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

## Link Rewriting

Supported inputs:

| Input | Meaning |
|---|---|
| `reference/cli` | doc node |
| `MEM` | epic |
| `MEM-T-0007` | task |
| relative markdown links | local links from source doc |
| repo-relative markdown links | registered repo docs |

Unpublished internal links are preserved as readable text or rewritten to known published routes when available.

## Validation

Publication validation checks:

- publish path shape
- publish path collisions
- stale generated manifests
- removed routes and redirects
- missing canon entries
- docs-map node resolution

## Rule

Docs publication is part of product correctness. If a task changes how the system is used, operated, or understood, the task must name the affected `doc_nodes` before close.
