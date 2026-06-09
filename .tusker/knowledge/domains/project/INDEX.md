---
schema: tusker.domain/v7
kind: domain
id: project
project: tusker
title: Project
status: current
summary: "Repository-wide Tusker V7 canon, orchestration, skills, and validation policy."
source_of_truth:
  - .tusker/SKILL.md
  - .tusker/WORKFLOW.md
  - tusker.yaml
canonical_files:
  - INDEX.md
  - CANON.md
  - cmd/tusker/**
  - internal/**
  - skill/**
created_at: 2026-06-04T00:00:00Z
updated_at: 2026-06-04T00:00:00Z
state_rev: 1
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
| Operator skill package | `skill/SKILL.md` and `skill/references/` |
| Runtime dispatch policy | `cmd/tusker/automation_commands.go`, `cmd/tusker/daemon.go`, `cmd/tusker/workflow.go` |
| Task/proof validation | `cmd/tusker/v7_validation.go`, `internal/v7schema/schema.go` |
