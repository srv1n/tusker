---
name: tusker
description: "Operate Tusker repo-local task contracts, proof, review, human gates, and agent orchestration."
---

# Tusker Operator Skill

## When To Use

Use this skill when a repository contains a Tusker vault or a task asks you to create, pick up, dispatch, prove, review, close, or explain Tusker work.

## First Action

Find the task id. Then run:

```bash
tusker automation plan <TASK-ID> --json
```

If the plan is dispatchable, run:

```bash
tusker packet <TASK-ID> --for agent
```

Read only the packet and its routed project-skill/domain files before touching implementation code.

## Hard Rules

- A task is a contract, not a chat log.
- `active` is never a durable V7 task status.
- Runtime activity lives in runs, leases, sessions, attempts, and workspaces.
- Human gates stop the agent. Do not keep validating around them.
- Proof must map to acceptance. A vague summary is not proof.
- Raw logs do not belong in task markdown.
- Tags are generated projections; typed frontmatter is source of truth.


## Hard Stop Rule

When Tusker records `agent_action: stop_until_human_response` or `readiness: waiting_on_human`, stop execution. Do not re-run the same validations, spawn subagents, or invent a workaround. Check:

```bash
tusker closeout status <TASK-ID> --json
tusker proof status <TASK-ID>
```

Revalidation while waiting on human is noise unless the human changed the gate, task, proof, or code.

## Skill Boundary

Treat repo `AGENTS.md` / `CLAUDE.md` as bootstrap pointers only. The installed Tusker operator skill owns tracker mechanics. The repo `.tusker/SKILL.md` owns project knowledge routing.

Keep canonical skills separate from project memory. Generic skill improvements patch canonical skill source. Project facts go into repo-local config/state/profile files such as `.tusker/**`, `.chatgpt-handoff.json`, or `.chatgpt-handoff/profile.md`. Generated installs under `.agents/skills/**` and `.claude/skills/**` are sync/bundle outputs; do not hand-edit them unless they are symlinks to the canonical source.

## Final Response Shape For Human Wait

When machine work is complete but a human gate remains, answer with exactly what is blocked, what the human must do, and the Tusker task/gate id to resume from. Do not claim completion.

## Read Next

| Need | File |
|---|---|
| CLI commands | `references/COMMANDS.md` |
| Repo skill/source policy | `references/REPO_CONTRACT.md` |
| Lifecycle and statuses | `references/WORKFLOW.md` |
| Automation, runners, browser workers, fanout | `references/ORCHESTRATION.md` |
| Existing repo setup/onboarding | `references/REPO_ONBOARDING.md` |
| Human gates and closeout | `references/CLOSEOUT_PROTOCOL.md` |
| Proof modes and evidence | `references/RISK_AND_EVIDENCE.md` |
| Obsidian/Bases projections | `references/OBSIDIAN_BASES.md` |

## Default Agent Loop

```text
plan → packet → routed domain canon → scoped code search → edit → exact verification → proof → review/closeout
```

Do not replace this with broad repo scans unless the task explicitly requires discovery.
