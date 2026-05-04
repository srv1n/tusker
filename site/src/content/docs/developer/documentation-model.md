---
title: "Tusker Documentation Model"
description: "How Tusker lays out durable docs, why Diátaxis is metadata, and how the CLI keeps docs freshness tied to task close."
tusker:
  audience: "developer"
  canonical: true
  canonical_status: "approved"
  owner_epic: "DOC"
  publish_path: "developer/documentation-model"
  route: "/developer/documentation-model/"
  source_kind: "repo_doc"
  source_path: "docs/documentation-model.md"
  summary: "How Tusker lays out durable docs, why Diátaxis is metadata, and how the CLI keeps docs freshness tied to task close."
  tags:
    - "docs"
    - "diataxis"
    - "architecture"
  updated: "2026-04-29"
  verified_at: "2026-04-29"
---

# Tusker Documentation Model

Tusker treats documentation as part of the work system, not as a separate cleanup phase. A task can change code, behavior, workflow, or operator expectations. When it changes durable understanding, the task has to name the docs it affects and prove the docs impact was handled before close.

That is the core idea:

```text
task changes reality
        |
        v
Knowledge delta explains the reader-facing change
        |
        v
doc_nodes name the exact durable pages affected
        |
        v
docs check/apply/waive records the resolution
        |
        v
task can close
```

Docs do not live inside epic folders anymore. Epics and tasks are execution records. Docs are durable knowledge pages under `tusker/docs/**`, indexed by `_config/docs-map.yaml`, and optionally exported into the static site.

## Philosophy

Tusker's documentation model is built around five rules.

| Rule | Meaning |
|---|---|
| Docs are durable knowledge | A doc explains current truth. A task proves how truth changed. Do not confuse the two. |
| Diátaxis is metadata, not folder jail | A page declares its reader intent, but the repo can still organize files in the shape that works locally. |
| Tasks point to exact docs | `domains` are broad areas. `doc_nodes` are exact pages. The close gate uses exact pages. |
| Freshness is evidence-backed | Tusker records apply/no-op/waiver events and shows stale docs instead of hoping people remember. |
| Generated output is disposable | `site/src/content/docs/**`, manifests, and generated indexes are compiler output. Author in source files. |

The point is not ceremony. The point is preventing the expensive lie: code changed, behavior changed, future agents read stale docs, and everyone acts surprised.

## Layout

```text
your-repo/
├── README.md
├── docs/
│   ├── documentation-model.md        # repo-level explanation of this model
│   └── publication.yaml              # repo docs selected for site export
├── tusker/
│   ├── _config/
│   │   └── docs-map.yaml             # controlled doc_node catalog
│   ├── docs/
│   │   ├── spec/
│   │   ├── reference/
│   │   └── agents/
│   ├── epics/
│   │   └── DOC/
│   │       ├── DOC.md
│   │       └── DOC-T-0001.md
│   ├── Docs.md                       # generated reader catalog
│   └── _system/generated/
│       ├── docs.index.json           # machine catalog + freshness
│       └── publication.index.json    # vault docs selected for export
└── site/
    ├── src/content/docs/             # generated; do not author here
    ├── src/generated/
    └── public/
        ├── canon-manifest.json
        ├── llms.txt
        └── llms-full.txt
```

## The Access Layer

`tusker/_config/docs-map.yaml` is the controlled access layer. It answers the questions a task or agent needs before touching docs:

- What exact `doc_node` should a task name?
- Which source page owns that node?
- Which domain owns the page?
- What kind of reader need does the page serve?
- Is this for humans, agents, or both?
- Which source files define truth for the page?
- What file changes should make the page stale?

A node looks like this:

```yaml
- id: tusker/docs-system
  title: Documentation freshness system
  page: docs/reference/docs-pipeline.md
  domain: docs-system
  mode: explanation
  audience: developer
  agent_layer: capsule
  kind: canon
  source_of_truth:
    - cmd/tusker/docs_map.go
    - cmd/tusker/docs_impact.go
    - cmd/tusker/docs_export.go
  stale_when:
    paths:
      - cmd/tusker/docs_*.go
      - skill/references/DOCS_PUBLICATION.md
  publish_lane: internal
  publish_path: reference/docs-pipeline
  publish_description: Docs routing, impact hook, waiver flow, and publication rules.
```

Do not invent doc nodes in tasks. Run `tusker docs map` and pick an existing node. If the right node does not exist, adding the node is part of the work.

## Diátaxis In Tusker

Tusker uses the Diátaxis model as a classification system. It does not force directories to mirror the taxonomy.

| Mode | Reader is asking | Default catalog section | Typical Tusker page |
|---|---|---|---|
| `tutorial` | "Teach me by walking me through it." | Start here | First-run onboarding |
| `how-to` | "Tell me the steps." | Guides | Operator workflows, agent recipes |
| `reference` | "Give me the facts." | Reference | CLI, schema, validator, templates |
| `explanation` | "Tell me why this exists." | Concepts | Architecture, tradeoffs, lifecycle rationale |

This matters because docs fail when they mix jobs. A CLI reference should not become a tutorial. A concept page should not hide the exact command syntax. A how-to should not bury the steps inside a history lesson.

Tusker makes the mode explicit so agents can update the right kind of content:

- `tutorial`: preserve sequence, prerequisites, checkpoints, and expected result.
- `how-to`: preserve task steps, decision points, rollback, and validation.
- `reference`: preserve exact fields, commands, flags, schemas, and examples.
- `explanation`: preserve rationale, tradeoffs, boundaries, and mental model.

## Audience And Agent Layer

`audience` says who the page is for. Current defaults include:

- `developer`
- `user`
- `operator`
- `support`
- `release`
- `agent`
- `internal`

`agent_layer` says how much of the page is meant for agents:

| Agent layer | Meaning |
|---|---|
| `none` | Human-facing doc only. |
| `capsule` | Human-facing doc with a compact agent note or agent-relevant metadata. |
| `standalone` | Agent-facing runbook or recipe. |

Agent docs are not second-class. If an agent needs a repeatable procedure, make it a doc under `tusker/docs/agents/**`, map it with `audience: agent`, and make it `agent_layer: standalone`.

## Tasks And Knowledge Delta

Tasks carry two docs-routing fields:

```yaml
domains:
  - docs-system
doc_nodes:
  - tusker/docs-system
```

`domains` are broad. They help humans scan work by area.

`doc_nodes` are exact. They drive validation and close gates.

High-risk tasks also need a `## Knowledge delta` table:

| Change type | Topic | Before | After | Audience | Target doc nodes | Mode | Status |
|---|---|---|---|---|---|---|---|
| changed | Docs model | Docs routing was implicit. | Docs routing is controlled by docs-map metadata. | developer | tusker/docs-system | explanation | pending |

This table is not decorative. `tusker docs check` reads it and routes target doc nodes from it. Empty nonsense like "updated implementation" should fail review because it does not tell a future reader what changed.

## Docs Close Gate

For every task with `doc_nodes`, close requires one resolution per node.

```bash
tusker docs check DOC-T-0001
tusker docs apply DOC-T-0001 --node tusker/docs-system --reason "Updated docs-map contract"
tusker docs noop DOC-T-0001 --node tusker/docs-system --reason "Checked; already current"
tusker docs waive DOC-T-0001 tusker/docs-system --reason "No reader-facing behavior changed"
```

Resolution statuses:

| Status | Meaning |
|---|---|
| `applied` | The target doc was updated or the needed docs change was applied. |
| `verified_noop` | The target doc was checked and already correct. |
| `waived` | The docs change is intentionally skipped and the reason is recorded. |

Waivers require a reason. A waiver with no reason is just a quiet failure with better formatting.

## CLI Surface

The docs system is intentionally tucked under the existing `docs` command instead of adding more top-level commands.

| Command | Use |
|---|---|
| `tusker docs model` | Explain the docs philosophy, Diátaxis mapping, agent layers, and close gate. |
| `tusker docs map` | Show controlled domains and doc nodes from `_config/docs-map.yaml`. |
| `tusker docs map <doc-node>` | Inspect one node: page, domain, mode, audience, source-of-truth, stale triggers. |
| `tusker docs catalog` | Show the generated reader-facing catalog from `Docs.md` / `docs.index.json`. |
| `tusker docs freshness` | Show every doc node's freshness, linked tasks, last event, and stale triggers. |
| `tusker docs freshness --stale` | Show only docs that are missing, stale, waived, or need verification. |
| `tusker docs check <task>` | Dry-run the docs impact for a task. |
| `tusker docs apply <task> --node <node>` | Record that docs were applied. |
| `tusker docs noop <task> --node <node>` | Record that the doc was checked and already correct. |
| `tusker docs waive <task> <node> --reason "..."` | Record an explicit no-change decision. |
| `tusker docs export` | Compile vault docs and registered repo docs into the site tree. |
| `tusker docs build` | Export, then build the static site. |

Every inspection command supports `--json` where machine output is useful.

## Generated Catalog

`tusker reindex` writes `tusker/Docs.md`. It is for humans in Obsidian:

```text
Docs Catalog
├── Start here
├── Guides
├── Reference
├── Concepts
├── Troubleshooting
├── Examples
├── For agents
└── Media
```

The sections are reader-facing labels. They are derived from docs-map metadata, not hand-maintained folders.

`tusker/_system/generated/docs.index.json` is for tools. It includes:

- doc node
- title
- section
- audience
- mode
- agent layer
- domain
- source path
- publish path
- freshness state
- linked tasks
- waivers
- last verified event
- source-of-truth paths
- stale triggers

## Publication

There are two source types for the public/static site:

| Source | Config |
|---|---|
| Vault docs | `tusker/docs/**` plus `tusker/_config/docs-map.yaml` |
| Repo-native docs | `docs/publication.yaml` |

The exporter writes:

- `site/src/content/docs/**`
- `site/src/generated/content-manifest.json`
- `site/src/generated/canon-manifest.json`
- `site/public/canon-manifest.json`
- `site/src/generated/routes-removed.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

Authoring rule: never edit `site/src/content/docs/**` directly. It is generated output.

`llms.txt` is the compact AI-readable catalog. `llms-full.txt` includes full Markdown bodies in docs-map order so agents can retrieve current canon without spelunking through stale source files.

## Maintenance Workflow

When implementation changes docs behavior:

1. Run `tusker docs map` and identify affected doc nodes.
2. Put those nodes in the task frontmatter.
3. Fill the task's `Knowledge delta` if durable understanding changed.
4. Update pages under `tusker/docs/**` or repo docs registered in `docs/publication.yaml`.
5. Run `tusker docs check <TASK-ID>`.
6. Apply, verify no-op, or waive every target node.
7. Run `tusker reindex`.
8. Run `tusker docs export --site ./site`.
9. Run `tusker docs build --site ./site`.
10. Run `tusker validate`.

That looks like a lot written out. In practice it is mostly two commands at the end, and it prevents an entire class of stale-doc failures.

## What Not To Do

- Do not put durable docs inside task bodies.
- Do not make raw Diátaxis labels the main navigation unless the project explicitly wants that.
- Do not invent doc nodes in task frontmatter.
- Do not waive docs impact without a reason.
- Do not edit generated site docs by hand.
- Do not treat `llms.txt` as source. It is a generated retrieval surface.

## Mental Model

```text
_config/docs-map.yaml
        |
        | controls
        v
tusker/docs/**  <----- tasks name doc_nodes
        |                    |
        | reindex            | docs check/apply/waive
        v                    v
tusker/Docs.md       docs_resolution on task
docs.index.json              |
        |                    |
        +---------- validate close gate
        |
        | export
        v
site/src/content/docs/**
site/public/canon-manifest.json
site/public/llms.txt
site/public/llms-full.txt
```

If you remember one sentence: docs-map is the contract, docs pages are the truth, tasks are the proof.
