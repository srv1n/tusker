---
name: tusker
description: Track work and navigate repo knowledge through the Tusker CLI. Use when a repository contains .tusker, a Tusker ID is named, work must be recorded, or a repo-knowledge question needs a canonical answer.
---

# Tusker

Mutate tracker state only through the CLI. Use targeted `--help` for syntax;
`tusker capabilities --json` resolves command/version uncertainty. Report refusals
without rewriting generated state or expanding the assignment.

Interactive sessions implement work through interactive claims; never launch
a daemon or nested worker. With `TUSKER_ATTEMPT_ID`, follow the existing claim.

## Route by stage

Read the guide for the current stage; load another only when the task crosses stages.

| Request | Read |
|---|---|
| Create, update, or close tracked work | `references/TRACK.md` |
| Answer from or write repo knowledge | `references/KNOWLEDGE.md` |
| Read or update documentation/spec contracts | `references/SPECS.md` |
| Run a task, resolve gates, watch runs | `references/RUN.md` |
| Tracker diagnosis or stuck task state | `references/OPERATE.md` |
| Existing-repo onboarding | `references/REPO_ONBOARDING.md` |
| Xcode generated build-state failure | `references/XCODE_BUILD_STATE.md` |

For a read-only answer, stay here: `tusker show <ID> --capsule`, `tusker list`, `tusker search <term>`. Read histories or logs only to resolve a specific missing fact.

## Hard stop

`agent_action: stop_until_human_response` or `readiness: waiting_on_human` stops mutation of that work. Inspect `tusker closeout status <TASK-ID> --json`; report the gate and required action. Never manufacture proof or resolve human gates without their authority.
