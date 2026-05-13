---
schema: "tusker.knowledge/v6"
node: "knowledge-system/reference/freshness"
title: "Knowledge freshness"
domain: "knowledge-system"
kind: "reference"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "Knowledge freshness."
aliases: []
source_of_truth:
  - "cmd/tusker/v6_knowledge.go"
  - "cmd/tusker/v6_commands.go"
stale_when:
  paths:
    - "cmd/tusker/v6_knowledge.go"
    - "cmd/tusker/v6_commands.go"
related_nodes: []
related_epics: []
publish:
  include_in_llms: true
  lane: "internal"
  path: "knowledge-system/reference/freshness"
created_at: "2026-05-12"
updated_at: "2026-05-12"
---

# Knowledge freshness

## Read this when

Read this when knowledge freshness is the narrowest matching knowledge node.

## Do not read this when

Do not read this for unrelated domains or task proof history.

## Source of truth

- `cmd/tusker/v6_knowledge.go`
- `cmd/tusker/v6_commands.go`

## Related

- [[knowledge-system/CANON]]

## Recent changes

<!-- tusker:backrefs:begin -->
- [[KNO-T-0005]] touched this knowledge node.
<!-- tusker:backrefs:end -->
