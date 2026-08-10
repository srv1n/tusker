---
schema: "tusker.domain/v7"
kind: "domain"
id: "project"
project: "tusker"
title: "Project"
status: "current"
summary: "Repository-wide Tusker V7 canon, orchestration, skills, and validation policy."
capsule:
  skip_when: "Skip when the task packet names exact files or you only need task proof/runtime state."
  use_when: "Use when no narrower domain is declared or work changes workflow, automation, validation, or skills."
  what: "Project domain index routing repo-wide Tusker V7 canon and implementation areas."
source_of_truth:
  - ".tusker/SKILL.md"
  - ".tusker/WORKFLOW.md"
  - "tusker.yaml"
canonical_files:
  - "INDEX.md"
  - "CANON.md"
  - "cmd/tusker/**"
  - "internal/**"
  - "skills/tusker/**"
created_at: "2026-06-04 00:00:00 +0000 UTC"
updated_at: "2026-08-04T06:33:48Z"
state_rev: "sha256:c37d537abe3c23cb8f084cc96fa9c1eef82df2efbefa5d050b944f9f90dfbd15"
---

# Project Domain

## Read This When

- A task changes V7 workflow semantics, automation planning, runner dispatch, proof policy, skill packaging, or repository bootstrap behavior.
- A task has no narrower domain route.

## Do Not Read This When

- You only need a specific task packet and exact file paths are already named.
- You are trying to inspect raw event or runtime logs. Use Tusker commands instead.

## Current Canon

Read `CANON.md` before implementation.

## Start Here

| Need | Read |
|---|---|
| V7-only lifecycle rules | `CANON.md` |
| Operator skill package | `skills/tusker/SKILL.md` and `skills/tusker/references/` |
| Execution identity, direct-work visibility, provider children, or timeline recovery | `docs/system/execution-observability.md`, `docs/runbooks/execution-observability.md` |
| Adaptive reconciliation | `docs/runbooks/adaptive-reconciliation.md`, `cmd/tusker/adaptive_reconcile.go` |
| Runtime dispatch policy | `cmd/tusker/automation_commands.go`, `cmd/tusker/daemon.go`, `cmd/tusker/workflow.go` |
| Task/proof validation | `cmd/tusker/v7_validation.go`, `internal/v7schema/schema.go` |
