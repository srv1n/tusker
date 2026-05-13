---
schema: "tusker.knowledge/v6"
node: "knowledge-system/canon"
title: "Knowledge System canon"
domain: "knowledge-system"
kind: "canon"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "Domain knowledge graph, indexer, freshness, routing, backrefs, and publication."
aliases:
  - "knowledge-system canon"
  - "knowledge system"
source_of_truth:
  - "tusker/SKILL.md"
stale_when:
  paths:
    - "tusker/SKILL.md"
publish:
  include_in_llms: true
  lane: "internal"
  path: "knowledge-system/canon"
created_at: "2026-05-12"
updated_at: "2026-05-12"
tags:
  - "knowledge-system"
---

# Knowledge System canon

## Read this when

Read this for the current model, invariants, defaults, and boundaries for knowledge system.

## Do not read this when

Do not use this as task proof; open linked tasks only when implementation history or evidence matters.

## Current model

This domain records current durable truth for knowledge system.

## Invariants

- Keep current truth in domain knowledge pages.
- Keep task proof in `tusker/epics/**`.
- Prefer source code over prose when exact behavior conflicts.

## Current defaults

- New knowledge starts as draft canon until verified.
- Route through this canon before opening historical tasks.

## Deprecated behavior

- Do not treat task files as canonical documentation.

## Source of truth

- `tusker/SKILL.md`

## Open questions

- Add domain-specific open questions here as the implementation matures.

## Related

- [[knowledge-system/INDEX]]

## Recent changes

<!-- tusker:backrefs:begin -->
- [[KNO-T-0003]] touched this knowledge node.
- [[KNO-T-0004]] touched this knowledge node.
- [[KNO-T-0006]] touched this knowledge node.
- [[KNO-T-0007]] touched this knowledge node.
<!-- tusker:backrefs:end -->
