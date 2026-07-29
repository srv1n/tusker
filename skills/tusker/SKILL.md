---
name: tusker
description: Operate Tusker task contracts, proof, review, gates, delivery plans, interactive work sessions, waves, fleet health, and resident-daemon orchestration. Use when a repo contains .tusker, a Tusker ID is named, work must be planned or tracked, or Tusker must explain why work is blocked.
---

# Tusker

## Capability check

Run `tusker capabilities --json` before relying on a command or schema. The
installed binary is executable truth; this skill routes intent. If the needed
capability or compatibility fingerprint is absent, report the exact mismatch
and supported repair. Do not improvise a legacy workflow.

## Route once

Read only the selected terminal guide:

| Request | Read |
|---|---|
| Requirements, decomposition, delivery DAG, review, held import, Start | `references/PLAN.md` |
| Interactive implementation, dispatched worker/reviewer, proof, gates, human wait | `references/WORK.md` |
| Resident daemon, automation, waves, integration, fleet repair, recovery | `references/OPERATE.md` |
| Existing-repo onboarding | `references/REPO_ONBOARDING.md` |
| Xcode generated build-state failure | `references/XCODE_BUILD_STATE.md` |
| Documentation publication | `references/DOCS_PUBLICATION.md` |
| Obsidian/Bases projection | `references/OBSIDIAN_BASES.md` |

For a read-only answer, prefer `tusker show <ID> --capsule`, path-scoped
status/search, and the smallest project-canon route. Do not scan task history,
events, attempts, evidence, attachments, generated indexes, or raw logs unless
the task explicitly requires them.

## Authority boundaries

- A user-opened Codex or Claude session implements the requested work itself.
  Interactive execution does not require daemon enablement or a daemon
  lifecycle claim. Never start `tusker daemon run`, invoke
  `tusker automation dispatch`, start a daemon service, or launch nested
  `codex exec` / `claude -p` workers.
- Planning, context, doctor, review, dry-run, task updates, proof, and held
  import are inert. They do not enable automation, arm work, dispatch, call a
  provider, move a ref, release, or spend.
- Unattended work requires an independently running resident daemon, project
  opt-in, exact wave authorization, and runtime preflight. Read-only planning
  never grants those authorities.
- `TUSKER_ATTEMPT_ID` means the daemon already claimed this dispatched attempt.
  Work only its task and do not spawn, claim, merge, land, close, or schedule.
- A dispatched reviewer inspects immutable inputs and submits one typed result.
  It never edits implementation, changes lifecycle state, or moves refs.
- Human gates are only for missing human capability/authority, unresolved
  product intent, or contractually subjective acceptance. Risk alone is not a
  human gate.

Use the CLI for lifecycle, proof, gate, wave, and evidence mutations; never
hand-edit their control fields. The installed skill owns tracker mechanics.
Repo `AGENTS.md` / `CLAUDE.md` are bootstrap pointers only, and repo
`.tusker/SKILL.md` owns project knowledge routing. Canonical skill changes
belong in `skills/tusker/**`; generated installs are repaired by skill sync.

## Hard Stop Rule

If Tusker reports `agent_action: stop_until_human_response` or
`readiness: waiting_on_human`, stop. Do not retry validation or spawn more
agents until the human changes the gate, task, proof, or code. Inspect only:

```bash
tusker closeout status <TASK-ID> --json
tusker proof status <TASK-ID>
```

Revalidation while waiting on human is noise.

## Compact loop

```text
inspect capability → select one guide → inspect capsule/project canon
→ perform the smallest authorized action → map proof to acceptance
→ submit/review/stop on the exact next owner
```

Keep proof compact: command plus PASS/FAIL, with noisy output in
`.tusker/scratch/<TASK-ID>/`. A task is a contract, not a chat log.
