---
schema: "tusker.knowledge/v6"
node: "runtime/reference/reviewer-lane"
title: "Reviewer lane"
domain: "runtime"
kind: "reference"
audience: "developer"
agent_layer: "capsule"
canonical_status: "draft"
summary: "Reviewer lane."
aliases:
  - "reviewer lane"
  - "agent reviewer"
  - "review handoff"
  - "auto close"
source_of_truth:
  - "tusker/WORKFLOW.md"
  - "cmd/tusker/daemon.go"
  - "cmd/tusker/commands_v5.go"
stale_when:
  paths:
    - "tusker/WORKFLOW.md"
    - "cmd/tusker/daemon.go"
    - "cmd/tusker/commands_v5.go"
related_nodes: []
related_epics: []
publish:
  include_in_llms: true
  lane: "internal"
  path: "runtime/reference/reviewer-lane"
created_at: "2026-05-12"
updated_at: "2026-05-12"
---

# Reviewer lane

## Read this when

Read this when reviewer lane is the narrowest matching knowledge node.

## Do not read this when

Do not read this for unrelated domains or task proof history.

## Source of truth

- `tusker/WORKFLOW.md`
- `cmd/tusker/daemon.go`
- `cmd/tusker/commands_v5.go`

## Related

- [[runtime/CANON]]

## Recent changes

<!-- tusker:backrefs:begin -->
- [[KNO-T-0007]] touched this knowledge node.
<!-- tusker:backrefs:end -->
