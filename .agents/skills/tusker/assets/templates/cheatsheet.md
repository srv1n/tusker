---
title: "Cheat Sheet"
type: "note"
created: "2026-04-29"
updated: "2026-04-29"
tags: ["cheatsheet"]
---

# Tusker Cheat Sheet

## Task flow

```text
draft -> backlog -> ready -> active -> review -> done
ready|active -> blocked -> ready|active
review -> rework -> active
active|review|blocked|rework|backlog -> cancelled
```

`review` is a checkpoint. With `reviewer.enabled`, the daemon can launch an independent review lane from `review`; low/medium work can close as `agent-reviewer`, and high/critical work stays human-gated.

## Current workflow settings

| Surface | Current setting |
|---|---|
| Worker dispatch | `active`, `rework` |
| Review checkpoint | `review` |
| Reviewer runner | `codex` |
| Reviewer actor | `agent-reviewer` |
| Auto-close | `low`, `medium` |
| Human close | `high`, `critical` |

## Common commands

```bash
tusker validate --vault ./tusker
tusker list --vault ./tusker --type task
tusker next --vault ./tusker
tusker claim --vault ./tusker MEM-T-0001 --as sarav
tusker status --vault ./tusker MEM-T-0001 active --actor sarav
tusker evidence --vault ./tusker MEM-T-0001 pr https://example.com/pr/123
tusker status --vault ./tusker MEM-T-0001 review --actor sarav
tusker verify --vault ./tusker MEM-T-0001 --by verifier
tusker close --vault ./tusker MEM-T-0001 --by sarav
```

## What to open

- `Dashboard.md` = landing page
- `Tasks.base#Board` = current execution work, grouped by status
- `Tasks.base#Ready` = shaped, unblocked current work, ready to pull
- `Tasks.base#Blocked` = current work waiting on dependencies or an external blocker
- `Tasks.base#Backlog` = shaped future work, not current release
- `Tasks.base#Needs Attention` = draft, blocked, review, rework — silent rotters
- `Tasks.base#By Epic` = open work grouped by epic
- `Tasks.base#Archive` = done + cancelled
- `BugTasks.base#Board` = current bug execution work, grouped by status

## Files and folders

- `_system/views/*.base` = Bases views
- `_system/generated/dashboard.json` = derived tracker/runtime snapshot
- `Attachments/<TASK-ID>/` = evidence files
