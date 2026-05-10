---
schema: "tusker.doc/v5"
id: "reference/templates"
title: "Template contract"
type: "doc"
node: "reference/templates"
audience: "developer"
mode: "reference"
agent_layer: "none"
kind: "reference"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "reference/templates"
publish_description: "Template contract."
created: "2026-04-29"
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
