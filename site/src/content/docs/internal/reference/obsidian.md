---
title: "Obsidian and view requirements"
description: "Obsidian and view requirements."
tusker:
  agent_layer: "none"
  audience: "developer"
  canonical_status: "draft"
  id: "reference/obsidian"
  mode: "how-to"
  publish_path: "internal/reference/obsidian"
  route: "/internal/reference/obsidian/"
  source_kind: "vault_doc"
  source_of_truth:
    - "cmd/tusker/v5_templates.go"
    - "skill/references/BASES.md"
  source_path: "docs/reference/obsidian.md"
  stale_when_paths:
    - "tusker/_system/views/**"
    - "skill/assets/bases/**"
    - "skill/references/BASES.md"
  summary: "Obsidian and view requirements."
  tags: []
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
| Review | `Tasks.base#Review` | tasks waiting for human verification |
| Follow-up | `Tasks.base#Follow-up` | tasks in `status: rework` |
| Ready | `Tasks.base#Ready` | shaped work not yet dispatching |
| Blocked | `Tasks.base#Blocked` | blockers and blocker reasons |
| Backlog | `Tasks.base#Backlog` | shaped future work |
| Task board | `Tasks.base#Board` | current execution/review work grouped by status |
| Bug board | `BugTasks.base#Board` | bug tasks using the same lifecycle |
| Docs pipeline | `Docs.base#Pipeline` | published docs queue grouped by lane |

`Follow-up` is only a view name. The actual status remains `rework`.

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
