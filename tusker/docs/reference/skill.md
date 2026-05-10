---
schema: "tusker.doc/v5"
id: "reference/skill"
title: "Skill and AGENTS guidance"
type: "doc"
node: "reference/skill"
audience: "developer"
mode: "reference"
agent_layer: "none"
kind: "reference"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "reference/skill"
publish_description: "Skill and AGENTS guidance."
created: "2026-04-29"
updated: "2026-05-10"
---

# Skill and AGENTS guidance

## Summary

The Tusker skill bundle is the primary agent contract. It teaches Codex, Claude Code, and other compatible harnesses how to read the vault, choose epics, search for existing work, create tasks, move status, attach evidence, resolve docs impact, and close only after verification.

The skill must keep context use proportional to the job. A lookup is not a closeout, and a backlog note is not a migration. Agents should use `tusker list --type epic`, `tusker search`, one-epic `tusker list --epic <ACR> --type task --open`, `tusker show <ID> --capsule`, `tusker compact <ID>` for old noisy notes, and exact task paths before broad file reads. They should not read `Attachments/**`, `_system/generated/**`, build logs, or raw runner logs unless the user is explicitly asking for evidence forensics.

| Lane | Use for | Expected proof |
|---|---|---|
| `look-up` | Answer whether work exists, inspect status, find a related task | Epic list, search result, one epic's open tasks, selected task capsule; no mutation and no validation |
| `bookkeeping` | Add a note, shape backlog, avoid duplicates | Reindex only if indexes changed; validate only if task schema changed |
| `implementation` | Make code or docs changes tied to a task | Risk-scaled evidence |
| `closeout` | Move tracked implementation work to review or done | Docs resolution, verification, and validation |

The current source of truth is `skill/SKILL.md` plus the files under `skill/references/**`, `skill/docs/**`, and `skill/assets/**`. Repo-local downstream copies live at `.agents/skills/tusker` and `.claude/skills/tusker`; refresh them from the local source payload with:

```bash
go run ./cmd/tusker update --repo . --repo-only --no-bin
```

Use the installed `tusker update --repo . --repo-only --no-bin` only after the installed binary has the desired embedded skill payload.

## Current status

| Surface | Status |
|---|---|
| Source skill | `skill/SKILL.md` and `skill/references/**` |
| Codex repo-local skill | `.agents/skills/tusker` |
| Claude repo-local skill | `.claude/skills/tusker` |
| Default worker runner | `codex` |
| Reviewer lane | enabled by `WORKFLOW.md` |
| Reviewer actor | `agent-reviewer` |
| Auto-close risks | `low`, `medium` |
| Human-gated risks | `high`, `critical` |

## Agent close contract

Agents must treat `review` as a verification checkpoint, not as automatic completion. When `WORKFLOW.md` enables `reviewer`, a separate reviewer run may inspect the task without editing implementation files.

The reviewer checks:

- acceptance contract
- task scope and surprise file changes
- evidence and verification log
- docs impact resolution
- relevant tests or smoke checks
- caveats that change scope

For low/medium risk work, the configured reviewer may run `docs check`, `verify`, and `close` when every gate passes. For high/critical work, reviewer output is advisory and a human must verify and close.

## Distribution rule

Do not hand-edit `.agents/skills/tusker` or `.claude/skills/tusker` as the long-term source. Patch `skill/**`, then refresh the repo-local copies. If a repo-local copy has useful divergence, first promote that guidance back into `skill/**`, then regenerate.
