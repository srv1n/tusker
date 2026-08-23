---
schema: "tusker.domain/v7"
kind: "domain"
id: "project"
project: "tusker"
title: "Project"
status: "current"
summary: "Durable project knowledge."
capsule:
  skip_when: "Skip when another domain is narrower or proof is the only need."
  use_when: "Read before a task changes project behavior or docs."
  what: "Routes readers to current repository facts and document guides."
source_of_truth:
  - "knowledge/domains/project/CANON.md"
canonical_files:
  - "INDEX.md"
  - "CANON.md"
created_at: "2026-08-23T10:56:31Z"
updated_at: "2026-08-23T15:03:08Z"
state_rev: "sha256:6a6bee1830e54bad549f1bb1a85d4dabef73c2f7fd4eb906ae2502184de70aa5"
---

# Project

## Summary

This domain maps the current repository facts.

## Read this when

- You need the source layout.
- You need the runtime boundary.
- You need the documentation route.
Execution identity, direct-work visibility, provider children, or timeline recovery are current execution topics.

## Canonical files

- `CANON.md`: current project truth.
- `INDEX.md`: reading order and links.

## Current guides

- `docs/system/00-overview.md`: system map.
- `docs/system/tasks-and-proof.md`: task lifecycle.
- `docs/system/orchestration.md`: daemon and run ownership.
- `docs/system/storage-and-runtime.md`: storage boundary.
- `docs/system/execution-observability.md`: execution identity and recovery.
- `docs/system/serve-ui.md`: local UI behavior.

## Invariants

- Keep durable facts in `CANON.md`.
- Keep procedures in a system guide or runbook.
- Keep task state in `.tusker/work/`.
- Treat generated maps as read-only outputs.

## Glossary

See `glossary.md`.
