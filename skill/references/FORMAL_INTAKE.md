# Formal intake

Creating a story at `risk ≥ medium`. This is the ceremony path. Load when the work is a feature, migration, refactor with teeth, or anything with rollout or rollback concerns.

## When this applies

- Features that ship to users
- Refactors that touch shared infrastructure
- Migrations (data, schema, API contract)
- Security-sensitive changes (secrets, auth, permissions)
- Anything with a rollback plan or feature flag
- Cross-team coordination

For anything else, use `QUICK_MODE.md`.

## Required frontmatter at intake

Beyond the quick-mode defaults, you must set:

- `size`: `s|m|l|xl` — effort estimate (agent-sessions, not days)
- `risk`: `medium|high|critical` — ceremony level
- `priority`: `p0|p1|p2|p3|icebox`
- `delegation`: `execute|explore|escalate`
- `surfaces`: which layers are touched (`frontend,api,runtime,desktop`)
- `change_type`: `feature|refactor|migration|security|docs|chore|research|incident`
- `ai_assistance`: `none|light|moderate|heavy`
- `ai_tools`: `[claude-code, codex, cursor, ...]`
- `assignee`: agent or human name (optional but preferred)

If any of these are missing at `status: active`, the validator will block the transition.

## Required sections by risk

| Section | medium | high | critical |
|---|---|---|---|
| `## Problem` | ✓ | ✓ | ✓ |
| `## Acceptance criteria` | ✓ | ✓ | ✓ |
| `## Canon` (spec/RFC references) | ✓ | ✓ | ✓ |
| `## Code anchors` | ✓ | ✓ | ✓ |
| `## Plan` | ✓ | ✓ | ✓ |
| `## Considered and rejected` | | ✓ | ✓ |
| `## Decision` | | ✓ | ✓ |
| `## Verification plan` | ✓ | ✓ | ✓ |
| `## Evidence` | ✓ | ✓ | ✓ |
| `## Rollout` | | ✓ | ✓ |
| `## Kill list` | | | ✓ |
| `## Work log` | ✓ | ✓ | ✓ |
| `## Agent handoff` (below the `---`) | ✓ | ✓ | ✓ |

Substance is checked, not presence. A section with `TODO` or empty body fails validation.

## What each section is for

- **Problem** — what is broken/missing/unclear. Who needs it fixed. *Not* the plan.
- **Acceptance criteria** — testable outcomes. Written as checklist. Must be verifiable.
- **Canon** — links to the epic canon, canonical D-note, or repo spec + section numbers. *Never* copy-paste spec prose.
- **Code anchors** — file paths and (when helpful) line ranges that the agent should read first.
- **Plan** — ordered approach, not the spec. Reads like a PR description.
- **Considered and rejected** — alternatives you weighed, one-line reason for each rejection. Forces the decision to be real.
- **Decision** — the chosen path and the trade-off you accepted.
- **Verification plan** — tests, manual steps, benchmarks that prove the acceptance criteria.
- **Evidence** — filled *after* execution. See `RISK_AND_EVIDENCE.md`.
- **Rollout** — feature flag name, staged rollout plan, rollback plan.
- **Kill list** — what old code/behavior gets deleted when this ships.
- **Work log** — bullet per meaningful step, `<date> — <author> — <what>`.

## Create-and-populate flow

```bash
tusker new-story --epic <ACR> --title "<title>" \
  --size <s|m|l|xl> --risk <medium|high|critical> \
  --change-type <type> --priority <p0|p1|p2|p3> \
  --delegation <execute|explore|escalate> \
  --surfaces "<csv>" \
  --ai-assistance heavy --ai-tools codex
```

Then open the generated file and fill the sections above. The scaffold includes section stubs — replace every stub, delete nothing.

## Dependencies between stories

Use wikilinks in frontmatter:

```yaml
blocks:
  - "[[ACR-S-0002]]"
blocked_by:
  - "[[ACR-S-0001]]"
```

Dependency graphs are rendered in Bases views and checked by `tusker validate`.

## Delegation

- `execute` — outcome and path known. Agent implements end-to-end.
- `explore` — outcome known, path unclear. Agent spikes, writes up `Considered and rejected`, stops at `in_review` without merged implementation.
- `escalate` — architecture or product questions. Agent analyzes, fills `## Decision-needed`, stops at `active`.

If you're not sure: prefer `execute` and stop yourself mid-work if you hit a genuine unknown. That's more honest than pre-emptively marking everything `explore`.

## When the spec is upstream

If the work implements an existing RFC:

- `## Canon` cites the D-note with section numbers: `[[PLC-D-0001]] §§6, 16`
- `## Code anchors` points to the files the RFC implementations will modify
- `## Plan` is the implementation order, not a restatement of the RFC

If the canon does not yet exist, see `CANON_LOCATIONS.md` — you may need to author it first.

## Story decomposition

For large RFCs that require multiple stories, see `STORY_DECOMPOSITION.md`. A story titled "implement the RFC" is a decomposition failure.
