---
schema: "tusker.doc/v5"
id: "agents/tusker-skill"
title: "Agent recipe: using Tusker"
type: "doc"
node: "agents/tusker-skill"
audience: "agent"
mode: "how-to"
agent_layer: "standalone"
kind: "runbook"
domains:
  - "skill"
source_of_truth:
  - "skill/SKILL.md"
  - "AGENTS.md"
stale_when_paths:
  - "skill/**"
  - "AGENTS.md"
  - "CLAUDE.md"
  - "cmd/tusker/cli.go"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "agents/use-tusker"
publish_description: "Agent recipe: using Tusker."
created: "2026-04-29"
updated: "2026-04-29"
---

# Agent recipe: using Tusker

## Goal

Use Tusker as the execution ledger for agent-first software work: choose the right epic, create or update tasks, keep docs impact explicit, attach evidence, and close work only after verification.

## Inputs

- User request or active task ID.
- Vault path, usually `tusker`.
- Project overview from `tusker/README.md`.
- Docs catalog from `tusker/Docs.md` and `_config/docs-map.yaml`.
- Relevant canon under `tusker/docs/**`.

## Preconditions

- Read `tusker/README.md` before creating work.
- Pick an existing epic when the request fits; create a new epic only for a durable workstream.
- Use task IDs, not story IDs, for executable work.
- Use `doc_nodes` from `_config/docs-map.yaml`; do not invent them.
- Treat `_system/generated/**` as rebuildable output.

## Steps

1. Inspect the active vault overview and choose the epic that matches the request.
2. Create or update a `task` with clear scope, acceptance criteria, verification plan, and knowledge delta when the work changes durable understanding.
3. Set `domains` for broad routing and `doc_nodes` for exact docs impact.
4. Implement the work in the repo or vault, keeping generated indexes rebuildable.
5. Run focused tests first, then the broader validation path when the change touches shared behavior.
6. Resolve docs impact for every targeted node with apply, verified no-op, or a waiver with a reason.
7. Attach evidence or record verification output in the task.
8. Move the task through review, verification, and close only when gates are satisfied.

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
