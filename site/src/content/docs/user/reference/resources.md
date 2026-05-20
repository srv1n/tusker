---
title: "Resources"
description: "Load this when inspecting, copying, repairing, or explaining bundled skill resources. Do not load for routine task logging."
tusker:
  audience: "user"
  publish_path: "user/reference/resources"
  route: "/user/reference/resources/"
  source_kind: "repo_doc"
  source_path: "skill/references/RESOURCES.md"
  summary: "Load this when inspecting, copying, repairing, or explaining bundled skill resources. Do not load for routine task logging."
  tags:
    - "reference"
  updated: "2026-05-18"
---

# Resources

Load this when inspecting, copying, repairing, or explaining bundled skill resources. Do not load for routine task logging.

## Rule

Prefer the CLI. Use bundled resources only for install/update/repair or when explaining what the skill ships.

## Templates

Templates live in `assets/templates/`. They are source templates for `tusker init` and `tusker new`.

| Template | Use |
|---|---|
| `epic.md` | Epic workstream file. Prefer `tusker new epic`. |
| `task.md` | Normal executable V7 task. Prefer `tusker new task`. |
| `bug.md` | Bug task shape. Prefer `tusker new task --kind bug`. |
| `doc.md` | Human-facing durable docs page. Prefer the docs CLI if available. |
| `agent-doc.md` | Agent-facing runbook or recipe. |
| `dashboard.md` | Plain markdown dashboard seed. |
| `cheatsheet.md` | Plain markdown quick reference. |
| `daily.md` | Optional daily-note helper. |

Do not hand-copy a template when the CLI can create the file. If you patch a template, update the matching generated-vault behavior in the Go code too.

## Repo contract assets

| Asset | Use |
|---|---|
| `assets/snippets/AGENTS.md.snippet` | Inject or repair repo-local agent instructions. |
| `assets/snippets/CLAUDE.md.snippet` | Inject or repair Claude-facing repo instructions. |
| `assets/repo-contract/AGENTS.workflow-snippet.md` | Explain or patch the workflow contract block. |
| `assets/gitignore.recommended` | Suggested ignore entries for generated/runtime files. |

## Icons and metadata

Icons live in `assets/icons/` and are referenced by `agents/openai.yaml`. They are generic Tusker icons, not a requirement for any editor.

`agents/openai.yaml` is UI metadata. Keep it short and aligned with `SKILL.md`.

## Scripts

This skill ships no standalone scripts. The deterministic execution surface is the `tusker` CLI.

If a future resource needs repeatable logic that agents keep rewriting, add it under `scripts/`, test it directly, and mention exactly when to run it from `SKILL.md`.
