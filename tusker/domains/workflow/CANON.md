---
schema: "tusker.knowledge/v6"
node: "workflow/canon"
title: "Workflow canon"
domain: "workflow"
kind: "canon"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "Task lifecycle, close gates, verification, evidence, and review policy."
aliases:
  - "workflow canon"
  - "workflow"
source_of_truth:
  - "tusker/SKILL.md"
stale_when:
  paths:
    - "tusker/SKILL.md"
publish:
  include_in_llms: true
  lane: "internal"
  path: "workflow/canon"
created_at: "2026-05-12"
updated_at: "2026-05-12"
tags:
  - "workflow"
---

# Workflow canon

## Read this when

Read this for the current model, invariants, defaults, and boundaries for workflow.

## Do not read this when

Do not use this as task proof; open linked tasks only when implementation history or evidence matters.

## Current model

This domain records current durable truth for workflow.

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

- [[workflow/INDEX]]

## Recent changes

<!-- tusker:backrefs:begin -->
_No task proof recorded yet._
<!-- tusker:backrefs:end -->
