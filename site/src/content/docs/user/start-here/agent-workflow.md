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
  updated: "2026-05-11"
  verified_at: "2026-04-28"
---

# Tusker

Use the CLI first. Edit markdown directly only when the CLI cannot express the change.

Default to the lightest lane that preserves truth. A backlog note is not a
closeout. A closeout is not a migration. Do not spend context proving things the
user did not ask to prove.

## Default Path

1. Find the vault. Omit `--vault` unless discovery fails.
2. Start with `tusker list --type epic`; read `tusker/README.md` only when the project overview is needed.
3. Use `tusker search "<term>" --type task` before creating possible duplicate work.
4. Drill into one likely epic only if needed: `tusker list --epic <ACR> --type task --open`.
5. Read a selected note with `tusker show <ID> --capsule`; use section flags before `--full`.
6. Create or update the narrowest relevant task record.
7. If closing tracked implementation work, record evidence, docs impact, verification, and close state.
8. Run `tusker validate` before saying done when task records changed.

Tell the user the ID and the epic rationale after creating work.

## Lanes

| Lane | Use For | Read | Prove |
|---|---|---|---|
| `look-up` | Find whether work exists, answer status, inspect a thread | `tusker list --type epic`, `tusker search`, one epic's open tasks, one named task | No mutation, no validate |
| `bookkeeping` | Add a work-log note, update a backlog task, avoid duplicates | Epic list plus named task(s) | `tusker reindex` if indexes changed; `validate` only if task schema changed |
| `implementation` | Code/docs changes for an active task | Task plus directly relevant files | Risk-scaled evidence; close gates only when moving to `done` |
| `closeout` | Move work to review/done | Task evidence/docs/verification sections | Docs resolution, independent verification, `validate` |

For syntax, read `references/COMMANDS.md` only when the command is not obvious.

## Engineering Discipline

For non-trivial implementation, bug diagnosis, tests, or refactors:

- Convert the request into behavior-level success criteria before editing.
- Work in vertical slices: one observable behavior, one check, one implementation step.
- Test through public interfaces. Mock only system boundaries you do not control.
- Build a fast feedback loop before debugging; if you cannot reproduce, say what you tried and ask for a real artifact.
- Keep changes surgical. No speculative abstractions, drive-by cleanup, or unrelated formatting churn.

For the fuller checklist, load `references/ENGINEERING_DISCIPLINE.md`.

## Non-Negotiables

- Use `task` as the execution unit. A bug is `task(kind: bug)`.
- Every task belongs to an epic.
- `domains` are broad areas; `doc_nodes` are exact docs targets from `_config/docs-map.yaml`.
- Close only after evidence, docs impact resolution, verification, and validation.
- Do not turn backlog/bookkeeping updates into closeout ceremony.
- Do not let the implementation worker self-certify. The configured reviewer actor is independent and may close only risks listed in `reviewer.auto_close_risks`.
- Do not edit generated files in `_system/generated/**` or `site/src/content/docs/**`.
- Documentation tickets are not permission to dump source text. Before writing docs, choose the audience, Diátaxis mode, and source authority.
- Human docs are synthesized outputs. Do not publish task records, evidence logs, D-note bodies, generated manifests, or agent-only instructions as user/developer prose.

## Context Budget Rules

- Prefer `tusker list`, `tusker search`, `tusker show`, and `tusker compact` over raw file reads.
- Use `tusker compact <ID>` before reading or editing an old noisy task; it is a dry-run unless `--write` is provided.
- Never read `Attachments/**`, `_system/generated/**`, build logs, or raw runner logs by default.
- Redirect noisy command output to a file and read only the failure summary or a small tail.
- For transcript/token analysis, use `tusker context audit --file <jsonl>`.
- For repository search, use `rg -l`, `rg --count`, narrow globs, or capped output before broad `rg -n`.
- In generated-heavy repos, avoid repeated full `git status --short`; use counts or a capped preview until staging/commit state matters.
- Do not add `Execution plan` or `Work log` by default. Put durable truth in capsule, acceptance, evidence, verification, and knowledge delta.

## Load References Only When Needed

| Need | Read |
|---|---|
| Decide whether this skill applies | `references/TRIGGERS.md` |
| Log, resume, or close routine work | `references/QUICK_MODE.md` |
| Command syntax | `references/COMMANDS.md` |
| Frontmatter, enums, sections | `references/SCHEMA.md` |
| Medium/high/critical task intake | `references/FORMAL_INTAKE.md` |
| Risk, evidence, verification bar | `references/RISK_AND_EVIDENCE.md` |
| Non-trivial implementation, bugs, TDD, refactors, architecture seams | `references/ENGINEERING_DISCIPLINE.md` |
| Lifecycle/status rules | `references/WORKFLOW.md` |
| Docs-map, Diátaxis, docs close gate, publishing | `references/DOCS_PUBLICATION.md` |
| Durable docs page creation | `references/DOC_PAGES.md` |
| Epic canon choices | `references/CANON_LOCATIONS.md` |
| Break large specs into tasks | `references/TASK_DECOMPOSITION.md` |
| Templates, bases, snippets, repo-contract assets | `references/RESOURCES.md` |
| Obsidian Bases views | `references/BASES.md` |
| Repo AGENTS/CLAUDE contract | `references/REPO_CONTRACT.md` |
| Install/update/setup | `references/PREREQUISITES.md` |
| Optional plugins | `references/OPTIONAL_PLUGINS.md`, `references/PLUGIN_COMPAT.md` |
| Runtime/orchestration internals | `docs/ORCHESTRATION_RUNBOOK.md` |

Use `tusker new ...` when possible; it writes the right templates. For docs
work, run the docs inspection commands from `references/COMMANDS.md`, then load
`references/DOCS_PUBLICATION.md` before drafting prose.
