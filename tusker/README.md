---
title: "Overview"
type: "note"
created: "2026-05-25"
updated: "2026-05-25"
tags: ["tusker-generated"]
---

# Project overview

<!-- tusker:overview:begin -->

Tusker is the repo-local, Obsidian-readable tracker for this project. The stable core is the V5 markdown task model, CLI, docs close gate, generated Bases views, and installable skill bundle. Operator runtime support is active for local use: task truth stays in markdown, while daemon leases, attempts, sessions, logs, and review packets stay in runtime state.

Current workflow policy as of 2026-05-08:

| Surface | Current status |
|---|---|
| Tracker schema | `tracker_schema_version: 5` |
| Runnable worker states | `active`, `rework` |
| Review checkpoint | `review` |
| Terminal success | `done` |
| Default worker runner | `codex` via `codex app-server` |
| Reviewer lane | enabled |
| Reviewer runner | `codex` today, configured through `reviewer.runner` |
| Reviewer actor | `agent-reviewer` |
| Auto-close risks | `low`, `medium` |
| Human-gated risks | `high`, `critical` |

The design is Codex-first, not Codex-only. Future Claude Code, OpenCode, or other runner adapters should plug into the same task statuses, runtime lanes, reviewer policy, and close gates.

<!-- tusker:overview:end -->

---

# Epic roster

_Auto-generated 2026-05-25T02:49:48Z. This top-level roster intentionally shows epics only. Run `tusker list --type epic` for the live terminal view, then drill into one epic with `tusker list --epic <ACR> --type task --open`._

Agents: use this page only to choose the right epic. Do not read every task file. Pick the epic whose summary best matches; if nothing fits and the work will outlive one task, propose a new epic with `tusker new epic --acronym <ACR> --title "<name>" --summary "..."`.

## Active

### [[ORC]] — Trustworthy Orchestration

**Summary:** Symphony-aligned daemon work: honest isolation, safe policy, continuation, evidence, and operator visibility.

**Counts:** 20 tasks, 0 bug tasks, 0 docs (open: 14, done: 6)

**Drill down:** `tusker list --epic ORC --type task --open`.

## Done

### [[DOC]] — Documentation system

**Summary:** Diátaxis docs-map, docs impact, agent docs, freshness, and AI-readable docs.

**Counts:** 9 tasks, 0 bug tasks, 0 docs (open: 0, done: 9)

**Drill down:** `tusker list --epic DOC --type task --open`.

### [[KNO]] — V6 Knowledge Graph

**Summary:** Hard-break Tusker V6: product knowledge graph, task proof layer, progressive-disclosure CLI, and filtered publication.

**Counts:** 7 tasks, 0 bug tasks, 0 docs (open: 0, done: 7)

**Drill down:** `tusker list --epic KNO --type task --open`.

### [[SKL]] — Skill distribution

**Summary:** Tight installable skill payload, metadata, references, templates, and distribution readiness.

**Counts:** 2 tasks, 0 bug tasks, 0 docs (open: 1, done: 1)

**Drill down:** `tusker list --epic SKL --type task --open`.

### [[VSK]] — V7 skill-shaped knowledge base

**Summary:** Make V7 initialization, validation, documentation, and skill packaging treat repo knowledge as a first-class agent skill while preserving the Tusker operator skill.

**Counts:** 10 tasks, 3 bug tasks, 0 docs (open: 0, done: 10)

**Drill down:** `tusker list --epic VSK --type task --open`.
