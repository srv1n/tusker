# Resources

Load this when you need to inspect, copy, repair, or explain bundled skill resources. Do not load this for routine task logging.

## Rule

Prefer the CLI. Use bundled resources when installing, refreshing, repairing, or explaining what the skill ships.

## Templates

Templates live in `assets/templates/`. They are source templates for `tusker init` and `tusker new`.

| Template | Use |
|---|---|
| `epic.md` | Epic workstream file. Prefer `tusker new epic`. |
| `task.md` | Normal executable task. Prefer `tusker new task`. |
| `bug.md` | Bug task shape. Prefer `tusker new bug` or `tusker new task --kind bug`. |
| `doc.md` | Human-facing durable docs page. Prefer `tusker new doc`. |
| `agent-doc.md` | Agent-facing runbook or recipe. Use when `audience: agent` or `agent_layer: standalone`. |
| `dashboard.md` | Vault dashboard seed. Written by `tusker init`. |
| `cheatsheet.md` | Quick reference seed. Written by `tusker init`. |
| `daily.md` | Optional daily-note helper. Copy only when the user wants daily notes. |

Do not hand-copy a template when the CLI can create the file. If you must patch a template, update the matching generated-vault template behavior in the Go code too.

## Bases Views

Bases views live in `assets/bases/` and are written to `_system/views/`.

| View | Use |
|---|---|
| `Epics.base` | Epic roster and status scanning. |
| `Tasks.base` | General task queues. |
| `BugTasks.base` | Bug-focused task view. |
| `Docs.base` | Durable docs pages and docs freshness. |

Use these when repairing Obsidian views or explaining the vault UI.

## Repo Contract Assets

| Asset | Use |
|---|---|
| `assets/snippets/AGENTS.md.snippet` | Inject or repair repo-local agent instructions. |
| `assets/snippets/CLAUDE.md.snippet` | Inject or repair Claude-facing repo instructions. |
| `assets/repo-contract/AGENTS.workflow-snippet.md` | Explain or patch the workflow contract block. |
| `assets/snippets/status-hooks.js` | Optional hook helper for teams wiring status automation. |
| `assets/gitignore.recommended` | Suggested ignore entries for generated/runtime files. |

Use the installer/init command before manual edits. Manual edits are for repair or review.

## Icons And Metadata

Icons live in `assets/icons/` and are referenced by `agents/openai.yaml`.

`agents/openai.yaml` is UI metadata. Keep it short and aligned with `SKILL.md`.

## Scripts

This skill ships no standalone scripts. The deterministic execution surface is the `tusker` CLI.

If a future resource needs repeatable logic that agents keep rewriting, add it under `scripts/`, test it directly, and mention exactly when to run it from `SKILL.md`.
