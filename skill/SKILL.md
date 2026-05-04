---
name: tusker
description: "Track, plan, document, or close repo-local work in a markdown vault. Use for epics, tasks, bugs, docs, evidence, validation, backlogs, specs, docs sites, resume/status, or close requests."
---

# Tusker

Use the CLI first. Edit markdown directly only when the CLI cannot express the change.

## Core Loop

1. Find the vault. Omit `--vault` unless discovery fails.
2. Read `tusker/README.md` before creating work.
3. Pick the epic whose summary matches the request.
4. Create or update a task.
5. Record evidence, docs impact, verification, and close state.
6. Run `tusker validate` before saying done.

Tell the user the ID and the epic rationale after creating work.

## Default Commands

```bash
tusker list --type epic
tusker list --epic <ACR>
tusker new task --epic <ACR> --title "<work>" --kind chore --size s --risk low --priority p2 --domains <domain>
tusker status <ID> active
tusker evidence <ID> pr <url>
tusker docs check <ID>
tusker status <ID> review
tusker verify <ID> --by <verifier>
tusker close <ID> --by <reviewer>
tusker validate
```

## Non-Negotiables

- Use `task` as the execution unit. A bug is `task(kind: bug)`.
- Every task belongs to an epic.
- `domains` are broad areas; `doc_nodes` are exact docs targets from `_config/docs-map.yaml`.
- Close only after evidence, docs impact resolution, verification, and validation.
- Do not edit generated files in `_system/generated/**` or `site/src/content/docs/**`.

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

Then read `references/DOCS_PUBLICATION.md`.

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
- `tusker validate` passes.
