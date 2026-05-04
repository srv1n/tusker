---
title: "Formal intake"
description: "Creating a task at risk >= medium. Load this when the work is a feature, migration, refactor with teeth, public docs change, security-sensitive change, or anything with rollout/rollback concerns."
tusker:
  audience: "user"
  publish_path: "user/reference/formal-intake"
  publish_section_title: "Reference"
  route: "/user/reference/formal-intake/"
  source_kind: "repo_doc"
  source_path: "skill/references/FORMAL_INTAKE.md"
  summary: "Creating a task at risk >= medium. Load this when the work is a feature, migration, refactor with teeth, public docs change, security-sensitive change, or anything with rollout/rollback concerns."
  tags:
    - "reference"
  updated: "2026-04-30"
---

# Formal intake

Creating a task at `risk >= medium`. Load this when the work is a feature, migration, refactor with teeth, public docs change, security-sensitive change, or anything with rollout/rollback concerns.

## Required frontmatter

Beyond quick-mode defaults, set:

- `kind`: `feature|bug|refactor|migration|security|docs|chore|research|incident`
- `size`: `s|m|l|xl`
- `risk`: `medium|high|critical`
- `priority`: `p0|p1|p2|p3`
- `domains`: broad areas touched
- `doc_nodes`: exact docs targets when durable docs are affected
- `delegation`: `execute|explore|escalate`
- `ai_assistance`: `none|light|moderate|heavy`
- `ai_tools`: `[claude-code, codex, cursor, ...]`
- `assignee`: optional but preferred

If any required fields are missing at active work, validation should block the transition.

## Required sections by risk

| Section | medium | high | critical |
|---|---|---|---|
| `## Intent` | yes | yes | yes |
| `## Scope` | yes | yes | yes |
| `## Acceptance contract` | yes | yes | yes |
| `## Canon` | yes | yes | yes |
| `## Code/system anchors` | yes | yes | yes |
| `## Constraints` | yes | yes | yes |
| `## Escalate if` | yes | yes | yes |
| `## Deliverables` | yes | yes | yes |
| `## Verification plan` | yes | yes | yes |
| `## Knowledge delta` | when docs/understanding changes | yes | yes |
| `## Considered and rejected` | no | yes | yes |
| `## Decision` | no | yes | yes |
| `## Rollout` | no | yes | yes |
| `## Kill list` | no | no | yes |
| `## Evidence` | yes | yes | yes |
| `## Verification log` | yes | yes | yes |
| `## Work log` | yes | yes | yes |

Substance is checked, not presence. `TODO` is not a contract.

## What each section is for

- **Intent** — what needs to be true and who needs it.
- **Scope** — explicit in/out boundaries.
- **Acceptance contract** — testable outcomes.
- **Canon** — links to epic canon, V5 docs, or repo spec. Never copy-paste the spec.
- **Code/system anchors** — files, modules, commands, schemas, or docs nodes to inspect first.
- **Constraints** — things the agent must not break or change.
- **Escalate if** — stop conditions.
- **Deliverables** — concrete artifacts expected from the work.
- **Verification plan** — tests/manual checks/benchmarks before work starts.
- **Knowledge delta** — what durable understanding changed.
- **Evidence** — filled after execution.
- **Verification log** — what was actually checked.
- **Work log** — dated meaningful steps.

## Create-and-populate flow

```bash
tusker new task --epic <ACR> --title "<title>" \
  --kind <kind> --size <s|m|l|xl> --risk <medium|high|critical> \
  --priority <p0|p1|p2|p3> \
  --domains "<csv>" \
  --doc-nodes "<csv>" \
  --delegation <execute|explore|escalate> \
  --ai-assistance heavy --ai-tools codex
```

Then fill the generated sections. Replace stubs; do not leave placeholders.

## Dependencies between tasks

```yaml
blocks:
  - "[[ACR-T-0002]]"
blocked_by:
  - "[[ACR-T-0001]]"
```

## Delegation

- `execute` — outcome and path are known.
- `explore` — outcome known, path unclear; agent spikes and writes up tradeoffs.
- `escalate` — architecture or product question; agent analyzes and stops for decision.

Prefer `execute` unless there is a real unknown. Premature `explore` is often just procrastination wearing a lab coat.

## When the spec is upstream

If the work implements an existing RFC:

- `## Canon` cites exact sections.
- `## Code/system anchors` points to likely implementation files.
- `## Plan` or execution plan is implementation order, not a restatement of the RFC.
- `doc_nodes` names docs that must remain true after the change.

If canon does not exist, see `CANON_LOCATIONS.md` and create it first.

## Task decomposition

For large RFCs, see `TASK_DECOMPOSITION.md`. A task titled "implement the RFC" is a decomposition failure.
