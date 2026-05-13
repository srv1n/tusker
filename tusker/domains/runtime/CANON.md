---
schema: "tusker.knowledge/v6"
node: "runtime/canon"
title: "Runtime canon"
domain: "runtime"
kind: "canon"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "Daemon dispatch, runner state, review lane, leases, attempts, sessions, and logs."
aliases:
  - "runtime canon"
  - "runtime"
source_of_truth:
  - "tusker/SKILL.md"
stale_when:
  paths:
    - "tusker/SKILL.md"
publish:
  include_in_llms: true
  lane: "internal"
  path: "runtime/canon"
created_at: "2026-05-12"
updated_at: "2026-05-12"
tags:
  - "runtime"
---

# Runtime canon

## Read this when

Read this for the current model, invariants, defaults, and boundaries for runtime.

## Do not read this when

Do not use this as task proof; open linked tasks only when implementation history or evidence matters.

## Current model

This domain records current durable truth for runtime.

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

- [[runtime/INDEX]]

## Recent changes

<!-- tusker:backrefs:begin -->
_No task proof recorded yet._
<!-- tusker:backrefs:end -->
