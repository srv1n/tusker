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
  updated: "2026-05-10"
  verified_at: "2026-04-28"
---

# Tusker

Use the CLI first. Edit markdown directly only when the CLI cannot express the change.

Default to the lightest lane that preserves truth. A one-line backlog note is
not a closeout. A task closeout is not a migration. Do not spend context proving
things the user did not ask to prove.

## Core Loop

1. Find the vault. Omit `--vault` unless discovery fails.
2. Start with `tusker list --type epic`; read `tusker/README.md` only when the project overview is needed.
3. Use `tusker search "<term>" --type task` before creating possible duplicate work.
4. Drill into one likely epic with `tusker list --epic <ACR> --type task --open` only when open task context is needed.
5. Create or update the narrowest relevant task record.
6. If closing tracked implementation work, record evidence, docs impact,
   verification, and close state.
7. Run `tusker validate` before saying done when task records changed.

Tell the user the ID and the epic rationale after creating work.

## Lanes

| Lane | Use For | Read | Prove |
|---|---|---|---|
| `look-up` | Find whether work exists, answer status, inspect a thread | `tusker list --type epic`, `tusker search`, one epic's open tasks, one named task | No mutation, no validate |
| `bookkeeping` | Add a work-log note, update a backlog task, avoid duplicates | Epic list plus named task(s) | `tusker reindex` if indexes changed; `validate` only if task schema changed |
| `implementation` | Code/docs changes for an active task | Task plus directly relevant files | Risk-scaled evidence; close gates only when moving to `done` |
| `closeout` | Move work to review/done | Task evidence/docs/verification sections | Docs resolution, independent verification, `validate` |

If the user asks for a quick tracking note, stay in `bookkeeping`. Do not run
the full closeout sequence.

## Default Commands

```bash
tusker list --type epic
tusker search "<term>" --type task
tusker list --epic <ACR> --type task --open
tusker show <ID> --capsule
tusker new task --epic <ACR> --title "<work>" --kind chore --size s --risk low --priority p2 --domains <domain>
tusker status <ID> active
tusker evidence <ID> pr <url>
tusker docs check <ID>
tusker status <ID> review
tusker verify <ID> --by <verifier>
tusker close <ID> --by <reviewer>
tusker validate
```

If `WORKFLOW.md` enables `reviewer`, `review` can dispatch an independent reviewer lane. Codex is the default live runner today, but the policy is runner-neutral: `reviewer.runner` selects an enabled runner, `reviewer.actor` records attribution, low/medium risks may auto-close, and high/critical risks stay human-gated.

## Non-Negotiables

- Use `task` as the execution unit. A bug is `task(kind: bug)`.
- Every task belongs to an epic.
- `domains` are broad areas; `doc_nodes` are exact docs targets from `_config/docs-map.yaml`.
- Close only after evidence, docs impact resolution, verification, and validation.
- Do not turn backlog/bookkeeping updates into closeout ceremony.
- Use `tusker compact <ID>` before reading or editing an old noisy task; it is a
  dry-run unless `--write` is provided.
- Do not let the implementation worker self-certify. The configured reviewer actor is independent and may close only risks listed in `reviewer.auto_close_risks`.
- Do not edit generated files in `_system/generated/**` or `site/src/content/docs/**`.
- Documentation tickets are not permission to dump source text. Before writing docs, choose the audience, Diátaxis mode, and source authority.
- Human docs are synthesized outputs. Do not publish task records, evidence logs, D-note bodies, generated manifests, or agent-only instructions as user/developer prose.
- Never read `Attachments/**`, `_system/generated/**`, build logs, or raw runner
  logs by default. Use `tusker search`, `tusker list`, and exact task paths first.
- When command output may be large, redirect full output to a file and read only
  the failure summary or a small tail. Raw logs are evidence artifacts, not model
  context.

## Load References Only When Needed

| Need | Read |
|---|---|
| Decide whether this skill applies | `references/TRIGGERS.md` |
| Log, resume, or close routine work | `references/QUICK_MODE.md` |
| Command syntax | `references/COMMANDS.md` |
| Frontmatter, enums, sections | `references/SCHEMA.md` |
| Medium/high/critical task intake | `references/FORMAL_INTAKE.md` |
| Risk, evidence, verification bar | `references/RISK_AND_EVIDENCE.md` |
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

## Docs Work

For docs requests, start with:

```bash
tusker docs model
tusker docs map
tusker docs catalog
tusker docs freshness --stale
```

Then read `references/DOCS_PUBLICATION.md` before drafting or editing prose.

Authoring contract for any docs page:

1. Pick one audience: `user`, `developer`, `agent`, or `internal`.
2. Pick one primary Diátaxis mode: `tutorial`, `how-to`, `reference`, or `explanation`.
3. Use source-of-truth material as input, not final prose.
4. Put exact canon and metadata in an agent/internal lane unless a human reader directly needs it.
5. Reject pages that mix audiences, mix modes, or fail to answer the reader's first-screen intent.

Resolve each targeted doc node with one of:

```bash
tusker docs apply <ID> --node <DOC-NODE> --reason "<what changed>"
tusker docs noop <ID> --node <DOC-NODE> --reason "<why already current>"
tusker docs waive <ID> <DOC-NODE> --reason "<why no change>"
```

## Templates And Assets

Use `tusker new ...` when possible; it writes the right templates.

When manual installation or repair is required, read `references/RESOURCES.md` before copying anything from `assets/`.

## Done Means

- Work is in the right epic.
- Required sections have substance.
- Evidence matches risk.
- Docs impact is applied, noop, or waived with a reason.
- Verification is recorded.
- The close record shows who verified and who closed, including `agent-reviewer` or the configured reviewer actor when the reviewer lane closed it.
- `tusker validate` passes.
