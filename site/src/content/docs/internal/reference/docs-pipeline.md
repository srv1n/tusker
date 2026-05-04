---
title: "Docs routing, impact hook, and publication model"
description: "Docs routing, impact hook, and publication model."
tusker:
  agent_layer: "capsule"
  audience: "developer"
  canonical_status: "draft"
  id: "tusker/docs-system"
  mode: "explanation"
  publish_path: "internal/reference/docs-pipeline"
  route: "/internal/reference/docs-pipeline/"
  source_kind: "vault_doc"
  source_of_truth:
    - "cmd/tusker/docs_map.go"
    - "cmd/tusker/docs_impact.go"
    - "cmd/tusker/docs_export.go"
  source_path: "docs/reference/docs-pipeline.md"
  stale_when_paths:
    - "cmd/tusker/docs_*.go"
    - "cmd/tusker/docs_map.go"
    - "skill/references/DOCS_PUBLICATION.md"
  summary: "Docs routing, impact hook, and publication model."
  tags: []
  updated: "2026-04-29"
---

# Docs routing, impact hook, and publication model

## Summary

Tusker v5 uses a docs-map as the controlled access layer between work items and durable documentation. Tasks name exact `doc_nodes`; docs pages carry Diátaxis metadata; generated catalogs turn that metadata into reader-facing navigation.

The important bit: Diátaxis is a design model, not a folder law. Pages can live wherever the vault needs them, but every docs-map node declares the page's dominant reader intent.

## Diátaxis model

| Mode | Reader need | Tusker use |
|---|---|---|
| `tutorial` | Learn by doing | First-run paths and onboarding sequences |
| `how-to` | Complete a task | Procedures, workflows, operator guides, agent recipes |
| `reference` | Look up facts | CLI, schema, validator, template contracts |
| `explanation` | Understand why | Architecture, decisions, lifecycle rationale |

Reader navigation uses labels such as Start here, Guides, Reference, Concepts, Troubleshooting, Examples, and For agents. Raw Diátaxis mode remains metadata unless a project intentionally exposes it.

## Docs-map contract

Every node in `_config/docs-map.yaml` must declare:

- `id`: stable doc node, for example `tusker/docs-system`
- `page` or `path`: vault Markdown source under `docs/**`
- `domain`: controlled area from the same map
- `mode`: `tutorial`, `how-to`, `reference`, or `explanation`
- `audience`: `developer`, `user`, `operator`, `support`, `release`, `agent`, or `internal`
- `agent_layer`: `none`, `capsule`, or `standalone`
- `source_of_truth`: files that define the truth for the page
- `stale_when.paths`: paths that should trigger freshness review

The validator fails unknown domains, unknown doc nodes, invalid modes, invalid audiences, invalid agent layers, and nodes missing freshness metadata.

## Task impact flow

Tasks use `domains` for broad routing and `doc_nodes` for exact docs impact. High-risk tasks also need a structured `## Knowledge delta` table that explains what changed in the reader's mental model.

`tusker docs model` explains the documentation philosophy, Diátaxis modes, agent layers, and close gate.

`tusker docs map` lists controlled domains and doc nodes from `_config/docs-map.yaml`.

`tusker docs map <DOC-NODE>` inspects one node, including its page, domain, mode, audience, agent layer, source-of-truth files, and stale triggers.

`tusker docs catalog` shows the generated reader-facing IA from `Docs.md` and `docs.index.json`.

`tusker docs freshness` shows freshness state, linked tasks, last verified event, waivers, and stale triggers.

`tusker docs check <TASK-ID>` reads both frontmatter `doc_nodes` and any target nodes in the knowledge-delta table. It reports the mapped page, freshness state, and deltas per node.

`tusker docs apply <TASK-ID> --node <DOC-NODE>` records an applied docs resolution.

`tusker docs noop <TASK-ID> --node <DOC-NODE>` records a verified no-op when the target doc was checked and already correct.

`tusker docs waive <TASK-ID> <DOC-NODE> --reason "<reason>"` records a waiver. A waiver without a reason is rejected.

`tusker close <TASK-ID>` refuses to close a task with unresolved docs impact.

## Generated surfaces

`tusker reindex` writes:

- `_system/generated/docs.index.json` with docs-map metadata, catalog entries, freshness state, linked tasks, and waivers
- `Docs.md`, grouped by reader-facing IA rather than raw taxonomy
- `Dashboard.md` docs freshness block

`tusker docs export` writes:

- `site/src/content/docs/**`
- `site/src/generated/content-manifest.json`
- `site/src/generated/canon-manifest.json`
- `site/public/canon-manifest.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

The compact `llms.txt` is a curated index. The full file includes selected Markdown bodies in docs-map order so agents retrieve the same canon a human would read.

## Agent docs

Agent-facing docs are first-class pages. Use `audience: agent` with `agent_layer: standalone` for a complete runbook. Use `agent_layer: capsule` when a human-facing page needs a small agent note but should remain primarily human documentation.

Agent docs stay plain Markdown. Markdoc can be a publish layer later, but task contracts and vault docs do not depend on it.

## Freshness rules

A docs page is fresh when it has recent verification through page metadata or a task docs-resolution event. It is stale or pending when:

- the mapped page is missing,
- `last_verified_at` is empty and no task resolution exists,
- a task touched the node without apply/no-op/waiver,
- a source path listed in `stale_when.paths` changed and the page has not been reviewed.

Tusker does not pretend freshness is magic. It records the evidence path and makes stale docs visible.
