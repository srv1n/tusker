---
title: "Tusker v5 adoption guide"
description: "Tusker v5 adoption guide."
tusker:
  agent_layer: "none"
  audience: "developer"
  canonical_status: "draft"
  id: "spec/v5-adoption"
  mode: "how-to"
  publish_path: "internal/spec/v5-adoption"
  route: "/internal/spec/v5-adoption/"
  source_kind: "vault_doc"
  source_of_truth:
    - "research/tusker_v5_implementation_spec.md"
    - "AGENTS.md"
  source_path: "docs/spec/v5-adoption.md"
  stale_when_paths:
    - "cmd/tusker/**"
    - "skill/**"
    - "tusker/WORKFLOW.md"
  summary: "Tusker v5 adoption guide."
  tags: []
  updated: "2026-05-04"
---

# Tusker v5 adoption guide

## Goal

Move an existing Tusker vault onto V5 without hand-renaming notes or leaving old story/bug concepts behind.

## Existing repo repair

Run this from the repo root after installing or rebuilding the current Tusker binary:

```bash
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
tusker validate --vault ./tusker
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site
tusker update --repo . --repo-only --no-bin
```

Use `--vault-only` when the repo already has its own `AGENTS.md`, `CLAUDE.md`, or project contract files and the goal is only to repair the Tusker vault.

## What migration changes

| Legacy shape | V5 shape |
|---|---|
| `type: story` | `type: task`, `kind: feature` |
| `type: bug` | `type: task`, `kind: bug` |
| `ABC-S-0001.md` | `ABC-T-0001.md` |
| `ABC-B-0001.md` | next non-conflicting `ABC-T-NNNN.md` |
| `epics/ABC/index.md` | `epics/ABC/ABC.md` |
| old wikilinks | rewritten to the new task IDs |
| missing docs map entries | added for published docs |

## Acceptance

- `tusker validate --vault ./tusker` exits with zero errors.
- No `*-S-NNNN.md`, `*-B-NNNN.md`, `Stories.base`, `Bugs.base`, or `story.md` files remain in the vault.
- `tusker list --vault ./tusker --type epic` shows the expected epic roster.
- `tusker docs build --vault ./tusker --site ./site` completes.

Warnings about missing V5 sections in old notes are migration debt, not a broken repo. Fix them when touching the note for real work.

## Codex-only operator loop adoption

V5 adoption and ORC runtime adoption are related but not identical.

| Capability | Adopt now? | Reason |
|---|---|---|
| V5 task schema, docs-map, dashboard, Bases | Yes | Public CLI and Obsidian files support this path. |
| `active`/`rework` as runnable tracker states | Yes | `WORKFLOW.md` and daemon polling code use these states. |
| Dashboard active/blocked/review/follow-up visibility | Yes in the checked-in vault and skill assets | The current `.base` files expose these views without `Orchestration.base`; keep code defaults in sync before claiming init/migration emits them. |
| Dashboard live-run block | Yes, as generated visibility | `tusker reindex` renders runtime rows when a project exists in the runtime store. |
| Codex-only daemon loop from Obsidian status to review | Yes for local operator use | `daemon`, `projects`, `runs`, and `refresh` are routed. Register the project, run the daemon or one `refresh` tick, and inspect runtime rows. |

Use the operator loop for smoke tests and local Codex pickup:

```bash
tusker projects add --repo . --vault ./tusker
tusker status <TASK-ID> active --actor <name>
tusker refresh --quiet
tusker runs inspect <TASK-ID> --json
tusker verify <TASK-ID> --by <verifier>
tusker close <TASK-ID> --by <reviewer>
```

The daemon queues continuation if a clean runner exit leaves the task in `active` or `rework`. A review handoff is honest only after Codex or a human moves the task to `review`; the daemon then writes a review packet from the completed run and leaves final verification to a human.

## Rollback

By default migration creates `tusker.backup-v5-YYYYMMDD-HHMMSS`. If a repo has a watcher that restores or deletes sidecar backups, rerun with `--no-backup` only after making your own git or filesystem checkpoint.
