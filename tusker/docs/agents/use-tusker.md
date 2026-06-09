---
schema: "tusker.doc/v5"
id: "agents/tusker-skill"
project: "tusker"
title: "Agent recipe: using Tusker"
type: "doc"
node: "agents/tusker-skill"
audience: "agent"
kind: "runbook"
status: "draft"
summary: "Agent runbook for using Tusker V7 work tracking, evidence, gates, and review requests."
domains:
  - "skill"
source_of_truth:
  - "skill/SKILL.md"
  - "skill/references/COMMANDS.md"
  - "skill/references/WORKFLOW.md"
  - "AGENTS.md"
stale_when_paths:
  - "skill/**"
  - "AGENTS.md"
  - "CLAUDE.md"
  - "cmd/tusker/cli.go"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "agents/use-tusker"
publish_description: "Agent recipe: using Tusker."
created: "2026-04-29"
updated: "2026-05-15"
---

# Agent recipe: using Tusker

## Goal

Use Tusker as the execution ledger for agent-first software work: choose the
right epic, create or update tasks, attach acceptance-linked evidence, handle
gates explicitly, and request review mechanically when implementation is done.

## Inputs

- User request or active task ID.
- Vault path, usually `tusker`.
- Epic/task roster from `tusker list` and `tusker search`.
- Repo-specific project skill from `tusker/SKILL.md`.
- V7 domain canon under `tusker/knowledge/domains/<domain>/`.
- This human-facing page is a projection/guide, not V7 source truth. Durable
  V7 project truth is `tusker/SKILL.md` plus `tusker/knowledge/domains/**`.

## Preconditions

- Start with `tusker list`; read `tusker/README.md` only when the
  project overview is needed.
- Pick an existing epic when the request fits; create a new epic only for a
  durable workstream.
- Use `tusker search` before broad repository search when the question is about
  existing tracker work.
- Use `tusker context audit --file <jsonl>` before raw-reading Codex JSONL.
- Use `tusker improve scan` only when explicitly asked to package repeated
  workflows or maintain reusable agent assets; it is not a routine closeout
  step.
- Use bounded shell reads: `rg -l`, `rg --count`, narrow globs, capped previews,
  and quiet/JSON build commands for noisy tools.
- Treat `Attachments/**`, raw runner logs, generated packets, and full build
  logs as artifact stores, not default context.
- For Tusker skill changes, treat `skill/**` as the source payload. Use
  `tusker update --repo . --repo-only --no-bin` for repo-local `.agents` and
  `.claude` copies without user-level writes.
- For V7 project knowledge, load `tusker/SKILL.md`, then the narrowest
  `tusker/knowledge/domains/<domain>/INDEX.md`, then that domain's `CANON.md`.

## Steps

1. Inspect the short epic roster and choose the likely epic.
2. Search for duplicates with `tusker search "<term>" --type task`.
3. Drill into one epic with `tusker list --epic <ACR> --type task --open` only
   when open-task context is needed.
4. Read a selected task with `tusker show <ID> --capsule` before opening the
   full markdown. For human terminal reading, use `tusker print <ID>`; for
   editor/Obsidian handoff, use `tusker open <ID>`.
5. For old noisy notes, run `tusker compact <ID>` as a dry-run before opening or
   editing the full file.
6. Create or update the narrowest relevant task with clear scope, acceptance,
   verification, evidence expectations, gates, and knowledge delta.
7. For non-trivial implementation, bug diagnosis, TDD, or refactors, load
   `skill/references/ENGINEERING_DISCIPLINE.md`.
8. Implement the work in the repo or vault, keeping generated outputs
   rebuildable.
9. Run focused tests first, then broader validation when the change touches
   shared behavior.
10. Attach evidence that covers concrete acceptance IDs.
11. Finish with `tusker finish <TASK-ID> --summary "<what changed and where proof lives>"`.
12. If blocked on human input, credentials, external setup, or another
   dependency, create/propose a gate with owner, action, verification, and why
   the agent cannot do it. Use reviewer/subagent work for code review, diffs,
   test/log inspection, docs review, and implementation judgment.
13. Move to close only from `review`; close policy checks gates and accepted
   evidence.

## Finish Contract

A worker agent may stop only in one of these states:

| State | Required proof |
|---|---|
| Review requested | Evidence covers acceptance, attempt is in handoff, and task is in `review` or has a status proposal to `review` |
| Blocked by gate | Gate exists or is proposed with owner, action, verification, blocked task links, and `why_agent_cannot` for human/external blockers |
| Explicit failure | Attempt is failed with summary, evidence/log pointer, and follow-up gate/task when useful |

If using lower-level commands on an implementation branch, run
`tusker propose status <TASK-ID> --status review` after handoff. Attempt handoff alone is not the review queue.

Never leave completed implementation work in `ready`, `active`, or `rework` with
only an attempt handoff. Handoff is attempt state; review status is the review
queue.

## Context Discipline

Use the lightest lane that preserves truth.

| Lane | Use for | Context budget |
|---|---|---|
| Lookup | Answer status or find existing work | `tusker list`, `tusker search`, one epic's open tasks, one task capsule |
| Bookkeeping | Add notes or shape backlog | Named task plus epic roster; validate only when schema changed |
| Implementation | Change code or docs | Task plus directly relevant files |
| Closeout | Move/propose work to review or done | Evidence, gates, attempts, proposals, validation |

Do not read attachments, generated indexes, raw runner logs, or full build logs
unless the user is explicitly asking for evidence forensics. Save large command
output to a file and bring back only the failure summary or a small tail.

## Improvement Scan Behavior

`tusker improve scan` is the supported way to turn repeated work into reusable
agent assets.

| Mode | Behavior |
|---|---|
| dry-run | Print a shortlist from Tusker history without mutating files. |
| `--write` | Store the report under `tusker/feedback/improvements/`. |
| `--apply` | Create high-confidence missing agent runbook drafts under `tusker/docs/agents/`. |

Do not enable Codex sessions, Claude transcripts, Memories, or Chronicle unless
the user explicitly opts in. When provider/model/reasoning flags are used, treat
them as runtime profile labels only; do not copy them into task frontmatter or
make them source truth.

## Feedback Reducer Behavior

`tusker feedback signals --since <date>` turns recent events, task contracts,
and bounded token usage into derived product facts. `--write` stores JSON under
`tusker/feedback/signals/YYYY-MM-DD/`.

`tusker feedback review --since <date>` renders the daily product packet:
facts, likely causes, proposed actions, and human decisions. Keep raw logs and
transcripts out of the packet.

`tusker feedback promote <signal-id>` defaults to dry-run. Use `--write` only
for one bounded action: create/update/link a task, create a decision, write a
gate/runbook/skill/CLI proposal record, or explicitly skip weak evidence.

## Review Lane Behavior

Moving a V7 task to `review` requests independent review. The reviewer is not
the implementation worker and should not edit implementation files.

| Risk | Reviewer behavior |
|---|---|
| `low`, `medium` | reviewer may verify and close when policy allows and all gates pass |
| `high`, `critical` | reviewer leaves advisory evidence; a human verifies and closes |

Reviewer attribution is explicit. Proposal acceptance defaults to a human actor,
and self-acceptance is blocked unless policy explicitly allows it.

## Validation

- `tusker validate --vault tusker --json` returns no errors for the changed
  tracker state.
- Focused behavior-level tests or smoke checks for changed code pass.
- Evidence records include command/result/artifact summary and acceptance IDs
  covered.
- Generated dashboards and packets are rebuilt by Tusker, not hand-edited.

## Failure Modes

- Missing acceptance proof: add accepted evidence or a waiver before finish/close.
- Open gate: satisfy with durable evidence, waive with reason, or leave the task
  blocked.
- Human gate for agent-capable work: route code review, diffs, test/log
  inspection, docs review, and implementation judgment to reviewer/subagent work.
- Bad or unclear spec: create a human-owned `decision` gate with the agent's
  recommended resolution in `suggestion`.
- Attempt handoff without review request: run `tusker finish` or create the
  status proposal to `review`.
- Generated file drift: rerun the generator instead of editing generated output.
- Epic mismatch: move the task or create the right epic before continuing.

## Rollback

- Revert only the files changed for the current task.
- Regenerate indexes/dashboards after rollback.
- Leave a concise failed-attempt summary and any remaining gate/follow-up.

## Escalate When

- The requested change conflicts with the V7 spec or current vault data.
- A migration could destroy user-authored notes.
- A gate requires human credentials, payment, production access, or security
  review.
- Tests prove the existing behavior contradicts the task's canon.

## Source Of Truth

- `skill/SKILL.md`
- `skill/references/COMMANDS.md`
- `skill/references/WORKFLOW.md`
- `skill/references/RISK_AND_EVIDENCE.md`
- `tusker/SKILL.md`
- `tusker/knowledge/domains/**`

## Stale When

- CLI commands or lifecycle rules change.
- Gate/evidence/proposal policy changes.
- The skill guidance changes.
- The V7 knowledge domain model changes.
