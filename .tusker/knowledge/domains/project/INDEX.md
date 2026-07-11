---
schema: "tusker.domain/v7"
kind: "domain"
id: "project"
project: "tusker"
title: "Project"
capsule:
  skip_when:
    - "A task packet names a narrower implementation file."
  use_when:
    - "Work touches V7 workflow, automation, proof, skills, or validation policy."
  what: "Project domain index routing agents to repository-wide Tusker V7 canon."
status: "current"
summary: "Repository-wide Tusker V7 canon, orchestration, skills, and validation policy."
source_of_truth:
  - ".tusker/SKILL.md"
  - ".tusker/WORKFLOW.md"
  - "tusker.yaml"
canonical_files:
  - "INDEX.md"
  - "CANON.md"
  - "cmd/tusker/**"
  - "internal/**"
  - "skill/**"
created_at: "2026-06-04 00:00:00 +0000 UTC"
updated_at: "2026-07-06T16:21:59Z"
state_rev: "sha256:bc79651cee71f1185e66548c1c36a39fb3210657e15bf51fae13b6e3c42ad992"
capsule:
  skip_when: "Skip when the task packet names exact files or you only need task proof/runtime state."
  use_when: "Use when no narrower domain is declared or work changes workflow, automation, validation, or skills."
  what: "Project domain index routing repo-wide Tusker V7 canon and implementation areas."
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
