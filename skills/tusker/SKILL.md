---
name: tusker
description: Manage Tusker tasks, proof, gates, and repository knowledge. Use when tracking work with Tusker, inspecting a Tusker ID, or finding canonical repo documentation.
---

# Tusker

Mutate tracker state only through the CLI. Use targeted `--help` for syntax;
`tusker capabilities --json` resolves command/version uncertainty. Report refusals
without rewriting generated state or expanding the assignment.

Interactive sessions implement authorized work through interactive claims;
never launch a daemon or nested worker. With `TUSKER_ATTEMPT_ID`, follow the
existing claim. Task records do not expand the user's authorization.

Use four verbs: inspect with `show` or `status`, run with `tusker run <ID>`,
observe with `runs`, and finish through proof and review. A run
directive queues the exact task or wave; project Background work is the only
daemon opt-in. Stop reading when the evidence answers the request.

## Route by stage

Read the guide for the current stage. Follow another reference only when its
condition applies; creating a handoff needs more context than checking status.

| Request | Read |
|---|---|
| Create, update, or close tracked work | `references/TRACK.md` |
| Answer from or write repo knowledge | `references/KNOWLEDGE.md` |
| Read or update documentation/spec contracts | `references/SPECS.md` |
| Run a task, resolve gates, watch runs | `references/RUN.md` |
| Tracker diagnosis or stuck task state | `references/OPERATE.md` |
| Queued work not progressing, Background-work scope, repair/escalation | `references/OPERATE.md` (Diagnosis and bounded self-recovery) |
| Existing-repo onboarding | `references/REPO_ONBOARDING.md` |
| Specs or docs outside `docs/system/`, migrating a repo | `references/MIGRATION.md` |
| Xcode generated build-state failure | `references/XCODE_BUILD_STATE.md` |

## Completion

Finish the requested outcome within the owned scope. For prose-only edits,
review the text; run builds or tests only when requested or an executable
contract changes. For implementation, fix failures caused by the change and
rerun affected checks. For tracked work, record proof and submit through the
lifecycle; submission is not review or closeout. Report any remaining gate.

## Hard stop

`agent_action: stop_until_human_response` or `readiness: waiting_on_human` stops
mutation of that work. Inspect `tusker closeout status <TASK-ID> --json`; report
the gate and required action. Continue independent authorized work. Never
manufacture proof or resolve human gates without their authority.
