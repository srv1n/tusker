---
title: "Agent recipe: using Tusker"
description: "Agent recipe: using Tusker."
tusker:
  agent_layer: "standalone"
  audience: "agent"
  canonical_status: "draft"
  id: "agents/tusker-skill"
  mode: "how-to"
  publish_path: "internal/agents/use-tusker"
  route: "/internal/agents/use-tusker/"
  source_kind: "vault_doc"
  source_of_truth:
    - "skill/SKILL.md"
    - "AGENTS.md"
  source_path: "docs/agents/use-tusker.md"
  stale_when_paths:
    - "skill/**"
    - "AGENTS.md"
    - "CLAUDE.md"
    - "cmd/tusker/cli.go"
  summary: "Agent recipe: using Tusker."
  tags: []
  updated: "2026-05-10"
---

# Agent recipe: using Tusker

## Goal

Use Tusker as the execution ledger for agent-first software work: choose the right epic, create or update tasks, keep docs impact explicit, attach evidence, and close work only after verification.

## Inputs

- User request or active task ID.
- Vault path, usually `tusker`.
- Progressive epic index from `tusker list --type epic`.
- Project overview from `tusker/README.md` only when needed.
- Docs catalog from `tusker/Docs.md` and `_config/docs-map.yaml`.
- Relevant canon under `tusker/docs/**`.

## Preconditions

- Start with `tusker list --type epic`; read `tusker/README.md` only when the project overview is needed.
- Pick an existing epic when the request fits; create a new epic only for a durable workstream.
- Use `tusker search` before broad repository search when the question is about existing tracker work.
- Use task IDs, not story IDs, for executable work.
- Use `doc_nodes` from `_config/docs-map.yaml`; do not invent them.
- Treat `_system/generated/**`, `Attachments/**`, raw runner logs, and full build logs as artifact stores, not default context.

## Steps

1. Inspect the short epic roster and choose the likely epic.
2. Search for duplicates with `tusker search "<term>" --type task` when creating or updating tracker work.
3. Drill into one epic with `tusker list --epic <ACR> --type task --open` only when open-task context is needed.
4. Read a selected task with `tusker show <ID> --capsule` before opening the full markdown.
5. For old noisy notes, run `tusker compact <ID>` as a dry-run before opening or editing the full file.
6. Create or update the narrowest relevant `task` with clear scope, acceptance criteria, verification plan, and knowledge delta when the work changes durable understanding.
7. Set `domains` for broad routing and `doc_nodes` for exact docs impact.
8. Implement the work in the repo or vault, keeping generated indexes rebuildable.
9. Run focused tests first, then the broader validation path when the change touches shared behavior.
10. Resolve docs impact for every targeted node with apply, verified no-op, or a waiver with a reason.
11. Attach evidence or record verification output in the task.
12. Move the task through review, verification, and close only when gates are satisfied.

## Context discipline

Use the lightest lane that preserves truth.

| Lane | Use for | Context budget |
|---|---|---|
| Lookup | Answer status or find existing work | `tusker list --type epic`, `tusker search`, one epic's open tasks, one task capsule |
| Bookkeeping | Add notes or shape backlog | Named task plus epic roster; validate only when schema changed |
| Implementation | Change code or docs | Task plus directly relevant files |
| Closeout | Move work to review or done | Evidence, docs resolution, verification, validation |

Do not read attachments, generated indexes, raw runner logs, or full build logs unless the user is explicitly asking for evidence forensics. Save large command output to a file and bring back only the failure summary or a small tail.

## Review lane behavior

If `WORKFLOW.md` enables `reviewer`, moving a task to `review` can trigger an independent reviewer run. The reviewer is not the implementation worker and should not edit implementation files.

Default policy:

| Risk | Reviewer behavior |
|---|---|
| `low`, `medium` | reviewer may run `docs check`, `verify`, and `close` after all gates pass |
| `high`, `critical` | reviewer leaves advisory evidence; a human verifies and closes |

Reviewer attribution is explicit. Low/medium auto-close records the configured `reviewer.actor` as verifier/closer, normally `agent-reviewer`. Human-gated close records the human verifier and closer instead.

## Validation

- `tusker validate --vault tusker --json` returns no errors.
- `tusker reindex --vault tusker --json` regenerates indexes without hand-editing them.
- Targeted tests for the changed code pass.
- `tusker docs check <TASK-ID>` reports each impacted node and the expected docs action.
- Generated docs outputs are rebuilt when public or agent-readable docs changed.

## Failure modes

- Unknown `doc_nodes`: read `_config/docs-map.yaml` and choose an existing node or add the node as part of docs-system work.
- Missing knowledge delta: add a real before/after row; “updated implementation” is not enough.
- Docs impact unresolved: apply the docs update, verify no-op, or waive with a reason.
- Generated file drift: rerun `tusker reindex` or `tusker docs export` instead of editing generated output.
- Epic mismatch: move the task or create the right epic before continuing.

## Rollback

- Revert only the files changed for the current task.
- Regenerate indexes after rollback.
- Leave a work-log entry explaining the rollback and any remaining docs impact.

## Escalate when

- The requested change conflicts with the v5 spec or current vault data.
- A migration could destroy user-authored notes.
- The docs-map needs a new domain or node and the ownership is unclear.
- Tests prove the existing behavior contradicts the task’s canon.

## Manual intervention points

- Choosing a new epic acronym.
- Waiving docs updates for product or release-significant work.
- Approving canonical docs for publication.
- Resolving conflicts between final-pack specs and active vault reality.

## Source of truth

- `_config/docs-map.yaml`
- `tusker/README.md`
- `tusker/Docs.md`
- `skill/SKILL.md`
- `AGENTS.md`

## Stale when

- CLI commands or task lifecycle rules change.
- The docs close gate changes.
- The skill guidance changes.
- The docs-map node model changes.
