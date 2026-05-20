---
schema: "tusker.project-skill/v7"
kind: "project_skill"
name: "project-knowledge"
project: "tusker"
status: "current"
description: "Route agents through this repository's V7 domain canon without publishing task proof or runtime state."
operator_skill: "tusker"
source_of_truth:
  - "knowledge/domains"
canonical_files:
  - "SKILL.md"
  - "knowledge/domains/*/INDEX.md"
  - "knowledge/domains/*/CANON.md"
created_at: "2026-05-19T05:19:14Z"
updated_at: "2026-05-19T05:19:14Z"
state_rev: "sha256:a2d3f3983e5c1b632b53f1bc2a1261baf4cfe7e6dddd76d3fbbe901ffe4e0319"
---

# Project Knowledge Skill

This is a generated V7 project knowledge skill. Use it after the Tusker operator skill when you need repository-specific context.

## Read This When

- You need durable repository-specific canon before implementing a task.
- A task packet routes you to one or more project domains.
- You are updating project knowledge after behavior, policy, or interfaces changed.

## Do Not Read This When

- You only need Tusker task lifecycle, proof, gates, closeout, or CLI semantics; use the Tusker operator skill.
- You are looking for raw proof logs, task history, attempts, events, generated packets, or local runtime state.

## First Action

Task agents must run `tusker packet <TASK-ID> --for agent`, then read only the routed domains from that packet unless the task contract names a narrower file.

## Routing Algorithm

1. Read this `SKILL.md`.
2. Use the task packet or intent to choose the narrowest matching domain.
3. Read that domain `INDEX.md`.
4. Read that domain `CANON.md`.
5. Open deeper runbooks, decisions, interfaces, invariants, sources, or glossary entries only when the domain files route you there.

## Domains

| Intent | Read first | Canon | Notes |
|---|---|---|---|
| Architecture and high-level system shape. | `knowledge/domains/architecture/INDEX.md` | `knowledge/domains/architecture/CANON.md` | architecture |
| Branch policy and protected state rules. | `knowledge/domains/branch-policy/INDEX.md` | `knowledge/domains/branch-policy/CANON.md` | branch policy |
| Build and fresh-clone baseline. | `knowledge/domains/build/INDEX.md` | `knowledge/domains/build/CANON.md` | build |
| Continuous integration checks and gates. | `knowledge/domains/ci/INDEX.md` | `knowledge/domains/ci/CANON.md` | ci |
| CLI commands and command behavior. | `knowledge/domains/cli/INDEX.md` | `knowledge/domains/cli/CANON.md` | cli |
| Close policy, acceptors, gates, and terminal states. | `knowledge/domains/close-policy/INDEX.md` | `knowledge/domains/close-policy/CANON.md` | close policy |
| Project and vault configuration. | `knowledge/domains/config/INDEX.md` | `knowledge/domains/config/CANON.md` | config |
| Generated dashboards and operator views. | `knowledge/domains/dashboards/INDEX.md` | `knowledge/domains/dashboards/CANON.md` | dashboards |
| Vault and repository discovery. | `knowledge/domains/discovery/INDEX.md` | `knowledge/domains/discovery/CANON.md` | discovery |
| Human documentation and publication projections. | `knowledge/domains/docs/INDEX.md` | `knowledge/domains/docs/CANON.md` | docs |
| Evidence records and proof hygiene. | `knowledge/domains/evidence/INDEX.md` | `knowledge/domains/evidence/CANON.md` | evidence |
| Human, reviewer, CI, and external gates. | `knowledge/domains/gates/INDEX.md` | `knowledge/domains/gates/CANON.md` | gates |
| Generated indexes, packets, and projections. | `knowledge/domains/generated/INDEX.md` | `knowledge/domains/generated/CANON.md` | generated |
| Git integration and state synchronization. | `knowledge/domains/git/INDEX.md` | `knowledge/domains/git/CANON.md` | git |
| Validation guardrails and safety checks. | `knowledge/domains/guardrails/INDEX.md` | `knowledge/domains/guardrails/CANON.md` | guardrails |
| V7 project knowledge and domain canon. | `knowledge/domains/knowledge/INDEX.md` | `knowledge/domains/knowledge/CANON.md` | knowledge |
| Legacy command and migration boundaries. | `knowledge/domains/legacy/INDEX.md` | `knowledge/domains/legacy/CANON.md` | legacy |
| Obsidian vault layout and editing surfaces. | `knowledge/domains/obsidian/INDEX.md` | `knowledge/domains/obsidian/CANON.md` | obsidian |
| Skill, archive, and release packaging. | `knowledge/domains/packaging/INDEX.md` | `knowledge/domains/packaging/CANON.md` | packaging |
| Project knowledge policy and source-truth boundaries. | `knowledge/domains/policy/INDEX.md` | `knowledge/domains/policy/CANON.md` | policy |
| Project identity and repository overview. | `knowledge/domains/project/INDEX.md` | `knowledge/domains/project/CANON.md` | project |
| Derived projections and recomputed state. | `knowledge/domains/projection/INDEX.md` | `knowledge/domains/projection/CANON.md` | projection |
| Daemon, leases, attempts, sessions, and runtime state. | `knowledge/domains/runtime/INDEX.md` | `knowledge/domains/runtime/CANON.md` | runtime |
| V7 schemas and frontmatter contracts. | `knowledge/domains/schema/INDEX.md` | `knowledge/domains/schema/CANON.md` | schema |
| Secret handling and distribution safety. | `knowledge/domains/security/INDEX.md` | `knowledge/domains/security/CANON.md` | security |
| Operator and project skill surfaces. | `knowledge/domains/skill/INDEX.md` | `knowledge/domains/skill/CANON.md` | skill |
| Validation rules and doctor checks. | `knowledge/domains/validation/INDEX.md` | `knowledge/domains/validation/CANON.md` | validation |
| Task lifecycle and review workflow. | `knowledge/domains/workflow/INDEX.md` | `knowledge/domains/workflow/CANON.md` | workflow |

## Updating Canon

- Update the narrowest owning domain `CANON.md` when durable truth changes.
- Create or update a leaf node only when the canon needs a stable runbook, interface, invariant, decision, glossary entry, or source attribution.
- Run `tusker validate --json` after changing project knowledge.
- Do not put proof logs, task history, attempts, event streams, generated packets, or raw terminal output in canon.

## Forbidden Source Truth

- Do not publish task records, evidence logs, attempts, event files, generated output, runtime state, or raw logs as project skill source.
- Forbidden paths include `work/**`, `epics/**`, `evidence/**`, `attempts/**`, `events/**`, `_generated/**`, `_system/**`, `dashboards/**`, packet caches, `.tusker-*`, raw logs, and local absolute paths.
- Raw external input belongs in `knowledge/domains/<domain>/sources/`.
- Root `docs/` may contain optional repository engineering guardrails; it is not the V7 canonical knowledge source.

## Validation

- `tusker skill doctor --strict --json` checks project skill routes and package hygiene.
- `tusker validate --json` checks V7 domain layout and task-domain coverage.
