---
schema: "tusker.doc/v5"
id: "reference/obsidian"
title: "Obsidian and view requirements"
type: "doc"
node: "reference/obsidian"
audience: "developer"
mode: "how-to"
agent_layer: "none"
kind: "guide"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "reference/obsidian"
publish_description: "Obsidian and view requirements."
created: "2026-04-29"
updated: "2026-05-04"
---

# Obsidian and view requirements

## Open the operator surface

Open `Dashboard.md` in the repo-local Tusker vault. The dashboard is the Obsidian operator surface for V5 task state and generated runtime summaries.

| Dashboard section | Source | Shows |
|---|---|---|
| Active epics | `Epics.base#Active` | epics that are not done/cancelled |
| Active work | `Tasks.base#Active` | tasks in `status: active` |
| Live runs | generated markdown block | runtime rows for active leases, if this vault is registered in the runtime store |
| Workflow status | static markdown table | current runner/reviewer policy defaults from `WORKFLOW.md` |
| Review | `Tasks.base#Review` | tasks waiting for reviewer or human verification |
| Follow-up | `Tasks.base#Follow-up` | tasks in `status: rework` |
| Ready | `Tasks.base#Ready` | shaped work not yet dispatching |
| Blocked | `Tasks.base#Blocked` | blockers and blocker reasons |
| Backlog | `Tasks.base#Backlog` | shaped future work |
| Task board | `Tasks.base#Board` | current execution/review work grouped by status |
| Bug board | `BugTasks.base#Board` | bug tasks using the same lifecycle |
| Docs pipeline | `Docs.base#Pipeline` | published docs queue grouped by lane |

`Follow-up` is only a view name. The actual status remains `rework`.

The workflow status table is intentionally static, not a generated runtime block. It is there so the root Obsidian surface states the operating policy at a glance: worker dispatch uses `active`/`rework`; `review` is a checkpoint; the default reviewer actor is `agent-reviewer`; low/medium work may auto-close; high/critical work stays human-gated.

## Live-run block

`tusker reindex` rewrites the dashboard block between:

```markdown
<!-- tusker:live-runs:begin -->
<!-- tusker:live-runs:end -->
```

The block reads runtime state from the default state root and matches registered projects by vault path. It shows rows for `claimed`, `running`, `retry_queued`, and `interrupted` runtime leases. If no matching project or live run exists, the dashboard says there are no live runs.

This is intentionally generated markdown, not a Base. Runtime leases, process ids, session refs, event paths, and log paths are not task frontmatter.

## Base rules

- Use stock Obsidian Bases view types only: `table`, `cards`, `list`, or `map`.
- Use `type: table` when grouping by status or epic. `cards` and `board` are not safe stock grouped views.
- Keep the shared-vault filter: `this.file.folder == "" || file.inFolder(this.file.folder)`.
- Include `file.name` in table orders so rows are clickable.
- Do not reference `Orchestration.base`. V5 ships `Epics.base`, `Tasks.base`, `BugTasks.base`, and `Docs.base`.
