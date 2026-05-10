---
title: "Template contract"
description: "Template contract."
tusker:
  agent_layer: "capsule"
  audience: "developer"
  canonical_status: "draft"
  id: "reference/templates"
  mode: "reference"
  publish_path: "internal/reference/templates"
  route: "/internal/reference/templates/"
  source_kind: "vault_doc"
  source_of_truth:
    - "cmd/tusker/v5_templates.go"
    - "skill/assets/templates"
  source_path: "docs/reference/templates.md"
  stale_when_paths:
    - "cmd/tusker/v5_templates.go"
    - "skill/assets/templates/**"
  summary: "Template contract."
  tags: []
  updated: "2026-05-10"
---

# Template contract

## Summary

Tusker task templates are risk-scaled and capsule-first.

Every task starts with the smallest durable context:

- `## Agent capsule`
- `## Intent`
- `## Acceptance contract`
- `## Evidence`

Medium tasks add scope, deliverables, and verification plan. High and critical
tasks add canon, code/system anchors, constraints, escalation conditions,
knowledge delta, and verification log. Critical tasks also add rollback.

Default templates do not include `## Execution plan` or `## Work log`. Those are
live scratchpads, not durable context. Use evidence summaries, verification
frontmatter, transitions, and review packets for audit fidelity.

Tusker omits empty optional frontmatter on write. Absence of `doc_nodes`,
`blocked_by`, `blocks`, `ai_tools`, or `tags` means the same thing as an empty
list. Use `tusker compact <ID>` to dry-run cleanup for older notes that still
carry empty fields or disposable placeholder sections.
