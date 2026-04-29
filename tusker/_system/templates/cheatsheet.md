---
title: "Cheat Sheet"
type: "note"
created: "{{date}}"
updated: "{{date}}"
tags: ["cheatsheet"]
---

# Tusker Cheat Sheet

## Ticket flow

```text
intake -> active -> in_review[verification_requested] -> in_review[requested] -> done
                 -> blocked -> active
in_review[*] -> rework -> active
active|in_review -> cancelled
```

- `active` = this ticket should be worked
- `in_review + verification_requested` = worker pass finished; claims still need truth-checking
- `in_review + requested` = verified and waiting on human review
- `rework` = changes requested; goes back to `active`

## Runtime flow

- `unclaimed` = no agent has picked it up
- `running` = Codex/Claude is currently working
- `retry_queued` = waiting to resume/retry
- `interrupted` = stopped by operator

Durable ticket status and daemon runtime state are different on purpose.

## Common commands

```bash
tusker validate --vault ./tusker
tusker list --vault ./tusker
tusker handoff --vault ./tusker --id MEM-S-0001 --for worker
tusker set-status --vault ./tusker --id MEM-S-0001 --status active --actor sarav
tusker review verify --vault ./tusker --id MEM-S-0001 --by verifier
tusker review request-changes --vault ./tusker --id MEM-S-0001 --by sarav --summary "Tighten it"
tusker review approve --vault ./tusker --id MEM-S-0001 --by sarav
```

## Daemon commands

```bash
tusker projects add --repo . --vault ./tusker
tusker projects disable
tusker projects enable
tusker projects limits --max-active-runs 1
tusker daemon limits --max-active-runs 2
tusker daemon run
tusker daemon status --json
tusker runs --json
tusker runs inspect --id MEM-S-0001 --json
tusker runs logs --id MEM-S-0001 --follow
tusker runs interrupt --id MEM-S-0001
tusker retry now --id MEM-S-0001
```

- `projects disable` = tracker-only mode for this repo
- `projects limits` = per-project cap, hot-reloaded on next poll
- `daemon limits` = global cap across all projects, hot-reloaded on next poll

## What to open

- `Dashboard.md` = landing page
- `Dashboard.md` -> `Active stories` = ready to be worked
- `Dashboard.md` -> `Verification queue` = worker passes that still need truth-checking
- `Dashboard.md` -> `Live runs` = what the daemon is doing now
- `Stories.base#Human review` = verified work waiting on approval

## Files and folders

- `_system/views/*.base` = Bases views
- `_system/generated/dashboard.json` = derived tracker/runtime snapshot
- `Attachments/<STORY-ID>/` = evidence files
