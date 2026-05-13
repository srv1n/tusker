---
schema: "tusker.doc/v5"
id: "spec/v6-rfc"
title: "Tusker V6 Product Knowledge Graph RFC"
type: "doc"
node: "spec/v6-rfc"
audience: "developer"
mode: "explanation"
agent_layer: "capsule"
kind: "canon"
domains:
  - "knowledge-system"
  - "schema"
  - "cli"
  - "skill"
source_of_truth:
  - "tusker/docs/spec/tusker_v6_rfc.md"
stale_when_paths:
  - "tusker/docs/spec/tusker_v6_rfc.md"
  - "cmd/tusker/**"
  - "skill/**"
  - "tusker/_config/docs-map.yaml"
canonical_status: "draft"
owner_epic: "KNO"
publish: true
publish_lane: "internal"
publish_path: "spec/tusker-v6-rfc"
publish_description: "Hard-break implementation RFC for Tusker V6 knowledge graph."
created: "2026-05-12"
updated: "2026-05-12"
---

# RFC: Tusker V6 Product Knowledge Graph

Status: Proposed
Date: 2026-05-12
Audience: Tusker maintainers, CLI implementers, agent-skill authors, and teams migrating existing Tusker vaults
Breaking changes: Yes
Supersedes: Tusker V5 docs/task integration model

---

## 1. One-line decision

Tusker V6 turns Tusker from a markdown task tracker with documentation support into a repo-local product knowledge graph where task tracking is the proof layer, domain knowledge is the current-truth layer, and the CLI enforces routing, freshness, validation, publication, and context control.

The shortest version:

```text
Tusker V6 = Product Knowledge Graph + Task Proof + Progressive Disclosure CLI
```

Not:

```text
Tusker V6 = better docs folder
Tusker V6 = markdown Jira
Tusker V6 = Obsidian wiki with tasks
```

---

## 2. Motivation

Tusker V5 already has the core instincts right:

- Each task is a markdown file.
- Tasks carry machine-readable frontmatter and human-readable work records.
- Tasks can name exact documentation nodes.
- Durable documentation is separate from execution records.
- The CLI gives agents bounded search, show, close, validation, and docs-impact commands.
- The operator skill teaches agents progressive disclosure.
- Generated site output is not source.
- `llms.txt` and similar outputs are generated retrieval surfaces.

The problem is vocabulary and vault grammar.

V5 still uses `docs` for too many meanings:

```text
docs = internal durable knowledge
docs = generated website content
docs = public documentation
docs = agent-readable canon
docs = publication pipeline
docs = close-gate target
```

That ambiguity is already leaking into the API: `tusker docs map`, `tusker docs freshness`, and `tusker docs export` are not the same kind of operation. Some operate on source truth. Some operate on publication projections.

V6 fixes this by splitting the model:

```text
knowledge = source truth
publish   = projection
```

It also fixes the human/agent navigation problem. A human should be able to open the vault in Obsidian and understand the current state. An agent should be able to route through the corpus without loading the whole damn thing.

The missing primitive is not “a better documentation site.” The missing primitive is a typed, indexed, progressively disclosed knowledge graph.

---

## 3. Goals

### 3.1 Make the skill-shaped corpus canonical

The repo-local Tusker vault is the source of truth. Rendered docs, `llms.txt`, packaged skills, and static websites are projections.

```text
                 tusker/
       canonical project knowledge graph
                         │
        ┌────────────────┼─────────────────┐
        v                v                 v
   human docs site   packaged skill     raw LLM surfaces
   site/**           skill zip          llms*.txt
```

### 3.2 Preserve progressive disclosure

Agents should not read everything. They should route:

```text
tusker/SKILL.md
    -> domain INDEX.md
        -> domain CANON.md
            -> specific reference / decision / troubleshooting / task proof
```

Every file must help answer:

```text
Should the agent read this file now?
```

### 3.3 Keep tasks and knowledge separate

Tasks are proof. Knowledge pages are current truth.

```text
Task     = what changed, why, and how it was proven
Knowledge = what is currently true
Epic     = temporary initiative boundary
Domain   = stable area of understanding
```

Do not merge these as prose. Connect them as graph nodes.

### 3.4 Make domains first-class

A domain is not a tag. A domain is a typed node with an index, canon, knowledge nodes, source files, and active work.

### 3.5 Make Obsidian navigation good without plugins

Use `[[wikilinks]]` in source files. Generate compact backrefs. Keep generated blocks small.

### 3.6 Make Go indexing cheap

Tusker does not need a custom Markdown parser. It needs:

- YAML frontmatter parsing.
- Lightweight heading-section extraction.
- Wikilink extraction.
- Markdown-link extraction.
- Managed-block replacement.
- A generated JSON index.

That is an indexer, not a renderer.

### 3.7 Make current truth difficult to rot

Every knowledge page names source-of-truth files and stale triggers. The CLI computes fingerprints and marks pages stale when source files change without a knowledge resolution event.

### 3.8 Make publication filtered

The default LLM surface must not include historical, deprecated, internal, or raw task evidence unless explicitly requested.

---

## 4. Non-goals

### 4.1 Do not build a custom Markdown renderer

The leverage is not in parsing Markdown into HTML. The leverage is in schemas, routing, validation, freshness, capsules, and graph edges.

### 4.2 Do not publish task records as docs

Task records can be inspected as proof. They are not user/developer docs by default.

### 4.3 Do not create one giant `SKILL.md`

V6 means one large skill-shaped corpus, not one giant root file.

### 4.4 Do not make MDX the canonical source format

Canonical knowledge files should be Markdown with YAML frontmatter. The renderer may synthesize MDX or HTML. MDX components in canonical files are context sludge and harder for Go to index.

### 4.5 Do not start with embeddings or semantic search

Start with deterministic routing over frontmatter, headings, IDs, aliases, domains, keywords, and read-when sections. Add embeddings later if deterministic routing is insufficient.

### 4.6 Do not use MCP to solve static knowledge structure

MCP is for dynamic context and tools: live state, logs, databases, accounts, schemas, environment diagnostics. Static product knowledge belongs in the repo-local corpus.

---

## 5. Current V5 model, restated

V5 effectively has this:

```text
repo/
├── AGENTS.md
├── skill/                         # Tusker operator skill source
├── tusker/
│   ├── docs/                      # durable docs
│   ├── epics/                     # epics and tasks
│   ├── _config/docs-map.yaml      # authored docs registry
│   ├── _system/generated/         # generated caches
│   ├── Docs.md                    # generated catalog
│   └── WORKFLOW.md
└── site/                          # generated docs website
```

V5 concepts:

```text
Epic = workstream boundary + canon + success metrics
Task = executable change contract
Doc  = durable knowledge page under tusker/docs
```

V6 keeps the good part and renames the wrong part:

```text
Epic      = initiative / workstream boundary
Task      = executable change contract + proof
Domain    = stable area of understanding
Knowledge = current durable truth
Publish   = projection / rendering / packaging
```

---

## 6. V6 architecture

### 6.1 Consumer repo layout

Tusker is used inside many repositories. Therefore the V6 layout must be specified from the perspective of a consumer repo, not only Tusker’s own repo.

```text
repo/
├── AGENTS.md                       # minimal agent bootstrap; points into tusker/SKILL.md
├── CLAUDE.md                       # optional equivalent bootstrap
├── tusker/
│   ├── SKILL.md                    # project knowledge router; per repo
│   ├── README.md                   # human vault overview
│   ├── WORKFLOW.md                 # lifecycle and gate policy
│   ├── domains/
│   │   ├── codebase/
│   │   │   ├── INDEX.md
│   │   │   ├── CANON.md
│   │   │   ├── repo-map.md
│   │   │   ├── architecture.md
│   │   │   ├── conventions.md
│   │   │   ├── testing.md
│   │   │   ├── local-dev.md
│   │   │   ├── safe-change-rules.md
│   │   │   └── known-footguns.md
│   │   ├── product/
│   │   │   ├── INDEX.md
│   │   │   ├── CANON.md
│   │   │   ├── glossary.md
│   │   │   ├── architecture.md
│   │   │   ├── decisions/
│   │   │   ├── reference/
│   │   │   ├── how-to/
│   │   │   ├── troubleshooting/
│   │   │   └── assets/
│   │   └── <project-domain>/
│   │       ├── INDEX.md
│   │       ├── CANON.md
│   │       └── ...
│   ├── epics/
│   │   └── <ACR>/
│   │       ├── <ACR>.md
│   │       └── <ACR>-T-0001.md
│   ├── _config/
│   │   └── knowledge-policy.yaml   # policy only, no authored per-node registry
│   ├── _system/
│   │   └── generated/
│   │       ├── knowledge-map.json
│   │       ├── route-index.json
│   │       ├── graph.index.json
│   │       ├── backlinks.index.json
│   │       ├── freshness.index.json
│   │       ├── publication.index.json
│   │       └── capsules/
│   └── Attachments/
└── site/                           # optional generated renderer output
```

### 6.2 Tusker’s own repo layout

Tusker’s repository has two roles:

1. It is the source repository for the Tusker CLI and operator skill.
2. It is also a consumer repo dogfooding the Tusker V6 vault.

Therefore Tusker itself should have:

```text
srv1n/tusker/
├── skill/                          # operator skill source: how to use Tusker
│   ├── SKILL.md
│   ├── references/
│   ├── scripts/
│   └── assets/
├── tusker/                         # project knowledge graph: how to understand Tusker
│   ├── SKILL.md
│   ├── domains/
│   │   ├── cli/
│   │   ├── runtime/
│   │   ├── workflow/
│   │   ├── schema/
│   │   ├── knowledge-system/
│   │   ├── skill/
│   │   ├── obsidian/
│   │   ├── adoption/
│   │   └── codebase/
│   ├── epics/
│   ├── _config/
│   └── _system/generated/
├── cmd/tusker/
└── site/
```

The important split:

```text
skill/SKILL.md   = how to operate Tusker across repos
tusker/SKILL.md  = how to understand this repo/project
```

Do not merge these. They are different skills.

---

## 7. Naming and breaking changes

### 7.1 Path changes

V5:

```text
tusker/docs/**
```

V6:

```text
tusker/domains/**
```

Reason: `docs` is overloaded. `domains` reflects the primary navigation shape.

### 7.2 Schema changes

| V5 | V6 |
|---|---|
| `tusker.doc/v5` | `tusker.knowledge/v6` |
| `doc_nodes` | `knowledge_nodes` |
| `docs_resolution` | `knowledge_resolution` |
| `_config/docs-map.yaml` authored registry | `_system/generated/knowledge-map.json` generated registry |
| `tusker docs ...` | `tusker knowledge ...` and `tusker publish ...` |

### 7.3 CLI changes

Remove the conceptual `docs` namespace from V6.

V6 source-truth commands:

```bash
tusker knowledge map
tusker knowledge list
tusker knowledge show <NODE> [--capsule|--full|--section <name>]
tusker knowledge route "<intent>"
tusker knowledge freshness [--stale]
tusker knowledge check <TASK-ID>
tusker knowledge apply <TASK-ID> --node <NODE> --reason "..."
tusker knowledge noop <TASK-ID> --node <NODE> --reason "..."
tusker knowledge waive <TASK-ID> --node <NODE> --reason "..."
```

V6 projection commands:

```bash
tusker publish export [--site ./site]
tusker publish build [--site ./site]
tusker publish dev [--site ./site]
tusker publish llms [--site ./site]
tusker publish skill [--out ./dist/project-skill.zip]
```

V6 domain commands:

```bash
tusker domain list
tusker domain new <domain-id> --title "..."
tusker domain show <domain-id> [--capsule|--full]
tusker domain canon <domain-id>
tusker domain graph <domain-id> [--depth 1]
```

Graph command:

```bash
tusker graph <node-or-task-or-domain> [--depth 1] [--json]
```

Compatibility aliases may exist in a transition build, but V6 documentation and generated agent prompts must not teach `tusker docs ...`.

---

## 8. Core concepts

### 8.1 Domain

A domain is a stable area of understanding. It is the first place a human or agent goes when trying to understand part of the product or codebase.

Examples in Tusker itself:

```text
cli
runtime
workflow
schema
knowledge-system
skill
obsidian
adoption
codebase
```

Examples in an application repo:

```text
auth
billing
notifications
deployments
admin
codebase
```

A domain must have:

```text
INDEX.md
CANON.md
```

### 8.2 Knowledge page

A knowledge page records current durable truth.

Examples:

```text
domains/runtime/CANON.md
domains/runtime/reference/reviewer-lane.md
domains/runtime/troubleshooting/runner-timeout.md
domains/cli/reference/commands.md
domains/codebase/repo-map.md
```

### 8.3 Task

A task is an executable change contract and proof record. It can change code, behavior, workflow, or knowledge. It must not become the main explanation of the system.

### 8.4 Epic

An epic is a temporary initiative boundary. It groups work. It points to domains and knowledge nodes. It does not own canonical truth.

### 8.5 Projection

A projection is generated output:

```text
site/**
llms.txt
llms-full.txt
llms-internal.txt
llms-historical.txt
packaged skill zip
knowledge-map.json
backlinks indexes
```

---

## 9. Graph model

V6 is many-to-many by design.

```text
          Domain
            │
            v
       Knowledge node  <────── Task
            ▲                 │
            │                 v
         Domain            Epic
```

A task can touch many knowledge nodes. A knowledge node can accumulate proof from many tasks.

Correct:

```text
ORC-T-0019 touches:
- runtime/reference/reviewer-lane
- workflow/reference/review-policy
- cli/reference/reviewer-commands
```

Incorrect:

```text
Put ORC-T-0019 inside domains/runtime/tasks and treat it as docs.
```

Tasks are commits. Knowledge pages are working tree state.

---

## 10. Source format

### 10.1 Canonical source is Markdown

V6 canonical files are Markdown with YAML frontmatter.

```text
.md + YAML frontmatter + predictable headings + wikilinks
```

Do not use MDX as canonical source. The renderer can generate MDX from Markdown if needed.

### 10.2 Frontmatter contains machine semantics

Use frontmatter for facts the CLI needs before reading the body:

- schema
- ID / node
- domain
- kind
- audience
- agent layer
- canonical status
- source-of-truth paths
- stale triggers
- publication flags
- aliases
- owners

### 10.3 Body sections contain routing prose

Use body sections for routing prose:

```md
## Read this when

...

## Do not read this when

...
```

This avoids duplicate frontmatter prose while allowing the Go indexer to extract routing text cheaply.

---

## 11. Required body sections

### 11.1 All knowledge pages

Every knowledge page must include:

```md
## Read this when

## Do not read this when

## Source of truth

## Related
```

### 11.2 Domain `INDEX.md`

Required sections:

```md
## Read this when
## Do not read this when
## Current canon
## Start here
## Main knowledge nodes
## Source of truth
## Related domains
## Current work
```

`Current work` may be generated or partially generated.

### 11.3 Domain `CANON.md`

Required sections:

```md
## Read this when
## Do not read this when
## Current model
## Invariants
## Current defaults
## Deprecated behavior
## Source of truth
## Open questions
## Related
```

`CANON.md` should be short. If it becomes huge, it is doing the wrong job.

### 11.4 Task files

Required sections for all non-legacy closed tasks:

```md
## Intent
## Read this when
## Acceptance
## Verification plan
## Evidence
## Knowledge delta
```

`Knowledge delta` is required when `knowledge_nodes` is non-empty or `knowledge_change: true`.

---

## 12. Schemas

### 12.1 `tusker.domain/v6`

File: `tusker/domains/<domain>/INDEX.md`

```yaml
---
schema: tusker.domain/v6
id: runtime
title: Runtime orchestration
status: current
owner: sarav
summary: Runtime dispatch, runner state, review lane, attempts, leases, events, and local orchestration.
required: false
primary_epics:
  - ORC
knowledge_nodes:
  - runtime/canon
  - runtime/reference/state-model
  - runtime/reference/reviewer-lane
code_anchors:
  - cmd/tusker/daemon.go
  - cmd/tusker/runtime_store.go
  - cmd/tusker/commands_runtime.go
tags:
  - runtime
  - orchestration
  - reviewer
---
```

Rules:

- `id` must match the folder name.
- `knowledge_nodes` must resolve.
- `code_anchors` must resolve unless marked external.
- Domain status is one of `current`, `draft`, `archived`.
- Every domain must have `CANON.md`.

### 12.2 `tusker.knowledge/v6`

File examples:

```text
tusker/domains/runtime/CANON.md
tusker/domains/runtime/reference/reviewer-lane.md
tusker/domains/codebase/repo-map.md
```

Frontmatter:

```yaml
---
schema: tusker.knowledge/v6
node: runtime/reference/reviewer-lane
title: Reviewer lane
domain: runtime
kind: reference
audience: developer
agent_layer: capsule
canonical_status: approved
summary: Explains how review-state tasks dispatch independent reviewer runs and when they can close work.
aliases:
  - reviewer lane
  - agent reviewer
  - review handoff
source_of_truth:
  - tusker/WORKFLOW.md
  - cmd/tusker/daemon.go
  - cmd/tusker/commands_runtime.go
stale_when:
  paths:
    - tusker/WORKFLOW.md
    - cmd/tusker/daemon.go
    - cmd/tusker/commands_runtime.go
related_nodes:
  - workflow/reference/review-policy
  - cli/reference/runtime-commands
related_epics:
  - ORC
publish:
  lane: internal
  path: runtime/reviewer-lane
  include_in_llms: true
---
```

Allowed `kind` values:

```text
canon
index
architecture
reference
how-to
troubleshooting
decision
glossary
runbook
asset
feature
support
release
```

Allowed `audience` values:

```text
user
developer
operator
support
release
agent
internal
```

Allowed `agent_layer` values:

```text
none        # human-facing only; capsule may omit body
capsule     # agent gets a compact generated summary first
standalone  # agent-facing runbook or procedure
```

Allowed `canonical_status` values:

```text
draft
approved
deprecated
historical
superseded
archived
```

### 12.3 `tusker.task/v6`

File: `tusker/epics/<ACR>/<ACR>-T-0001.md`

```yaml
---
schema: tusker.task/v6
id: ORC-T-0019
title: Add policy-driven reviewer lane
epic: ORC
kind: feature
status: done
risk: medium
size: m
priority: p1
primary_domain: runtime
domains:
  - runtime
  - workflow
  - cli
knowledge_change: true
knowledge_nodes:
  - runtime/reference/reviewer-lane
  - workflow/reference/review-policy
  - cli/reference/runtime-commands
knowledge_resolution:
  - node: runtime/reference/reviewer-lane
    status: applied
    reason: Added reviewer lane behavior and close policy.
    by: codex
    at: "2026-05-12T10:30:00Z"
  - node: workflow/reference/review-policy
    status: applied
    reason: Updated review-state policy.
    by: codex
    at: "2026-05-12T10:31:00Z"
  - node: cli/reference/runtime-commands
    status: verified_noop
    reason: Existing command reference already covered reviewer inspection commands.
    by: codex
    at: "2026-05-12T10:32:00Z"
ai_assistance: assisted
ai_tools:
  - codex
created_at: "2026-05-12T09:00:00Z"
updated_at: "2026-05-12T10:35:00Z"
closed_at: "2026-05-12T10:35:00Z"
---
```

Rules:

- `knowledge_nodes` must resolve to existing `tusker.knowledge/v6` nodes.
- If `knowledge_nodes` is non-empty, `## Knowledge delta` is required.
- If status is `done`, every knowledge node must have a resolution.
- Waivers require a reason.
- Closed tasks require non-empty acceptance, evidence, and verification unless `migration_legacy: true`.

### 12.4 `tusker.epic/v6`

File: `tusker/epics/<ACR>/<ACR>.md`

```yaml
---
schema: tusker.epic/v6
id: ORC
title: Trustworthy orchestration
status: active
primary_domains:
  - runtime
  - workflow
knowledge_nodes:
  - runtime/canon
  - workflow/canon
owner: sarav
created_at: "2026-05-12T00:00:00Z"
---
```

Epic canon section should not duplicate domain canon.

```md
## Canon

This epic primarily touches [[runtime/CANON]] and [[workflow/CANON]].
Current truth lives there. This epic records the workstream and proof history.
```

---

## 13. Project `tusker/SKILL.md`

Every consumer repo gets a project-specific skill router at:

```text
tusker/SKILL.md
```

This is not the generic Tusker operator skill.

### 13.1 Size budget

`SKILL.md` should stay small.

Recommended limits:

```text
frontmatter.description <= 350 characters
body <= 2500 tokens
managed domain table <= 60 rows
```

### 13.2 Source split

Hand-authored:

- Purpose of the project knowledge skill.
- Answering rules.
- Safety/verification rules.
- How to route by intent.

Generated:

- Domain table.
- Maybe recent active work summary.
- Maybe stale knowledge warnings.

Managed block:

```md
<!-- tusker:domains:begin -->
| Intent | Read first | Canon | Notes |
|---|---|---|---|
| Modify CLI commands | [[cli/INDEX]] | [[cli/CANON]] | Command surface and flags. |
| Understand runtime | [[runtime/INDEX]] | [[runtime/CANON]] | Daemon, runner, reviewer lane. |
<!-- tusker:domains:end -->
```

### 13.3 Template

```md
---
schema: tusker.project-skill/v6
name: project-knowledge
description: Understand, modify, explain, or verify this repository using its domain canon, codebase map, task proof, and knowledge graph. Use when product behavior, architecture, implementation, docs, or repo conventions matter.
---

# Project knowledge skill

Use this file to route through this repository's Tusker knowledge graph.
Use the Tusker operator skill for task mechanics and CLI workflow.

## Routing rule

Start with the narrowest domain INDEX. Read CANON before task history.
Read task files only for proof, evidence, or implementation history.

## Answering rules

1. Prefer domain `CANON.md` over task history.
2. Prefer source code or API schemas over prose when exact behavior conflicts.
3. When code and canon disagree, trust code, mark canon stale, and report the conflict.
4. Do not read generated output by default.
5. Do not load full files when a capsule or section read is enough.
6. When suggesting a code change, include verification.
7. When production impact is possible, include rollback or safe-change checks.

## Domains

<!-- tusker:domains:begin -->
Generated by `tusker reindex`.
<!-- tusker:domains:end -->
```

---

## 14. Domain grammar

### 14.1 Domain folder shape

```text
domains/<domain>/
├── INDEX.md
├── CANON.md
├── architecture.md
├── glossary.md
├── decisions/
├── reference/
├── how-to/
├── troubleshooting/
└── assets/
```

Not every folder must have every subfolder, but `INDEX.md` and `CANON.md` are mandatory.

### 14.2 `INDEX.md` template

```md
---
schema: tusker.domain/v6
id: runtime
title: Runtime orchestration
status: current
summary: Runtime dispatch, reviewer lane, attempts, leases, events, and local orchestration.
knowledge_nodes:
  - runtime/canon
  - runtime/reference/state-model
  - runtime/reference/reviewer-lane
code_anchors:
  - cmd/tusker/daemon.go
  - cmd/tusker/runtime_store.go
---

# Runtime orchestration

## Read this when

Read this domain when you need to understand how Tusker starts, resumes,
reviews, interrupts, or records agent work.

## Do not read this when

Do not use this domain for task schema rules, Obsidian vault layout,
static site publishing, or generic CLI syntax.

## Current canon

Start with [[runtime/CANON]].

## Start here

- [[runtime/CANON]] — current runtime model and invariants.
- [[runtime/reference/state-model]] — attempts, turns, leases, sessions, events.
- [[runtime/reference/reviewer-lane]] — independent review dispatch and close policy.

## Main knowledge nodes

Generated from frontmatter.

## Source of truth

- `cmd/tusker/daemon.go`
- `cmd/tusker/runtime_store.go`
- `tusker/WORKFLOW.md`

## Related domains

- [[workflow/INDEX]]
- [[cli/INDEX]]
- [[codebase/INDEX]]

<!-- tusker:domain-work:begin -->
## Current work

Generated by `tusker reindex`.
<!-- tusker:domain-work:end -->
```

### 14.3 `CANON.md` template

```md
---
schema: tusker.knowledge/v6
node: runtime/canon
title: Runtime canon
domain: runtime
kind: canon
audience: developer
agent_layer: capsule
canonical_status: approved
summary: Current runtime model, invariants, defaults, and deprecated behavior.
source_of_truth:
  - cmd/tusker/daemon.go
  - cmd/tusker/runtime_store.go
  - tusker/WORKFLOW.md
stale_when:
  paths:
    - cmd/tusker/daemon.go
    - cmd/tusker/runtime_store.go
    - tusker/WORKFLOW.md
---

# Runtime canon

## Read this when

Read this before changing runtime dispatch, runner pickup, review handoff,
leases, attempts, events, or run state.

## Do not read this when

Do not use this for CLI flag syntax, docs publication, or Obsidian vault views.

## Current model

Tusker keeps durable task truth in markdown. Runtime execution state lives
outside canonical task frontmatter and is treated as local/operator state.

## Invariants

- Runnable task states are `active` and `rework`.
- Review handoff uses `review`.
- Terminal success is `done`.
- Runtime leases, attempts, sessions, and raw logs do not belong in task frontmatter.
- High and critical risk reviewer output remains advisory unless human policy changes.

## Current defaults

| Setting | Default |
|---|---|
| Default runner | Codex |
| Reviewer actor | agent-reviewer |
| Auto-close risks | low, medium |

## Deprecated behavior

- Do not store live run leases in task frontmatter.
- Do not treat implementation-worker output as independent verification.

## Source of truth

- `cmd/tusker/daemon.go`
- `cmd/tusker/runtime_store.go`
- `tusker/WORKFLOW.md`

## Open questions

- Whether reviewer auto-close should remain enabled by default for all repos.

## Related

- [[runtime/reference/reviewer-lane]]
- [[workflow/CANON]]
- [[cli/reference/runtime-commands]]

<!-- tusker:backrefs:begin -->
## Recent changes

Generated by `tusker reindex`.
<!-- tusker:backrefs:end -->
```

---

## 15. Codebase domain

Every V6 vault must include `domains/codebase`.

This is the highest-leverage addition for coding agents. Without it, every agent rediscovers the repository layout, test commands, and footguns through ad hoc grep.

Required files:

```text
domains/codebase/
├── INDEX.md
├── CANON.md
├── repo-map.md
├── architecture.md
├── conventions.md
├── testing.md
├── local-dev.md
├── safe-change-rules.md
└── known-footguns.md
```

### 15.1 `codebase/INDEX.md` minimum content

```md
# Codebase

## Read this when

Read this when the user asks the agent to inspect, modify, test, extend,
refactor, debug, or review this repository's code.

## Do not read this when

Do not use this for product behavior unless the behavior depends on code paths
listed in the relevant domain canon.

## Start here

- [[codebase/repo-map]] to locate code.
- [[codebase/conventions]] before editing.
- [[codebase/testing]] before claiming completion.
- [[codebase/safe-change-rules]] before changing high-risk areas.

## High-risk areas

| Area | Risk | Required checks |
|---|---|---|
| Runtime | runaway agent work, bad close state | runtime tests, manual smoke, reviewer policy check |
| Schema | invalid vaults, broken migrations | validator tests, fixture tests |
| Publish | stale or misleading LLM surfaces | export test, llms filter test |
```

### 15.2 `repo-map.md` rules

`repo-map.md` should map behavior to paths. It should not be a full code tour.

Good:

```md
| Need | Files | Notes |
|---|---|---|
| CLI command registration | `cmd/tusker/cli.go`, `cmd/tusker/commands_*.go` | Add command, help text, tests. |
| Knowledge indexer | `cmd/tusker/knowledge_*.go` | Parses frontmatter, headings, links. |
| Runtime daemon | `cmd/tusker/daemon.go`, `cmd/tusker/runtime_store.go` | Local operator state; not task truth. |
```

Bad:

```md
Here is every file and every function in the repository...
```

---

## 16. Knowledge delta and close gate

### 16.1 Rule

If `knowledge_nodes` is non-empty, `## Knowledge delta` is required.

Risk changes the detail level. Risk does not decide whether a delta exists.

```text
low risk      = one concise before/after row is enough
medium risk   = before/after + affected audience + node status
high/critical = full table + verification + human review
```

### 16.2 Table format

```md
## Knowledge delta

| Change type | Topic | Before | After | Audience | Knowledge nodes | Status |
|---|---|---|---|---|---|---|
| changed | Reviewer lane | Review was human-only after implementation. | Low/medium review tasks can dispatch independent reviewer and auto-close after gates pass. | developer, agent | runtime/reference/reviewer-lane, workflow/reference/review-policy | applied |
```

Allowed `Change type`:

```text
added
changed
removed
deprecated
clarified
verified_noop
waived
```

Allowed status:

```text
pending
applied
verified_noop
waived
```

### 16.3 Close gate

Before a task moves to `done`, Tusker validates:

```text
1. Task has non-empty acceptance criteria.
2. Task has evidence.
3. Task has verification.
4. If knowledge_nodes is non-empty, Knowledge delta exists.
5. Every knowledge node has a resolution.
6. Waivers have reasons.
7. No touched knowledge node is still stale after the resolution event.
8. `tusker validate` passes.
9. High/critical risk human gate passes.
```

---

## 17. Backrefs

### 17.1 Purpose

Backrefs solve the Obsidian navigation loop:

```text
knowledge page -> task proof -> evidence -> current status
```

They also let humans answer:

```text
Why does this page say this now?
```

### 17.2 Managed block

Each knowledge page may contain:

```md
<!-- tusker:backrefs:begin node="runtime/reference/reviewer-lane" limit="5" -->
## Recent changes

- [[ORC-T-0019]] — applied — Added reviewer lane behavior and close policy.
- [[DOC-T-0009]] — verified_noop — Checked publication docs; already current.
<!-- tusker:backrefs:end -->
```

Rules:

- Generated by `tusker reindex`.
- Default limit: 5 most recent resolved tasks.
- Full history lives in `_system/generated/backlinks.index.json`.
- Backrefs should use `[[wikilinks]]`.
- Generated block should stay near the bottom of the page.
- Manual edits inside the block are overwritten.

### 17.3 Backref source

Backrefs come from task `knowledge_resolution` entries, not from loose text links.

Loose links are navigation. Resolutions are proof.

---

## 18. Wikilinks

### 18.1 Source files use wikilinks

Use Obsidian-style wikilinks in source files:

```md
[[runtime/CANON]]
[[runtime/reference/reviewer-lane]]
[[ORC-T-0019]]
[[cli/reference/commands#Runtime commands]]
```

### 18.2 Resolver rules

Tusker resolves wikilinks in this order:

```text
1. Exact knowledge node ID.
2. Exact domain ID -> domain INDEX.md.
3. Exact task ID.
4. Exact epic ID.
5. Alias match from frontmatter.
```

Ambiguous links fail validation.

### 18.3 Renderer behavior

The site renderer rewrites wikilinks into route links using `publication.index.json`.

Source files stay Obsidian-native. Rendered output becomes web-native.

---

## 19. Go indexer and parser

### 19.1 Do not build a full Markdown parser

Tusker needs a small source indexer.

It parses:

```text
- YAML frontmatter
- ATX headings (#, ##, ###)
- Required section bodies
- Wikilinks
- Markdown links to .md files
- Managed blocks
- Markdown tables in known sections
```

It does not need to render Markdown.

### 19.2 Parsing algorithm

For every `.md` file under `tusker/` excluding generated paths:

```text
1. Read file as UTF-8.
2. If it starts with `---`, parse YAML frontmatter until next `---`.
3. Scan line-by-line.
4. Capture ATX headings with level and normalized title.
5. Build section map: heading -> body until next heading of same or higher level.
6. Extract wikilinks with a simple scanner or regex.
7. Extract markdown links that point to .md files.
8. Detect managed blocks.
9. For task files, parse `## Knowledge delta` markdown table.
10. Emit an indexed document record.
```

Heading normalization:

```text
lowercase
trim whitespace
collapse spaces
remove punctuation except hyphen
```

So these match:

```text
Read this when
read-this-when
Read This When
```

### 19.3 Parser data model

```go
type IndexedMarkdown struct {
    Path        string
    Schema      string
    Frontmatter map[string]any
    Title       string
    Headings    []Heading
    Sections    map[string]Section
    WikiLinks   []WikiLink
    MarkdownLinks []MarkdownLink
    ManagedBlocks []ManagedBlock
    Tables      map[string]MarkdownTable
}
```

### 19.4 Required generated indexes

`_system/generated/knowledge-map.json`:

```json
{
  "schema": "tusker.knowledge-map/v6",
  "generated_at": "2026-05-12T00:00:00Z",
  "domains": [],
  "knowledge_nodes": [],
  "tasks": [],
  "epics": []
}
```

Each knowledge node record includes:

```json
{
  "node": "runtime/reference/reviewer-lane",
  "path": "domains/runtime/reference/reviewer-lane.md",
  "title": "Reviewer lane",
  "domain": "runtime",
  "kind": "reference",
  "audience": "developer",
  "agent_layer": "capsule",
  "canonical_status": "approved",
  "summary": "...",
  "read_when": "...",
  "do_not_read_when": "...",
  "source_of_truth": ["..."],
  "stale_when": {"paths": ["..."]},
  "links_out": ["..."],
  "backlinks": ["..."],
  "recent_tasks": ["ORC-T-0019"],
  "freshness": "current"
}
```

Other generated files:

```text
route-index.json       # deterministic intent routing index
backlinks.index.json   # graph edges and task proof history
freshness.index.json   # source fingerprints and stale state
graph.index.json       # nodes and edges for graph command
publication.index.json # renderable docs and routes
capsules/*.md          # optional precomputed capsules
```

---

## 20. Freshness model

### 20.1 Source fingerprints

Each knowledge page declares source files:

```yaml
source_of_truth:
  - cmd/tusker/daemon.go
  - tusker/WORKFLOW.md
stale_when:
  paths:
    - cmd/tusker/daemon.go
    - tusker/WORKFLOW.md
```

Tusker computes a fingerprint:

```text
fingerprint = hash(contents of all matched source_of_truth + stale_when paths)
```

The fingerprint is stored in the latest knowledge resolution event.

### 20.2 Stale states

Allowed freshness states:

```text
current
stale
missing
unknown
waived
historical
```

A node is stale when:

```text
current source fingerprint != fingerprint recorded by latest apply/noop resolution
```

A node is missing when:

```text
frontmatter declares a node but the file does not exist
or task references a node not present in knowledge-map
```

A historical/deprecated node is excluded from normal stale checks unless referenced by active work.

### 20.3 Resolution commands

```bash
tusker knowledge apply <TASK-ID> --node <NODE> --reason "..."
tusker knowledge noop <TASK-ID> --node <NODE> --reason "..."
tusker knowledge waive <TASK-ID> --node <NODE> --reason "..."
```

All three record:

```text
node
task
status
reason
actor
timestamp
source_fingerprint
```

---

## 21. Capsules and context budgets

### 21.1 Rule

No Tusker command should dump the whole corpus by default.

### 21.2 Suggested budgets

```text
root tusker/SKILL.md       <= 2500 tokens
domain INDEX capsule       <= 900 tokens
knowledge capsule          <= 800 tokens
task capsule               <= 800 tokens
graph depth 1 capsule      <= 1200 tokens
search result item         <= 80 tokens
```

These are defaults, not hard universal limits. The CLI should expose config.

### 21.3 Knowledge capsule shape

```text
Title
Node
Domain / kind / audience / agent layer
Canonical status
Read this when
Do not read this when
Summary
Current canon excerpt, if present
Source-of-truth files
Freshness status
Related nodes
Recent proof tasks
```

### 21.4 Domain capsule shape

```text
Domain title
Read this when
Do not read this when
Current canon pointer
Top knowledge nodes
Source-of-truth paths
Active tasks touching this domain
Stale nodes count
Related domains
```

### 21.5 Routing before reading

Agent default route:

```bash
tusker knowledge route "reviewer lane auto close"
tusker domain show runtime --capsule
tusker knowledge show runtime/reference/reviewer-lane --capsule
```

Only then should it open a full file.

---

## 22. Knowledge routing

### 22.1 Deterministic route index

`route-index.json` should include:

```text
node id
title
aliases
summary
domain
kind
audience
agent_layer
read_when
do_not_read_when
source_of_truth paths
related nodes
```

### 22.2 `tusker knowledge route`

Command:

```bash
tusker knowledge route "How does reviewer auto-close work?"
```

Output:

```text
Best matches:
1. runtime/reference/reviewer-lane
   why: matched "reviewer", "auto-close", domain runtime
   read: tusker knowledge show runtime/reference/reviewer-lane --capsule
2. workflow/reference/review-policy
   why: matched review state and close policy
   read: tusker knowledge show workflow/reference/review-policy --capsule
```

Use simple scoring first:

```text
exact node match              +100
alias match                   +80
title phrase match            +60
domain match                  +40
read_when keyword match       +30
summary keyword match         +20
source path match             +10
do_not_read_when match        -100
historical/deprecated         -50 unless --include-historical
```

No embeddings required for V6.0.

---

## 23. Publication model

### 23.1 Source vs projection

Source:

```text
tusker/domains/**
tusker/SKILL.md
tusker/epics/**
```

Projection:

```text
site/src/content/docs/**
site/public/llms.txt
site/public/llms-full.txt
site/public/llms-internal.txt
site/public/llms-historical.txt
dist/*-skill.zip
```

Never edit projections by hand.

### 23.2 Default LLM lanes

Default generated surfaces:

```text
site/public/llms.txt
    compact current canon, non-internal, non-historical

site/public/llms-full.txt
    full bodies for current canon and selected current references, non-internal

site/public/llms-internal.txt
    internal and agent-only nodes, opt-in

site/public/llms-historical.txt
    deprecated, historical, superseded, archived nodes, opt-in
```

Default `llms.txt` filter:

```text
canonical_status in {approved, draft}
audience not in {internal}
publish.include_in_llms != false
kind not in {asset} unless asset has transcript/explanation
exclude tasks and evidence by default
```

Historical material must not appear in default LLM surfaces.

### 23.3 Site renderer

The renderer consumes `publication.index.json` and source Markdown.

Rules:

- The renderer may turn Markdown into MDX/HTML.
- The renderer may insert components around asset references.
- The renderer must not require canonical files to be MDX.
- The renderer must rewrite wikilinks.
- The renderer must not publish task evidence logs by default.

---

## 24. Asset model

Assets must be addressable. Do not rely on agents “ignoring” images or videos.

### 24.1 Asset knowledge file

```md
---
schema: tusker.knowledge/v6
node: auth/assets/saml-setup-video
title: SAML setup walkthrough video
domain: auth
kind: asset
audience: user
agent_layer: capsule
canonical_status: approved
asset:
  type: video
  source: assets/saml-setup.mp4
  transcript: assets/saml-setup-transcript.md
  chapters: assets/saml-setup-chapters.md
related_nodes:
  - auth/how-to/configure-saml-sso
---

# SAML setup walkthrough video

## Read this when

Use this asset when a human asks for a visual walkthrough of SAML setup.

## Do not read this when

Do not use this for OIDC, SCIM, social login, or API key authentication.

## Source of truth

- [[auth/how-to/configure-saml-sso]]
- `assets/saml-setup-transcript.md`

## Related

- [[auth/how-to/configure-saml-sso]]
```

### 24.2 Diagrams

Prefer source diagrams:

```text
.mmd
.puml
.svg with explanation
```

A diagram asset should include:

```text
source
rendered output
short explanation
use_when
related nodes
```

---

## 25. Policy config

`_config/knowledge-policy.yaml` contains policy only. It must not become a hidden per-node registry.

Example:

```yaml
schema: tusker.knowledge-policy/v6

required_domain_files:
  - INDEX.md
  - CANON.md

allowed_kinds:
  - canon
  - index
  - architecture
  - reference
  - how-to
  - troubleshooting
  - decision
  - glossary
  - runbook
  - asset
  - feature
  - support
  - release

allowed_audiences:
  - user
  - developer
  - operator
  - support
  - release
  - agent
  - internal

allowed_agent_layers:
  - none
  - capsule
  - standalone

required_sections:
  knowledge:
    - Read this when
    - Do not read this when
    - Source of truth
    - Related
  canon:
    - Current model
    - Invariants
    - Current defaults
    - Deprecated behavior
    - Source of truth
    - Open questions
  domain:
    - Read this when
    - Do not read this when
    - Current canon
    - Start here
    - Source of truth
    - Related domains

backrefs:
  default_limit: 5
  max_limit: 20

capsules:
  knowledge_max_tokens: 800
  domain_max_tokens: 900
  task_max_tokens: 800

publication:
  default_llms_statuses:
    - approved
    - draft
  default_llms_excluded_audiences:
    - internal
  historical_statuses:
    - deprecated
    - historical
    - superseded
    - archived
```

---

## 26. Validation rules

V6 validation should fail on these conditions.

### 26.1 Domain validation

```text
- Missing tusker/SKILL.md.
- Missing tusker/domains/codebase/INDEX.md.
- Missing tusker/domains/codebase/CANON.md.
- Domain folder lacks INDEX.md.
- Domain folder lacks CANON.md.
- Domain INDEX.md id does not match folder name.
- Domain INDEX.md references missing knowledge nodes.
- Domain INDEX.md lacks Read this when / Do not read this when.
```

### 26.2 Knowledge validation

```text
- Knowledge page lacks schema: tusker.knowledge/v6.
- Knowledge page node does not match domain folder.
- Knowledge page lacks source_of_truth.
- Knowledge page lacks Read this when.
- Knowledge page lacks Do not read this when.
- Knowledge page has canonical_status: approved but empty Current model / Source of truth sections when kind=canon.
- Knowledge page links to missing node.
- Knowledge page source_of_truth path does not exist unless marked external.
- Knowledge page has ambiguous aliases.
- Historical/deprecated page appears in default llms.txt.
```

### 26.3 Task validation

```text
- Task references missing domain.
- Task references missing knowledge node.
- Task has knowledge_nodes but no Knowledge delta.
- Knowledge delta references nodes not in frontmatter, unless explicitly allowed.
- Task status done but no acceptance.
- Task status done but no evidence.
- Task status done but no verification.
- Task status done but unresolved knowledge nodes.
- Waiver has no reason.
- Closed task with empty scaffolding unless migration_legacy: true.
```

### 26.4 Projection validation

```text
- Generated site source changed manually.
- llms.txt includes internal/historical nodes by default.
- Publication route removed without redirect.
- Wikilink cannot be rewritten to a route for published page.
```

---

## 27. Command surface

### 27.1 Init

```bash
tusker init [--profile generic|library|app|cli|infra|tusker] [--yes]
```

Creates:

```text
tusker/SKILL.md
tusker/domains/codebase/INDEX.md
tusker/domains/codebase/CANON.md
tusker/domains/product/INDEX.md
tusker/domains/product/CANON.md
tusker/epics/
tusker/_config/knowledge-policy.yaml
tusker/_system/generated/
tusker/WORKFLOW.md
```

Profiles add starter domains:

```text
generic: codebase, product
library: codebase, api, usage
app: codebase, product, auth, operations
cli: codebase, cli, workflow
infra: codebase, deployments, operations, security
tusker: codebase, cli, runtime, workflow, schema, knowledge-system, skill, obsidian, adoption
```

### 27.2 Knowledge creation

```bash
tusker knowledge new runtime/reference/reviewer-lane \
  --title "Reviewer lane" \
  --kind reference \
  --audience developer \
  --agent-layer capsule \
  --source cmd/tusker/daemon.go,tusker/WORKFLOW.md
```

Creates the file and updates no central authored map. `reindex` generates the map.

### 27.3 Domain creation

```bash
tusker domain new billing --title "Billing" --summary "Plans, invoices, usage, entitlements."
```

Creates:

```text
domains/billing/INDEX.md
domains/billing/CANON.md
```

### 27.4 Inspection

```bash
tusker domain list
tusker domain show billing --capsule
tusker domain canon billing
tusker knowledge route "invoice entitlement mismatch"
tusker knowledge show billing/troubleshooting/entitlement-mismatch --capsule
tusker graph billing --depth 1
```

### 27.5 Close gate

```bash
tusker knowledge check BILL-T-0007
tusker knowledge apply BILL-T-0007 --node billing/reference/entitlements --reason "Updated entitlement matrix."
tusker verify BILL-T-0007 --by reviewer
tusker close BILL-T-0007 --by sarav
```

### 27.6 Reindex

```bash
tusker reindex
```

Writes:

```text
_system/generated/knowledge-map.json
_system/generated/route-index.json
_system/generated/graph.index.json
_system/generated/backlinks.index.json
_system/generated/freshness.index.json
managed blocks in tusker/SKILL.md
managed backref blocks in knowledge pages
managed current-work blocks in domain indexes
```

---

## 28. AGENTS.md contract

`AGENTS.md` should stay small. It should not become another knowledge corpus.

Template:

```md
# Agent instructions

For project/product knowledge, start with `tusker/SKILL.md`.
For Tusker task mechanics, use the installed Tusker operator skill and CLI.

Do not read the whole vault. Route first:

```bash
tusker knowledge route "<intent>"
tusker domain show <domain> --capsule
tusker knowledge show <node> --capsule
```

Do not edit generated files under:

- `tusker/_system/generated/**`
- `site/src/content/docs/**`
- `site/src/generated/**`

Before claiming task completion, run the relevant verification and `tusker validate`.
```

---

## 29. Migration stance

The project is not public-distribution-stable. Therefore V6 should be a clean break.

Recommended stance:

```text
Do not preserve bad vocabulary for compatibility.
Do not teach aliases to agents.
Do not keep docs-map as authored source.
Do not keep canonical files under tusker/docs.
```

Migration can be destructive and LLM-assisted:

```text
1. Create V6 vault structure.
2. Move durable pages from tusker/docs/** into tusker/domains/**.
3. Convert tusker.doc/v5 -> tusker.knowledge/v6.
4. Convert doc_nodes -> knowledge_nodes.
5. Convert docs_resolution -> knowledge_resolution.
6. Create domain INDEX/CANON files.
7. Generate knowledge-map from frontmatter.
8. Validate and manually clean sludge.
```

Migration scripts are optional. Spec correctness matters more than automated migration.

---

## 30. Rejected alternatives

### 30.1 Keep `tusker/docs/<domain>`

Rejected. It preserves the naming ambiguity. `docs` already means source, site, public docs, internal canon, close-gate target, and LLM surface.

### 30.2 Keep `tusker docs ...`

Rejected for V6 docs and generated prompts. Source truth and projection are different operations.

### 30.3 Keep authored `_config/docs-map.yaml`

Rejected. It becomes a hidden CMS. Semantic facts belong in the file that humans and agents open. The generated map is a cache.

### 30.4 Put tasks under domain folders

Rejected. Tasks are proof records. Domain folders hold current truth.

### 30.5 One giant `SKILL.md`

Rejected. It destroys progressive disclosure.

### 30.6 Domain as separate installed skill

Rejected for V6.0. Start with one project skill and many domain folders. Split into multiple skills only if ownership, size, tooling, or installation boundaries force it.

### 30.7 Custom parser

Rejected. Implement a lightweight indexer, not a renderer.

### 30.8 MDX as canonical source

Rejected. It is too easy for canonical content to become frontend code. Use Markdown source and renderer-side enhancement.

---

## 31. Implementation plan

### 31.1 Epic: KNO — V6 Knowledge Graph

#### KNO-T-0001 — V6 schemas and path layout

Deliver:

```text
- tusker.domain/v6 schema
- tusker.knowledge/v6 schema
- tusker.task/v6 knowledge fields
- tusker.epic/v6 domain fields
- tusker init --profile support
- V6 templates
```

Acceptance:

```text
- fresh `tusker init --profile generic` creates valid V6 vault
- `tusker validate` passes on fresh vault
- no `tusker/docs/**` created
```

#### KNO-T-0002 — Lightweight Markdown indexer

Deliver:

```text
- YAML frontmatter parser
- required heading extraction
- wikilink extraction
- markdown link extraction
- managed block detection/replacement
- markdown table parser for Knowledge delta
```

Acceptance:

```text
- parses domain, knowledge, epic, and task files
- extracts Read this when / Do not read this when
- emits knowledge-map.json
- fails validation on missing required sections
```

#### KNO-T-0003 — Domain and knowledge CLI

Deliver:

```text
- tusker domain list/show/new/canon/graph
- tusker knowledge map/list/show/route/freshness/check/apply/noop/waive
- capsule output for domain and knowledge pages
```

Acceptance:

```text
- route command finds correct node by alias/title/read_when
- show --capsule stays under configured budget
- check/apply/noop/waive update task knowledge_resolution
```

#### KNO-T-0004 — Backrefs and Obsidian graph

Deliver:

```text
- generated backrefs blocks
- current work blocks in domain indexes
- wikilink resolver
- graph.index.json
- tusker graph command
```

Acceptance:

```text
- knowledge page links to recent task proof
- task IDs resolve through wikilinks
- ambiguous links fail validation
```

#### KNO-T-0005 — Freshness fingerprints

Deliver:

```text
- source_of_truth/stale_when glob hashing
- freshness.index.json
- stale/current/missing/waived states
- validation gate for stale touched nodes
```

Acceptance:

```text
- modifying a source_of_truth file marks affected nodes stale
- applying/nooping a node records new fingerprint
- closing touched task with stale unresolved node fails
```

#### KNO-T-0006 — Publish split and LLM lanes

Deliver:

```text
- tusker publish export/build/dev/llms/skill
- publication.index.json
- llms.txt filters
- llms-full.txt filters
- llms-internal.txt
- llms-historical.txt
```

Acceptance:

```text
- default llms.txt excludes internal/historical/deprecated pages
- task records do not publish by default
- source wikilinks rewrite in rendered output
```

#### KNO-T-0007 — Dogfood Tusker itself

Deliver:

```text
- Tusker repo V6 vault
- domains: cli, runtime, workflow, schema, knowledge-system, skill, obsidian, adoption, codebase
- tusker/SKILL.md project router
- AGENTS.md updated to point into tusker/SKILL.md
```

Acceptance:

```text
- agent can route from "reviewer lane" to runtime canon without reading epics
- agent can route from "change CLI flag" to cli/codebase nodes
- human can browse in Obsidian through wikilinks and backrefs
```

---

## 32. V6 acceptance criteria

V6 is ready when these pass:

```text
1. Fresh init creates a valid V6 vault.
2. Tusker's own repo dogfoods V6.
3. `tusker/SKILL.md` routes by domain and stays small.
4. Every domain has INDEX.md and CANON.md.
5. Every knowledge page has Read this when / Do not read this when.
6. `tusker knowledge route` returns useful nodes without reading full files.
7. `tusker knowledge show <node> --capsule` is bounded.
8. Tasks with knowledge_nodes cannot close without Knowledge delta and resolution.
9. Backrefs generate from knowledge_resolution.
10. Wikilinks resolve in Obsidian and rewrite in published output.
11. Default llms.txt excludes historical/deprecated/internal content.
12. `_config/knowledge-policy.yaml` contains policy only.
13. `knowledge-map.json` is generated from frontmatter.
14. Site output is disposable.
```

---

## 33. Concrete recommendation for Tusker repo dogfood

Tusker itself should use these domains:

```text
tusker/domains/
├── cli/
├── runtime/
├── workflow/
├── schema/
├── knowledge-system/
├── skill/
├── obsidian/
├── adoption/
└── codebase/
```

Initial canonical nodes:

```text
cli/canon
cli/reference/commands
runtime/canon
runtime/reference/reviewer-lane
runtime/reference/state-model
workflow/canon
workflow/reference/lifecycle
workflow/reference/review-policy
schema/canon
schema/reference/frontmatter
knowledge-system/canon
knowledge-system/reference/freshness
knowledge-system/reference/publication
skill/canon
skill/reference/operator-skill
obsidian/canon
obsidian/reference/vault-layout
adoption/canon
codebase/canon
codebase/repo-map
codebase/testing
codebase/safe-change-rules
```

Do not start by perfectly migrating every page. Start by making these nodes good.

---

## 34. Final position

Tusker V6 should be a product knowledge graph, not a documentation plugin for a task tracker.

The right invariant:

```text
Same vault.
Same graph.
Separate node types.
Domain-shaped knowledge.
Task proof stays in epics.
Project skill routes the corpus.
Generated surfaces stay disposable.
```

The right implementation bet:

```text
Build the schemas, indexer, validator, router, freshness model, backrefs, and projection commands.
Do not build a CMS.
Do not build a Markdown renderer.
Do not let tasks become docs.
```

That is the spec.
