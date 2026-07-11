---
schema: "tusker.project-skill/v7"
kind: "project_skill"
name: "project-knowledge"
project: "tusker"
capsule:
  skip_when:
    - "You only need task lifecycle, proof, gates, or closeout mechanics."
  use_when:
    - "Choosing project/domain context before implementation."
  what: "Repo project knowledge router for Tusker V7 domain canon."
status: "current"
description: "Route agents through this repository's V7 domain canon without publishing task proof or runtime state."
operator_skill: "tusker"
source_of_truth:
  - "knowledge/domains"
canonical_files:
  - "SKILL.md"
  - "knowledge/domains/*/INDEX.md"
  - "knowledge/domains/*/CANON.md"
created_at: "2026-07-05T17:58:21Z"
updated_at: "2026-07-07T10:40:35Z"
state_rev: "sha256:de58933ddce32e98bba70739b8a39af909d4dd5ca813def31f18e78893b9025f"
---

# Project Knowledge Skill

This is a generated V7 project knowledge skill. Use it after the Tusker operator skill when you need repository-specific context.

This is the project knowledge skill for this repository. Use it after the Tusker operator skill when repository-specific canon is needed.

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
| Repository-wide Tusker V7 canon, orchestration, skills, and validation policy. | `knowledge/domains/project/INDEX.md` | `knowledge/domains/project/CANON.md` | Project |

## Repo Command Policy

- Put repository-specific command rules here or in routed runbooks: validation commands, build-lock/status commands, token/noise wrappers, and forbidden expensive probes.
- Keep root `AGENTS.md` and `CLAUDE.md` as managed Tusker bootstrap pointers; do not copy Tusker workflow mechanics there.
- Agents should prefer path-scoped status/search, lock/status commands over process-table probes, redirected validation logs, and command + PASS/FAIL summaries.

## Serve/API Routing

- Run display is liveness-derived, never row-derived. `/api/runs` omits `unclaimed` zero-attempt placeholders by default; `?all=true` may expose them as `leaseState: "unclaimed"` with `outcome: "idle"`. `outcome: "running"` requires a held lease and a heartbeat fresher than the reclaim grace window; stale held leases are labeled `stale`.

## Prompt Signs

- `.tusker/signs.md` is the operator-maintained corrective prompt file injected into every dispatched attempt when present.
- Keep signs operational, terse, and durable: recurring mistakes, repo-specific traps, and validation habits belong there; task progress, proof logs, and transcripts do not.
- Keep the file near 60 lines or less. V7 validation warns when it bloats because every future attempt pays for it in context.

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
