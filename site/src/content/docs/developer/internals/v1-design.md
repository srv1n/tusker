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
  updated: "2026-04-22"
---

# Tusker V1 — design index

The v1 design is spread across the skill entry point and the `references/` folder so agents only load what's relevant. This file is an index.

## Entry points

- **`SKILL.md`** — lean skill contract. Trigger words, three hard rules, quick-mode commands, routing table. Every agent invocation starts here.
- **`README.md`** — repo README for humans. What Tusker is, how to install, how to point it at a vault.

## Operating modes

Tusker has two ceremony modes; agents pick based on the work:

- **Quick mode** (`references/QUICK_MODE.md`) — the 90% case. Log, discover, close with defaults. `risk: low`, one-line evidence, self-attestation.
- **Formal intake** (`references/FORMAL_INTAKE.md`) — the ceremony path. `risk ≥ medium`: full frontmatter, considered-and-rejected, decision, rollout, human attestation at `high`/`critical`.

## The model

```
Vault = one product/repo
  └── Epic (3-char acronym, e.g. MEM)
        ├── Story  MEM-S-NNNN  (work item: feature, refactor, migration, docs, chore, research)
        ├── Bug    MEM-B-NNNN  (defect)
        └── Doc    MEM-D-NNNN  (standalone doc: RFC, user guide, release notes)
```

No project layer. No task layer. Sub-work inside a story is agent-managed (your internal todos). If work doesn't fit one session, split into multiple stories — see `references/STORY_DECOMPOSITION.md`.

## Where to find what

| Topic | File |
|---|---|
| Frontmatter fields, enums, linking | `references/SCHEMA.md` |
| Story lifecycle, status gates | `references/WORKFLOW.md` |
| Risk tiers, section requirements, evidence, attestation | `references/RISK_AND_EVIDENCE.md` |
| When to invoke Tusker at all | `references/TRIGGERS.md` |
| Quick-capture workflow | `references/QUICK_MODE.md` |
| Full ceremony workflow | `references/FORMAL_INTAKE.md` |
| Choosing canon location for an epic | `references/CANON_LOCATIONS.md` |
| Breaking a large spec into stories | `references/STORY_DECOMPOSITION.md` |
| Full CLI reference | `references/COMMANDS.md` |
| Bases views (Obsidian) | `references/BASES.md` |
| Obsidian community plugin compatibility | `references/PLUGIN_COMPAT.md` |
| Optional plugins worth installing | `references/OPTIONAL_PLUGINS.md` |
| Install prerequisites (Go, Obsidian, sync) | `references/PREREQUISITES.md` |
| Repo contract (AGENTS.md, .gitignore, hooks) | `references/REPO_CONTRACT.md` |
| Dispatcher + cron-driven agent loop | `docs/DISPATCHER_PSEUDOCODE.md` |
| Failure classes and retry policy | `docs/FAILURE_CLASSES.md` |
| Safe manual overrides | `docs/OPERATOR_INTERVENTION.md` |

## Design principles (the why)

**Markdown is the source of truth.** Frontmatter is machine layer, body is human layer. No database. Generated JSON (`_system/generated/*.json`) is a cache, not canon.

**Risk drives ceremony, not size.** A typo fix at `risk: low` needs one line of evidence; a one-liner that flips a prod flag at `risk: high` needs a rollout plan. Blast radius, not LOC.

**Agents act; humans gate.** Agents create, execute, attach evidence, request attestation. Humans sign off on `risk ≥ high`. The validator enforces the boundary.

**Evidence is artifacts, not plans.** `## Evidence` is filled after execution with links, test output, and demo assets. Plans live in `## Plan` and `## Verification plan`.

**The vault is the operating surface AND the spec archive.** Canon may live in epic `## Design`, a canonical D-note, or a repo `spec_source` file — see `references/CANON_LOCATIONS.md`. Stories cite canon with links, not copy-paste.

**Progressive disclosure.** `SKILL.md` is short. Agents load a reference file only when the current task requires it.

## Shipping as a binary

`go build -o dist/tusker ./cmd/tusker` produces `dist/tusker`, a self-contained executable with all templates, bases, snippets, and repo-contract files embedded in the binary. Drop it on `$PATH` and the dispatcher (or any cron caller) can shell out to it with no source checkout or JS runtime installed.

The live vault is the source of truth for policy. The PID table at `_system/logs/runs.json` is a process-liveness cache. Two dispatchers on the same vault: enforce single-writer via `_system/logs/dispatcher.lock`. iCloud sync latency is not a correctness issue — every CLI call reads fresh state, and `pickup` is atomic on the local filesystem.
