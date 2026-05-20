---
title: "Agent Workflow"
description: "Operating contract for agents working with Tusker in a markdown-first vault."
tusker:
  audience: "user"
  canonical: true
  canonical_status: "approved"
  owner_epic: "ORC"
  publish_path: "user/start-here/agent-workflow"
  route: "/user/start-here/agent-workflow/"
  source_kind: "repo_doc"
  source_path: "skill/SKILL.md"
  summary: "Operating contract for agents working with Tusker in a markdown-first vault."
  tags:
    - "start-here"
    - "workflow"
  updated: "2026-05-18"
  verified_at: "2026-04-28"
---

# Tusker

Tusker is a repo-local work ledger. Use the CLI first. Edit markdown only when the CLI cannot express the change, and never hand-edit protected lifecycle fields.

The core rule: **do the smallest truthful work, prove it once, then stop when the next owner is not the agent.**

## Hard Stop Rule

Before continuing an existing task, check whether the task is already waiting on a human or another external owner.

Preferred check, when the installed CLI supports it:

```bash
tusker closeout status <TASK-ID> --json
```

Fallback check:

```bash
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID>
tusker search <TASK-ID> --type gate --status open --json
```

Stop immediately when any of these is true:

- `agent_action: stop_until_human_response`
- `readiness: held` with `next_owner: human:*` or a newer schema-specific human-wait readiness
- `next_owner` starts with `human:`
- all machine proof gaps are closed and the only remaining gaps are `human_signoff`, `manual_smoke`, product/security approval, credentials, device access, CI/external service, or a human-owned gate
- a valid closeout packet/checkpoint already exists for the current state fingerprint

In that state, do **not** validate again, inspect more files, spawn subagents, refresh dashboards, or edit task/evidence/gate records. Reply with the last clean validation snapshot, the review packet path, and the exact human gates or decisions pending.

## Validation Budget

Validation is not a ritual. It is a state-change check.

Run validation only after:

- you changed source files, docs, task records, evidence, gates, generated projections, or workflow config;
- a human accepted/waived/rejected a gate;
- a previous validation fingerprint no longer matches;
- the user explicitly asks for fresh validation.

Never re-run the same validation against the same unchanged state just because the task is still open. One clean validation per state revision/fingerprint is enough.

Budgets per task state revision:

| Action | Budget |
|---|---:|
| Full validation after mutation | 1 |
| Broad audit/subagent audit | 1 |
| Focused failing test attempts before summarizing/gating | 3 |
| Closeout packet emission | 1 |
| Revalidation while waiting on human | 0 unless user explicitly asks |

## Default Path

1. Find the Tusker vault. Omit `--vault` unless discovery fails.
2. For lookup, start with `tusker list --type epic`, then `tusker search "<term>" --type task`.
3. Before creating work, search for duplicates.
4. Read the smallest useful task view: `tusker show <ID> --capsule`; use section flags before `--full`.
5. Create or update the narrowest relevant task record.
6. For implementation, satisfy the task's proof mode and move/propose the task to review.
7. For closeout, classify remaining gaps by owner: machine, reviewer, human, or external.
8. If only human/external gaps remain, emit/update the review packet once and stop.

Tell the user the task ID and why that epic was selected when creating work.

## Lanes

| Lane | Use For | Read | Validate |
|---|---|---|---|
| `look-up` | Find existing work, answer status, inspect a thread | Epic list, search results, one named task | Never |
| `bookkeeping` | Add a follow-up, update a backlog/ready task, avoid duplicates | Named task(s), maybe one epic | Only if schema/lifecycle changed |
| `implementation` | Code/docs change for one current task | Task capsule, acceptance, relevant source files | Once after mutation |
| `closeout` | Move machine-complete work to review/done/waiting-human | Proof status, gates, evidence, docs impact | Once per changed fingerprint |
| `human-wait` | Human/reviewer/external owner is the only blocker | Last closeout/checkpoint only | Never unless user asks |

For syntax, load `references/COMMANDS.md` only when the command is not obvious.

## V7 State Contract

Use V7 semantics by default.

Task lifecycle status:

```text
idea -> backlog -> ready -> review -> done
                  ^          |
                  |          v
                rework <-----+

any nonterminal state -> cancelled | superseded
```

Do not use task `status: active` as V7 lifecycle truth. Claimed/running/leased is runtime state, not task status.

Use readiness and ownership to explain why a task is not agent-runnable. Current-compatible human wait is `readiness: held` plus `next_owner: human:*`; if the installed schema later supports a dedicated human-wait readiness, it means the same stop rule:

```yaml
readiness: ready | blocked_by_gate | blocked_by_dependency | waiting_on_review | waiting_on_ci | held | done | cancelled | superseded
next_owner: agent | agent:<name> | reviewer:<name> | human:<name> | external:<service>
agent_action: continue | request_review | stop_until_human_response | stop_until_external_response
```

A task is agent-runnable only when:

```text
kind == task
status in ready|rework
readiness == ready
next_owner is agent or agent:<name>
no valid closeout checkpoint says stop
```

## Protected Fields

You may read all Tusker work records.

Implementation agents may add or update:

- inline verification rows;
- evidence records required by the proof mode;
- attempt summaries;
- inbox proposals;
- documentation and knowledge updates;
- decision proposals;
- gates for human/device/env/CI/external blockers.

Do not directly edit these fields from an implementation branch:

```yaml
status:
readiness:
next_owner:
next_source:
next_ref:
next_action:
agent_action:
accepted_by:
accepted_at:
closed_at:
state_rev:
closeout_status:
```

Use CLI control commands or proposals.

## Proof Discipline

First inspect `proof_mode`. Do not create evidence files by default.

| Proof mode | Behavior |
|---|---|
| `inline` | Add concise verification rows with `tusker verify add`. No evidence file. |
| `card` | Create one evidence card summarizing acceptance coverage. |
| `artifact` | Create one evidence card plus the required screenshot, video, trace, CI link, or manual proof. |
| `audit` | Follow the task audit checklist and explicit gates. |

Evidence must prove acceptance. It is not a diary.

Do not commit as evidence:

- full passing logs;
- copied source files;
- generated indexes;
- raw sidecar dumps;
- full terminal transcripts;
- repeated negative collections;
- screenshots/videos that do not prove an acceptance item.

Use `.tusker/scratch/<TASK-ID>/` for raw debug output. Promote only selected artifacts into evidence.

## Finish And Closeout Contract

When implementation work is complete:

1. Satisfy the task proof mode.
2. Add or update the attempt summary.
3. Open or update the PR when the workflow uses PRs.
4. Prefer `tusker finish <TASK-ID> --request-review --summary "<what changed and where proof lives>"`.
5. Run validation once if you mutated files or Tusker records.
6. Classify remaining gaps by owner.
7. If machine gaps remain, report the smallest next machine action.
8. If only human/external gaps remain, emit/update a review packet or closeout checkpoint once, set/recognize `agent_action: stop_until_human_response`, then stop.
9. Do not mark the task `done` unless running as an allowed reviewer/control actor and close policy permits it.

Manual smoke, human signoff, security approval, credentials, device access, release approval, and product decisions are human/external gates unless the task explicitly assigns them to an agent-capable owner.

## Final Response Shape For Human Wait

When stopping for human review, answer in this shape:

```text
Machine work is complete.

Last clean validation:
- <command>: PASS
- fingerprint/state_rev: <value if known>

Review packet:
- <path>

Remaining human/external gates:
- <GATE-ID>: <owner> must <action>

Agent action: stop_until_human_response.
No further validation was run because the state has not changed.
```

## Engineering Discipline

For non-trivial implementation, bug diagnosis, tests, or refactors:

- Convert the request into behavior-level success criteria before editing.
- Work in vertical slices: one observable behavior, one check, one implementation step.
- Test through public interfaces. Mock only system boundaries you do not control.
- Build a fast feedback loop before debugging; if you cannot reproduce, say what you tried and ask for a real artifact.
- Keep changes surgical. No speculative abstractions, drive-by cleanup, or unrelated formatting churn.

After three failed focused attempts, stop and summarize the blocker or create a gate. Do not thrash.

For the fuller checklist, load `references/ENGINEERING_DISCIPLINE.md`.

## Context Budget Rules

- Prefer `tusker list`, `tusker search`, `tusker show`, and `tusker compact` over raw file reads.
- Never read attachments, generated indexes, build logs, raw runner logs, or full transcripts by default.
- Redirect noisy command output to a file and read only the failure summary or a small tail.
- Use capped search: `rg -l`, `rg --count`, narrow globs, or limited output before broad `rg -n`.
- Do not add `Execution plan`, `Work log`, or append-only diaries by default.
- Put durable truth in capsule, acceptance, verification, evidence, review packet, closeout checkpoint, and knowledge delta.

## Load References Only When Needed

| Need | Read |
|---|---|
| Decide whether this skill applies | `references/TRIGGERS.md` |
| Routine log/resume/close | `references/QUICK_MODE.md` |
| Prevent agent loops and handle human gates | `references/CLOSEOUT_PROTOCOL.md` |
| Command syntax | `references/COMMANDS.md` |
| Frontmatter, enums, sections | `references/SCHEMA.md` |
| Lifecycle/status rules | `references/WORKFLOW.md` |
| Risk, evidence, verification bar | `references/RISK_AND_EVIDENCE.md` |
| Medium/high/critical task intake | `references/FORMAL_INTAKE.md` |
| Non-trivial implementation, bugs, TDD, refactors, architecture seams | `references/ENGINEERING_DISCIPLINE.md` |
| Break large specs into tasks | `references/TASK_DECOMPOSITION.md` |
| Durable docs/publication work | `references/DOCS_PUBLICATION.md`, `references/DOC_PAGES.md` |
| Epic canon and project knowledge routes | `references/CANON_LOCATIONS.md` |
| Templates, snippets, repo contract assets | `references/RESOURCES.md` |
| Repo AGENTS/CLAUDE contract | `references/REPO_CONTRACT.md` |
| Install/update/setup | `references/PREREQUISITES.md` |
| Runtime/orchestration internals | `docs/ORCHESTRATION_RUNBOOK.md` |

Use `tusker new ...` when possible; it writes the right templates. Do not load the entire skill payload for a routine lookup.
