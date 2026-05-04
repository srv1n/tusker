---
title: "Tusker V1 Design Index"
description: "Historical design notes covering the Tusker v1 model and operating assumptions."
tusker:
  audience: "developer"
  canonical_status: "historical"
  deprecated: true
  publish_path: "developer/internals/v1-design"
  route: "/developer/internals/v1-design/"
  source_kind: "repo_doc"
  source_path: "skill/V1_DESIGN.md"
  summary: "Historical design notes covering the Tusker v1 model and operating assumptions."
  superseded_by: "/developer/specs/"
  tags:
    - "internals"
    - "design"
  updated: "2026-04-29"
---

# Tusker V5 Design Index

The V5 design is spread across the skill entry point and the `references/` folder so agents only load what they need.

## Entry Points

- `SKILL.md` - lean skill contract, triggers, routing table, and close flow.
- `README.md` - repo README for humans.
- `references/COMMANDS.md` - the small public CLI surface.

## Model

```text
Vault = one product/repo
  └── Epic  MEM         workstream boundary and canon
        ├── Task       MEM-T-NNNN executable change contract
        ├── Bug task   MEM-T-NNNN with kind: bug
        └── Doc page   tusker/docs/<node>.md durable knowledge
```

The current work item is a task. Bugs are tasks. Docs are durable pages, not executable work.

## Where To Find What

| Topic | File |
|---|---|
| Frontmatter fields, IDs, sections | `references/SCHEMA.md` |
| Task lifecycle and close gates | `references/WORKFLOW.md` |
| Risk, evidence, and verification | `references/RISK_AND_EVIDENCE.md` |
| When to invoke Tusker | `references/TRIGGERS.md` |
| Quick capture | `references/QUICK_MODE.md` |
| Formal task intake | `references/FORMAL_INTAKE.md` |
| Canon placement | `references/CANON_LOCATIONS.md` |
| Decomposing large specs into tasks | `references/TASK_DECOMPOSITION.md` |
| Obsidian Bases views | `references/BASES.md` |
| Docs publication | `references/DOCS_PUBLICATION.md` |

## Design Principles

**Markdown is the source of truth.** Frontmatter carries current task state; the body carries the human-readable contract and evidence.

**Tasks carry work. Docs carry durable knowledge.** A task can require docs through `doc_nodes`; close is blocked until those docs are applied or waived.

**Risk drives ceremony.** Low-risk work can stay light. High and critical work require a real knowledge delta and stronger verification.

**The public CLI stays small.** The supported surface is `init`, `new`, `list`, `status`, `evidence`, `docs`, `verify`, `close`, `validate`, `reindex`, and `update`.

**Runtime state is not tracker state.** Attempts, sessions, and event streams belong to runtime storage. Durable lifecycle truth stays in V5 markdown notes.
