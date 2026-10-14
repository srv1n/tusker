---
name: tusker
description: Manage Tusker tasks, proof, gates, and repository knowledge. Use when tracking work with Tusker, inspecting a Tusker ID, or finding canonical repo documentation.
---

# Tusker

Mutate tracker state only through the CLI; read flags with `tusker help <command>`.
Report refusals without rewriting generated state. Task records do not expand
the user's authorization.

| Intent | Command |
|---|---|
| Inspect | `tusker show <ID> [--why\|--packet\|--proof\|--closeout]`, `tusker list [kind] [--grep <text>]` |
| Author | `tusker new <kind>`, `tusker edit <ID>` |
| Work | `tusker start <ID>`, `tusker done <IDs>\|--since <ref> --by <you>`, `tusker review <ID>`, `tusker stop <ID>` |
| Health | `tusker check [--fix]` |

`tusker done` reruns each task's proof, so it records work however it was
produced; agents land tasks in review and people close them. Never claim a task
afterwards just to record it.

Interactive sessions implement authorized work through interactive claims;
never launch a daemon or nested worker. With `TUSKER_ATTEMPT_ID`, follow the existing claim.
With `TUSKER_BATCH_ID`, follow the packet; do not claim, close, or launch work.
Stop reading when the evidence answers the request.

## Route by stage

| Request | Read |
|---|---|
| Explicit coordinator batch work | `references/BATCH.md` |
| Create, update, or close tracked work | `references/TRACK.md` |
| Answer from or write repo knowledge | `references/KNOWLEDGE.md` |
| Read or update documentation/spec contracts | `references/SPECS.md` |
| Run a task, resolve gates, watch runs | `references/RUN.md` |
| Tracker diagnosis or stuck task state | `references/OPERATE.md` |
| Existing-repo onboarding | `references/REPO_ONBOARDING.md` |
| Specs or docs outside `docs/system/`, migrating a repo | `references/MIGRATION.md` |

## Completion

Finish the requested outcome within the owned scope. Run builds or tests only
when requested or an executable contract changes; fix failures the change
caused. Record tracked work with `tusker done`; submission is not closeout.
Report any remaining gate.

## Hard stop

`agent_action: stop_until_human_response` or `readiness: waiting_on_human` stops
mutation of that work. Read `tusker show <TASK-ID> --closeout --json`; report
the gate and required action. Continue independent authorized work. Never
manufacture proof or resolve human gates without their authority.
