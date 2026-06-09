---
schema: tusker.project_skill/v7
kind: project_skill
name: tusker-project
project: tusker
status: current
description: "Repo-local routing skill for Tusker's own codebase."
operator_skill: tusker
source_of_truth:
  - tusker.yaml
  - .tusker/WORKFLOW.md
  - .tusker/knowledge/domains/**
canonical_files:
  - cmd/tusker/**
  - internal/**
  - skill/**
created_at: 2026-06-04T00:00:00Z
updated_at: 2026-06-04T00:00:00Z
---

# Tusker Project Skill

## Read This When

Use this file when an agent is working inside this repository and needs to decide which task, domain canon, command, or proof route to read.

## Do Not Read This When

Do not use this as the global Tusker operator manual. The global/operator skill lives under `skill/` and explains how to operate Tusker in any repository.

## First Action

Run:

```bash
tusker automation plan <TASK-ID> --json
```

Then run:

```bash
tusker packet <TASK-ID> --for agent
```

Read only the domain files named by the plan or packet.

## Routing Algorithm

1. Read this file.
2. Read the task packet for the assigned task.
3. Read `knowledge/domains/project/INDEX.md` when the task has no narrower domain.
4. Read a domain `CANON.md` before implementation.
5. Use path-scoped search after reading the routed domain; do not scan the whole repo by default.
6. Record proof in the task or required evidence object; never paste raw logs.

## Domains

| Domain | Read when |
|---|---|
| `project` | Repository-wide workflow, V7-only policy, validation, skill packaging, and orchestration invariants. |

## Repo Command Policy

Prefer exact commands over broad sweeps:

```bash
go test ./cmd/tusker -run <focused-test> -count=1
go test ./internal/... -count=1
go test ./...                # only for broad proof or pre-close validation
```

## Forbidden Source Truth

Do not treat these as canonical product truth:

- `research/legacy/**`
- `site/**`
- `docs/publication.yaml`
- old visible `tusker/` vault state
- raw runtime logs
- orphaned event history

Those paths are intentionally absent from the V7 baseline.

## Validation

Before closing work, verify the exact acceptance contract and run the smallest command that proves it. High and critical risk tasks require human acceptance.
