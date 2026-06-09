---
schema: tusker.domain-canon/v7
kind: domain_canon
id: project/canon
project: tusker
domain: project
title: Project Canon
status: current
summary: "Current durable rules for Tusker's own repository."
source_of_truth:
  - .tusker/SKILL.md
  - .tusker/WORKFLOW.md
  - tusker.yaml
created_at: 2026-06-04T00:00:00Z
updated_at: 2026-06-04T00:00:00Z
state_rev: 1
---

# Project Canon

## Current Truth

- V7 is the only product surface in this repository.
- Durable task status never uses `active`.
- Dispatchable task states are `ready` and `rework`.
- Runtime activity is represented by run leases, attempts, sessions, and workspaces.
- Human-owned gates set `agent_action: stop_until_human_response` and `readiness: waiting_on_human`.
- Tags are projections; typed frontmatter is source of truth.
- Obsidian Bases and dashboards are generated views, not canonical state.
- Browser-backed ChatGPT work is a runner result source, not a direct state writer.

## Invariants

- A ready/rework task must have concrete acceptance and verification proof.
- Placeholder acceptance must block dispatch.
- Raw CLI output belongs in runtime scratch/logs, not task markdown.
- `tusker automation plan <task> --json` is the canonical pre-dispatch explanation.
- High and critical risk closeout requires human acceptance.
- Legacy V5/V6 docs, publication manifests, site export state, and checked-in event history are not default read paths.

## Verification

Use focused proof first:

```bash
go test ./cmd/tusker -run <test-name> -count=1
```

Use broad proof before closing cross-cutting changes:

```bash
go test ./...
```
