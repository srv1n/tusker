# Tusker Final Spec v4

## 0. Thesis

Tusker should be a **repo-native, Markdown-first, agent-oriented work contract system**.

The unit of work is a **task contract**.
The unit of human review is an **evidence packet**.
The unit of knowledge maintenance is a **docs update driven by knowledge delta**.

This is not a Jira clone.
This is not a generic PM system.
This is a harness for humans steering and agents executing.

---

## 1. Final decisions

### 1.1 Rename `story` -> `task`

Do it everywhere.

- CLI: `new-task`
- IDs: `MEM-T-0001`
- views: `Tasks.base`, not `Stories.base`
- docs: stop using “story” in the product language

Reason: `task` is the correct execution unit for agents.

### 1.2 `bug` is **not** a top-level type

Use:

```yaml
 type: task
 kind: bug
```

Keep a `bug.md` template **only as an ergonomic entrypoint**.

That means:

- same lifecycle as all tasks
- same validator pipeline
- different body shape for repro/regression evidence

### 1.3 Drop `Doc` as a ticket type

This is the largest change.

Docs should be **pages**, not tickets.

Use:

- `task(kind: docs)` for documentation work
- `docs/` pages for durable knowledge
- `doc_nodes` to target exact pages

Do **not** create `MEM-D-0001` style tracker tickets as the default representation of docs.
That makes docs feel like backlog artifacts instead of durable knowledge.

### 1.4 Keep current lifecycle state in task frontmatter

Canonical task state stays in the Markdown file:

```yaml
status: draft | ready | active | blocked | review | rework | done | cancelled
```

Do **not** derive current state exclusively from an append-only event log.

Reason:

- Tusker is intentionally Obsidian-first and Markdown-editable.
- The daemon is optional.
- Humans need to be able to inspect and change state directly in the same file.
- Full event-sourced truth is elegant, but it is the wrong complexity budget for a small, Markdown-first system.

### 1.5 No per-task sidecars

Reject:

```text
.tusker/state/<TASK-ID>.json
```

Use instead:

```text
_system/
├── generated/   # caches, indexes, dashboards
├── runs/        # daemon/runtime attempt state
└── events/      # append-only audit log (optional but recommended)
```

Important distinction:

- `frontmatter` = current contract + current task state
- `_system/events` = audit trail for CLI/daemon actions
- `_system/runs` = volatile/runtime machine state
- `_system/generated` = derived caches

### 1.6 Replace freeform tags as the contract with controlled routing

Use two layers only:

```yaml
domains:
  - memory
  - harness

doc_nodes:
  - memory/overview
  - memory/retrieval/pipeline
```

Rules:

- `domains` = broad human grouping and filtering
- `doc_nodes` = exact documentation targets for automation
- unknown domain/node = validator failure
- optional personal `tags` may exist, but core logic ignores them

### 1.7 Docs are a close gate

A task is not done until one of these is true:

- docs impact is resolved and relevant pages are updated/verified
- docs impact is explicitly waived with a reason
- task is genuinely docs-neutral (`docs: none` and `knowledge delta: none`)

### 1.8 Add `knowledge delta`

This is mandatory for any task that changes durable understanding.

`knowledge delta` is the bridge between implementation and documentation.
It tells the docs hook what actually changed in the product or system model.

### 1.9 Separate acceptance, deliverables, verification, and evidence

These are different things:

- **acceptance** = what must be true
- **deliverables** = what must be attached/shown
- **verification** = how truth is checked
- **evidence** = actual attached proof after execution

### 1.10 Risk tiers decide ceremony

Do not force every task through the same bloated template.
Keep one schema, but let the validator decide which sections are required by risk.

---

## 2. Core model

```text
Vault = one repo

Repo
└── tusker/
    ├── epics/
    │   └── MEM/
    │       ├── MEM.md
    │       ├── MEM-T-0001.md
    │       └── MEM-T-0002.md
    ├── docs/
    │   ├── memory/overview.md
    │   └── memory/retrieval-pipeline.md
    ├── _config/
    │   ├── docs-map.yaml
    │   └── WORKFLOW.md
    └── _system/
        ├── generated/
        ├── runs/
        └── events/
```

Only two executable work objects exist:

```text
Epic
Task
```

Docs are pages, not tickets.

---

## 3. Frontmatter

### 3.1 Task frontmatter

Required:

```yaml
---
schema: tusker.task/v4
id: MEM-T-0001
title: Add retrieval cache invalidation
type: task
kind: feature
epic: MEM
status: ready
priority: p2
risk: medium
domains: [memory]
doc_nodes: [memory/overview, memory/retrieval/pipeline]
docs: check
created: 2026-04-29
updated: 2026-04-29
---
```

Optional:

```yaml
owner: sarav
size: m
surfaces: [api, worker]
depends_on: [MEM-T-0007]
autonomy: execute_no_merge
```

### 3.2 Task enums

```text
kind:
  feature | bug | migration | docs | research | incident | security | chore

status:
  draft | ready | active | blocked | review | rework | done | cancelled

risk:
  low | medium | high | critical

priority:
  p0 | p1 | p2 | p3

docs:
  none | check | update | new | deprecate
```

### 3.3 Epic frontmatter

```yaml
---
schema: tusker.epic/v4
id: MEM
title: Memory system
type: epic
status: active
owner: sarav
created: 2026-04-29
updated: 2026-04-29
---
```

No `canon_mode`.
No `spec_source` as a required metadata crutch.
The epic body is canon by default unless it points elsewhere.

---

## 4. Epic contract

Epic body structure:

```markdown
# MEM · Memory system

## Why

## Outcomes

## Scope
### In
### Out
### Non-goals

## Success metrics

## Canon
<!-- This section is canonical by default. Link out when needed. -->

## Linked docs
<!-- High-level human-readable pointers; generated list okay. -->

## Open questions
```

Epic lifecycle:

```text
Active = at least one non-terminal task exists
         OR success metrics not yet met
         OR linked docs are stale against latest completed task

Done   = no open tasks
         AND success metrics met
         AND linked docs verified through latest relevant task close
```

---

## 5. Task contract

### 5.1 Body shape

Use one Markdown file with a hard split:

```markdown
# MEM-T-0001 · Add retrieval cache invalidation

## Intent

## Scope
### In
### Out
### Non-goals

## Acceptance contract
| # | Outcome | Proof required | Docs impact |
|---|---|---|---|
| 1 | ... | ... | memory/overview |

## Canon
<!-- exact docs/paths to read first -->

## Code/system anchors
<!-- files, symbols, commands, endpoints -->

## Constraints
<!-- preserve API, no PII in logs, etc. -->

## Escalate if
<!-- when the agent must stop and ask -->

## Deliverables
<!-- demo video, screenshot, logs, doc patch, PR, etc. -->

## Verification plan
<!-- commands, manual steps, benchmark checks -->

## Knowledge delta
| Topic | Change | Audience | Target doc nodes |
|---|---|---|---|

---

## Execution plan
<!-- agent-authored / optionally human-edited -->

## Evidence
<!-- actual artifacts after execution -->

## Verification log
<!-- verifier outcomes -->

## Work log
<!-- chronological notes -->
```

### 5.2 Critical rules

- Above the horizontal rule is the **contract**.
- Below the horizontal rule is **execution output**.
- Do not split human and agent into separate task files.
- Do not let the task become a giant session diary.
- Long scratch output belongs in attachments, not in the main note.

### 5.3 Bug template shape

Same frontmatter schema, but bug-specific body headings are allowed:

```markdown
## Symptom
## Reproduction
## Expected vs observed
## Suspected cause
## Regression proof
```

Still a task.
Still same lifecycle.

---

## 6. Risk-tiered required sections

```text
low
  required:
    - Intent
    - Acceptance contract (>= 1 row)
    - Evidence before close

medium
  required:
    - Scope
    - Deliverables
    - Verification plan
    - docs != none => doc_nodes + docs impact filled

high
  required:
    - Canon
    - Code/system anchors
    - Constraints
    - Escalate if
    - Knowledge delta
    - Verification log
    - Rollout/rollback note if applicable

critical
  required:
    - everything above
    - explicit rollback plan
    - human signoff before done
    - no auto-close
```

The template may include all headings.
The validator decides what is mandatory.

---

## 7. Documentation system

### 7.1 Docs are pages, not tracker tickets

Put docs in a real docs tree.
Example:

```text
tusker/docs/
├── memory/
│   ├── overview.md
│   └── retrieval-pipeline.md
├── harness/
│   └── skills-and-agents.md
└── agents/
    └── memory-recipe.md
```

### 7.2 Docs map

Use one controlled catalog:

```yaml
schema: tusker.docs-map/v1

domains:
  memory:
    description: Product and runtime memory behavior
  harness:
    description: Agent harness and execution model

nodes:
  memory/overview:
    path: docs/memory/overview.md
    audience: user
    kind: concept
    role: guide
    domains: [memory]

  memory/retrieval/pipeline:
    path: docs/memory/retrieval-pipeline.md
    audience: developer
    kind: reference
    role: canon
    domains: [memory]

  agents/memory-recipe:
    path: docs/agents/memory-recipe.md
    audience: agent
    kind: recipe
    role: guide
    domains: [memory, harness]
```

Validator rules:

- unknown `domain` => fail
- unknown `doc_node` => fail
- duplicate node ID => fail
- duplicate path => fail

### 7.3 Docs freshness mechanism

Task closure flow:

```text
Task -> review
    -> validate acceptance/evidence
    -> if docs != none or doc_nodes present:
         run docs-impact agent
         using:
           - task contract
           - acceptance contract
           - knowledge delta
           - changed files / diff
           - target doc pages
         produce:
           - doc patch
           - or explicit no-op verification
           - or waiver request
    -> human reviews evidence/docs
    -> done
```

### 7.4 Close hook semantics

Be explicit:

- the close-time docs hook is an **agent invocation**, not a deterministic compiler step
- it costs tokens
- it is non-deterministic
- it should support `--dry-run`
- patches still go through normal review

### 7.5 Periodic sweeper

Run daily or in CI to detect drift:

- changed code surfaces with stale linked docs
- broken links/examples
- nodes with unresolved docs waivers
- docs pages whose last verification predates the last linked task close

---

## 8. `_system/` layout

```text
_system/
├── generated/
│   ├── tasks.index.json
│   ├── docs.index.json
│   ├── links.index.json
│   └── dashboard.json
├── runs/
│   └── MEM-T-0001/
│       └── attempt-001.json
└── events/
    └── 2026-04.jsonl
```

Semantics:

- `generated/` is disposable cache
- `runs/` is runtime attempt state
- `events/` is audit history for CLI/daemon actions

Not canonical:

- no current task state mirrored into per-task JSON
- no docs metadata hand-maintained in multiple places

Canonical sources are:

- task markdown
- epic markdown
- docs pages
- docs-map.yaml
- WORKFLOW.md

---

## 9. Workflow policy

Put workflow policy in `tusker/_config/WORKFLOW.md`.

It should define:

- allowed statuses
- eligibility to auto-pick work
- bounded concurrency
- retry policy
- handoff rules
- close-time docs behavior
- risk gates for merge/close

Minimal model:

```text
draft -> ready -> active -> review -> done
                   -> blocked -> active
review -> rework -> active
active/review -> cancelled
```

No separate `review_state` in the core schema.
If you need finer detail, derive it from evidence + verification log + events.

---

## 10. Attachments

Use per-task attachment folders.
This is good.
It is not the same thing as a state sidecar.

```text
Attachments/
└── MEM-T-0001/
    ├── demo.mp4
    ├── before.png
    ├── after.png
    ├── test-output.txt
    └── docs-dry-run.md
```

---

## 11. What goes in AGENTS.md vs task vs workflow

### AGENTS.md

Short table of contents only.

Contains:

- how to bootstrap/run/test
- where canon lives
- repo-wide engineering rules
- pointers to deeper docs

Does **not** contain:

- the whole knowledge base
- every product requirement
- every architecture rule inline

### Task

Contains:

- intent
- scope
- acceptance
- doc impact
- knowledge delta
- evidence

### WORKFLOW.md

Contains:

- process law
- statuses
- retry/merge/handoff policy
- daemon behavior

That keeps repository knowledge layered instead of bloated.

---

## 12. Migration from current Tusker

### 12.1 Rename and flatten the model

- `story.md` -> `task.md`
- `MEM-S-0001` -> `MEM-T-0001`
- `type: story` -> `type: task`
- `change_type` -> `kind`
- `bug` becomes `task(kind: bug)`

### 12.2 Remove frontmatter sludge

Delete from task frontmatter:

```text
record_id
epic_record_id
review_state
work_revision
ai_session_log
attested_*
signoff_*
verified_*
reviewed_*
dod_*
*_record_ids
transitions
prs
related
blocks
blocked_by
```

Some of these can survive in:

- `_system/events`
- `_system/generated`
- `## Evidence`
- `## Verification log`

### 12.3 Drop D-note ticketing

- stop generating `MEM-D-0001.md` by default
- move durable docs into `docs/` pages
- migrate doc publication metadata into `docs-map.yaml` if needed

### 12.4 Add docs map

Create:

```text
tusker/_config/docs-map.yaml
```

### 12.5 Add knowledge delta and verification log

Every medium+ task should gain:

```markdown
## Knowledge delta
## Verification log
```

### 12.6 Introduce audit/events without making them authoritative

CLI/daemon writes:

- status change events
- review events
- docs verification events
- run attempt metadata

But the Markdown task remains the current truth.

---

## 13. Strong opinions

### 13.1 What I am explicitly rejecting

- `story` as the main work unit
- a separate top-level `bug` type
- docs as tracker tickets by default
- per-task JSON sidecars
- freeform tags as automation routing
- one giant AGENTS.md encyclopedia
- uniform ceremony across all risk tiers
- pretending the docs close hook is deterministic
- copying task acceptance criteria directly into user docs without a knowledge-delta layer

### 13.2 What I am explicitly keeping

- repo-local markdown as source of truth
- Obsidian as the best reading/editing surface
- optional daemon/orchestrator
- evidence-first review
- risk-driven ceremony
- agents managing sub-work internally

---

## 14. Short version

```text
Epic = workstream + success metrics + canon
Task = executable contract + evidence + docs impact
Docs = pages, not tickets

Frontmatter = small and human-editable
No sidecars
No tag soup
Docs close gate is mandatory
Knowledge delta is the bridge
Risk decides ceremony
```

This is the spec I would build.
