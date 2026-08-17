---
name: tusker
description: Track and manage Tusker tasks, acceptance, proof, gates, and lifecycle state through the installed CLI. Use when a repository contains .tusker, a Tusker ID is named, or work must be recorded or updated in Tusker.
---

# Tusker

## Capability check

Run `tusker capabilities --json` before relying on a command or schema. The
installed binary is executable truth. If a needed task-management command is
missing or broken, report that tracker failure plainly; do not invent a legacy
workflow or let tracker repair expand the user's coding request.

## Scope

This skill governs Tusker task records only: requirements, acceptance,
dependencies, status, proof, gates, review state, and closeout. It grants no
authority over repository operations, source changes, releases, providers, or
spending. Those remain governed by the user and the repository's own rules.

When Tusker is used, mutate its records through the CLI. Never hand-edit
lifecycle, proof, gate, or generated control fields. A tracker failure is not
a source-code failure and does not revoke an otherwise authorized user request.

## Execution modes

An interactive agent session implements the requested work itself and uses
Tusker only to record contracts, proof, gates, and lifecycle state. Interactive
work does not require daemon enablement or a daemon lifecycle claim. Never
start `tusker daemon run`, invoke `tusker automation dispatch`, or launch
nested workers from an interactive session; background execution belongs only
to an independently running resident daemon. When `TUSKER_ATTEMPT_ID` is set,
follow the claimed-run protocol and work only the claimed task.

## Route once

Read only the selected terminal guide:

| Request | Read |
|---|---|
| Requirements, decomposition, or creating tracked work | `references/PLAN.md` |
| Task status, proof, gates, review state, or closeout | `references/WORK.md` |
| Tracker diagnosis or stuck task state | `references/OPERATE.md` |
| Existing-repo onboarding | `references/REPO_ONBOARDING.md` |
| Xcode generated build-state failure | `references/XCODE_BUILD_STATE.md` |
| Documentation publication | `references/DOCS_PUBLICATION.md` |
| Obsidian/Bases projection | `references/OBSIDIAN_BASES.md` |

For a read-only answer, prefer `tusker show <ID> --capsule`, `tusker list`,
`tusker search`, and the smallest project-canon route. Do not scan task
history, attempts, attachments, generated indexes, or raw logs unless the
request explicitly requires them.

## CLI-only mutations

Use the installed command family for tracker changes:

```bash
tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
tusker new task --vault ./.tusker --epic APP --title "Implement auth"
tusker status <TASK-ID> ready --reason "Contract is actionable."
tusker verify add <TASK-ID> --covers A1 --check "<CHECK>" --result pass
tusker proof status <TASK-ID>
```

Use `tusker new gate`, `tusker gate`, `tusker discard`, and `tusker close`
for their matching lifecycle operations. Check command help when flags differ
from the examples; do not patch task markdown around a refusal.

## Hard stop rule

If Tusker reports `agent_action: stop_until_human_response` or
`readiness: waiting_on_human`, stop changing Tusker state. Inspect only:

```bash
tusker closeout status <TASK-ID> --json
tusker proof status <TASK-ID>
```

Report the exact human action and task/gate ID. Do not manufacture proof or
clear a human-owned gate.

## Compact loop

```text
check capability -> inspect capsule -> perform the requested work
-> record the smallest truthful tracker update -> stop
```

Keep proof compact: command plus PASS/FAIL, with noisy output in
`.tusker/scratch/<TASK-ID>/`. A task is a contract, not a chat log.
