---
schema: "tusker.doc/v5"
id: "spec/v5-adoption"
title: "Tusker v5 adoption guide"
type: "doc"
node: "spec/v5-adoption"
audience: "developer"
mode: "how-to"
agent_layer: "none"
kind: "guide"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "spec/v5-adoption"
publish_description: "Tusker v5 adoption guide."
created: "2026-04-29"
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

## Codex-first operator loop adoption

V5 adoption and ORC runtime adoption are related but not identical.

| Capability | Adopt now? | Reason |
|---|---|---|
| V5 task schema, docs-map, dashboard, Bases | Yes | Public CLI and Obsidian files support this path. |
| `active`/`rework` as runnable tracker states | Yes | `WORKFLOW.md` and daemon polling code use these states. |
| Dashboard active/blocked/review/follow-up visibility | Yes in the checked-in vault and skill assets | The current `.base` files expose these views without `Orchestration.base`; keep code defaults in sync before claiming init/migration emits them. |
| Dashboard live-run block | Yes, as generated visibility | `tusker reindex` renders runtime rows when a project exists in the runtime store. |
| Codex-first daemon loop from Obsidian status to review | Yes for local operator use | `daemon`, `projects`, `runs`, and `refresh` are routed. Codex is the default live runner today; the runtime lane model is not Codex-specific. |
| Policy-driven agent reviewer lane | Yes for low/medium auto-close; advisory for high/critical | `WORKFLOW.md` reviewer policy starts an independent `review` lane for the configured runner. Configured reviewers can close low/medium only; high/critical still require a human gate. |

Use the operator loop for smoke tests and local runner pickup:

```bash
tusker projects add --repo . --vault ./tusker
tusker status <TASK-ID> active --actor <name>
tusker refresh --quiet
tusker runs inspect <TASK-ID> --json
tusker verify <TASK-ID> --by <verifier>
tusker close <TASK-ID> --by <reviewer>
```

The daemon queues continuation if a clean runner exit leaves the task in `active` or `rework`. A review handoff is honest only after Codex or a human moves the task to `review`; the daemon then writes a review packet from the completed implementation run.

If reviewer policy is enabled, a later poll can run an independent reviewer prompt for the task in `review`. Low/medium tasks may be verified and closed by the configured reviewer when the evidence, tests, docs, and scope checks pass. High/critical tasks stay in `review` until a human verifies and closes.

## Rollback

By default migration creates `tusker.backup-v5-YYYYMMDD-HHMMSS`. If a repo has a watcher that restores or deletes sidecar backups, rerun with `--no-backup` only after making your own git or filesystem checkpoint.
