# Tusker v5 final spec

## Product thesis

Tusker turns repo-local Markdown into agent work contracts and keeps product knowledge fresh.

```text
intent → task contract → agent run → evidence → verification → knowledge delta → docs freshness → close
```

It is not Markdown Jira. It is a small operating system for agentic software work.

## Core entities

```text
Epic       = workstream, scope, canon map, success definition
Task       = executable work contract
Doc page   = durable knowledge artifact
Doc node   = exact routing key for a documentation target
Evidence   = proof attached to a task
Run        = one agent attempt
Event      = append-only audit entry
Media      = transcript-backed video/screenshot/demo/promo asset
Eval       = task-success test for docs, agents, references, media, or promo
```

## Layout

```text
tusker/
├── Dashboard.md
├── epics/
│   └── MEM/
│       ├── MEM.md
│       ├── MEM-T-0001.md
│       └── MEM-T-0002.md
├── docs/
│   ├── start/
│   ├── guides/
│   ├── reference/
│   ├── concepts/
│   ├── troubleshooting/
│   ├── agents/
│   └── media/
├── media/
├── evals/
├── _config/
│   ├── docs-map.yaml
│   ├── media-map.yaml
│   ├── risk.yaml
│   └── validator-rules.yaml
└── _system/
    ├── events/
    ├── runs/
    └── generated/
```

## Work hierarchy

```text
Vault/repo
└── Epic
    └── Task
        ├── kind: feature
        ├── kind: bug
        ├── kind: docs
        ├── kind: migration
        ├── kind: research
        ├── kind: security
        └── kind: chore
```

No story layer. No project layer. No subtask layer. Agent subtasks live in `## Workpad` or the agent's private todo list.

## Task frontmatter

Default:

```yaml
---
schema: tusker.task/v5
id: MEM-T-0001
title: Add project memory fallback
type: task
kind: feature
epic: MEM
status: ready
priority: p2
risk: medium
domains:
  - memory
doc_nodes:
  - memory/runtime
created: 2026-04-29
updated: 2026-04-29
---
```

Optional, omitted unless used:

```yaml
owner: sarav
size: m
autonomy: implement_no_merge
depends_on:
  - MEM-T-0007
blocks:
  - MEM-T-0012
surfaces:
  - runtime
  - docs
```

Do not include empty placeholders.

## Status model

```text
intake → ready → active → review → done
         ↓       ↓
       blocked  cancelled
```

`rework` is not a status. Rework is an active task with a `changes_requested` review event.

Current status stays canonical in frontmatter because Tusker must work in Obsidian without the daemon.

## Events/runs/generated split

```text
Task frontmatter        = current readable state
_system/events/*.jsonl  = append-only audit history
_system/runs/<id>/*.json = agent attempt runtime/session details
_system/generated/*.json = rebuildable cache/indexes
```

Do not create per-task sidecar JSON that mirrors Markdown.

## Task body contract

Required shape for medium+ risk:

```text
Outcome
Acceptance contract
Scope
Context and canon
Constraints and stop conditions
Verification contract
Review focus
---
Agent packet
  Workpad
  Evidence packet
  Knowledge delta
  Verification log
  Work log
```

Acceptance criteria are truths. Deliverables are proof. Verification is how truth is checked. Docs impact is routed through doc_nodes.

## Knowledge delta

Structured deltas are mandatory when doc_nodes are present and for high+ risk tasks.

```markdown
| Change type | Topic | Before | After | Audience | Target doc nodes | Mode impact | Status |
|---|---|---|---|---|---|---|---|
| changed | memory/runtime | Global memory preceded project memory. | Project memory precedes global memory. | user, developer, agent | memory/overview, memory/runtime | explanation, reference | pending |
```

Reject tautologies like `Updated implementation`.

## Documentation model

Use controlled routing:

```yaml
domains:
  - memory
  - harness

doc_nodes:
  - memory/runtime
  - harness/evidence
```

`domains` are broad buckets. `doc_nodes` are exact targets defined in `_config/docs-map.yaml`.

No `docs: check|update|new|deprecate` field. If `doc_nodes` is non-empty, run docs impact. If no update is needed, record a waiver.

## Diátaxis

Diátaxis belongs to durable docs and docs-map nodes:

```yaml
mode: tutorial | how-to | reference | explanation
audience: user | developer | operator | support | agent | internal
agent_layer: none | capsule | standalone
```

Public navigation should use reader language:

```text
Start here
Guides
Reference
Concepts
Troubleshooting
Examples
For agents
```

## Agent docs

The `For agents` section is real infrastructure:

```text
agents/
├── quickstart/
├── runbooks/
├── contracts/
├── permissions.md
├── manual-intervention.md
├── rollback.md
└── evals/
```

Agent how-to/runbook pages require:

```text
goal
inputs
preconditions
steps
validation
failure modes
rollback
escalate when
manual intervention points
```

## Markdoc boundary

Canonical source remains plain Markdown + frontmatter.

Markdoc is optional publish/render infrastructure only.

Allowed first:
- Node customization that keeps source entirely Markdown.

Restricted tags, published human docs only:
- callout
- tabs
- figure
- video
- badge

Forbidden:
- task contracts
- AGENTS.md
- WORKFLOW.md
- agent runbooks
- llms.txt
- canonical internal docs

Every component must export clean plain Markdown for agents.

## Media

Video and screenshots are first-class artifacts, but never source of truth.

Every important video must have:
- transcript
- chapters
- step list
- commands/inputs
- expected outputs
- manual intervention points
- source task
- doc_nodes
- claims
- alt text

Promo can derive from evidence. Promo cannot become source of truth.

## Docs evals

Docs quality is task success.

Eval kinds:
- retrieval
- execution
- reference
- staleness
- media
- promo

## Closure rule

A task closes only when:
- acceptance contract is satisfied
- evidence packet is complete
- verification log is resolved
- docs impact is updated or waived
- risk-tier requirements are satisfied
