---
title: "Repository Overview"
description: "High-level overview of the Tusker repository, install modes, and key entry points."
tusker:
  audience: "developer"
  publish_path: "developer/repository/repository-overview"
  route: "/developer/repository/repository-overview/"
  source_kind: "repo_doc"
  source_path: "README.md"
  summary: "High-level overview of the Tusker repository, install modes, and key entry points."
  tags:
    - "repository"
    - "overview"
  updated: "2026-04-29"
---

# Tusker

Tusker is a markdown-first task tracker that lives inside your git repo and reads cleanly in Obsidian. It is built for one developer, or a very small team, working with coding agents.

Each task is one markdown file. It is both:

1. **An executable contract for an agent.** Frontmatter carries the machine fields: risk, kind, domains, doc targets, dependencies, AI tools, and lifecycle state. The body carries intent, scope, acceptance, verification, deliverables, evidence, and knowledge delta.
2. **A human-readable work record.** Open it in Obsidian and it reads like a normal spec plus execution log. Humans define intent and accept evidence; agents implement and leave proof.

```text
your-repo/
├── src/
└── tusker/
    ├── epics/
    │   └── MEM/
    │       ├── MEM.md          # epic + canon
    │       ├── MEM-T-0001.md   # task
    │       └── MEM-T-0002.md   # task, kind: bug
    ├── docs/
    │   ├── spec/
    │   └── reference/
    ├── _config/
    └── _system/
```

![Tusker keeps the tracker in git while Obsidian stays a reading and editing surface.](/generated/assets/repo-doc/developer-repository-repository-overview/docs/diagrams/01-two-surfaces.png)

## Why this exists

Linear and Jira live outside the repo. GitHub Issues are thin. Beads gets the git-native idea right but is not designed around Obsidian-readable executable specs for coding agents.

When an agent does the implementation, the spec is the control plane. Tusker makes the ticket carry the acceptance contract, the verification plan, the evidence, and the durable knowledge change. That last part matters: if implementation changes what future readers need to know, the task must say which docs changed or why they did not.

|                                       | Linear / Jira | GitHub Issues | Beads | Tusker |
| ------------------------------------- | :-----------: | :-----------: | :---: | :----: |
| Lives in your repo                    |       no      |    partial    |  yes  |   yes  |
| Plain markdown                        |       no      |      yes      | partial |  yes  |
| Renders in Obsidian without plugins   |       no      |       no      |   no  |   yes  |
| Risk-scaled spec and evidence layout  |    partial    |       no      |   no  |   yes  |
| Docs impact as part of the task       |       no      |       no      |   no  |   yes  |
| CLI for agents                        |       no      |    partial    |  yes  |   yes  |
| Installable agent skill               |       no      |       no      |   no  |   yes  |

If you have a sprint, a product manager, and 30 engineers, use Linear. Tusker is for the smaller, sharper setup where the repo is the source of truth.

## The V5 model

```text
Epic = workstream boundary + canon + success metrics
Task = executable change contract
Bug  = task(kind: bug)
Doc  = durable knowledge page under tusker/docs
```

The task contract separates five things that should not be blurred:

| Section | Job |
|---|---|
| Acceptance contract | What must be true |
| Deliverables | What artifacts or changes must exist |
| Verification plan | How truth will be checked |
| Evidence | Proof produced after execution |
| Knowledge delta | What durable understanding changed |

Tasks carry `risk`, `size`, `kind`, `priority`, `domains`, `doc_nodes`, `delegation`, `ai_assistance`, and `ai_tools`. Risk drives ceremony, not file count. A typo at `risk: low` needs one useful evidence line. A risky migration needs rollout, rollback, verification, and human review.

`domains` are broad human-facing areas like `schema`, `cli`, `docs`, or `runtime`. `doc_nodes` are exact documentation targets from `_config/docs-map.yaml`. If `doc_nodes` is non-empty, the docs close gate runs. For high-risk work, the task must include a `## Knowledge delta` table:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|

![Risk controls how much evidence and human review a task needs.](/generated/assets/repo-doc/developer-repository-repository-overview/docs/diagrams/03-risk-ladder.png)

## Pieces

**A skill bundle.** Install it into Claude Code, Codex, or any harness that loads `SKILL.md`. The skill teaches the agent the layout, lifecycle, evidence rules, and docs close gate. The skill alone is enough for most users.

**A Go CLI (`tusker`).** A helper binary for init, task creation, status changes, evidence, verification, docs checks, closing, validation, reindexing, docs publishing, and updates. If the CLI is unavailable, the agent can still edit the markdown directly, but the CLI is safer.

**An Obsidian vault layout.** Repo-local markdown, Bases views, dashboards, templates, and generated indexes. Open `tusker/` as a vault and it works.

**A symlink workspace.** One central Obsidian vault can symlink multiple repo-local `tusker/` folders. The repo remains authoritative; Obsidian is the reading and editing surface.

The dispatcher is experimental. It can pick up ready tasks and run Claude or Codex with concurrency caps, but the markdown layer and CLI are the stable core.

## Install

The skill alone is enough for most users. The CLI makes operations easier and gives agents atomic status changes, validation, evidence handling, and docs checks.

```sh
curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh
```

Install the agent skill for Claude Code and Codex:

```sh
curl -fsSL https://raw.githubusercontent.com/srv1n/tusker/main/scripts/install.sh | sh -s -- --codex-user --claude-user
```

After pulling or rebuilding Tusker, refresh the installed binary link and skill bundle:

```sh
tusker update
```

In any repo:

```sh
cd ~/code/my-app
tusker init --yes
```

You now have `~/code/my-app/tusker/` committed with the repo.

## Working with an agent

Ask Claude Code or Codex to log a task. The agent should:

1. Run `tusker list --type epic` to see existing workstreams.
2. Pick the right epic, or propose a new one if nothing fits.
3. Create a task with sensible defaults.
4. Print the ID and a one-line reason for the epic choice.

Primary V5 CLI:

```sh
tusker new task --epic MEM --title "Add cache invalidation" \
  --kind feature --size m --risk medium --domains runtime,docs \
  --doc-nodes reference/cache

tusker status MEM-T-0007 active
# work happens
tusker evidence MEM-T-0007 pr https://github.com/.../pull/42
tusker status MEM-T-0007 review
tusker verify MEM-T-0007 --by codex
tusker close MEM-T-0007 --by sarav
tusker validate
```

![A task moves from draft to ready, active, review, done, blocked, rework, or cancelled.](/generated/assets/repo-doc/developer-repository-repository-overview/docs/diagrams/04-lifecycle.png)

## Documentation

V5 docs live in `tusker/docs/`, not inside epic folders. They are durable knowledge pages with `schema: tusker.doc/v5`, `node`, `domains`, `audience`, `kind`, and publication metadata.

The full model is documented in [`docs/documentation-model.md`](/developer/documentation-model/). Read that when you need the philosophy, Diátaxis mapping, docs-map contract, freshness model, generated catalog, and publication flow.

Authoring flow:

1. Pick or create the epic that owns the workstream.
2. Create tasks that name impacted `domains` and exact `doc_nodes`.
3. Put durable doc changes in `tusker/docs/**`.
4. Fill `## Knowledge delta` before close when the task changes how future readers should understand the system.
5. Run the docs close gate before closing.

```sh
tusker docs model
tusker docs map
tusker docs catalog
tusker docs freshness --stale
tusker docs check MEM-T-0007
tusker docs apply MEM-T-0007 --node reference/cache --reason "Updated cache docs"
# or, when the no-op is intentional:
tusker docs waive MEM-T-0007 reference/cache --reason "No public behavior changed"
```

The static site remains generated output. Do not author in `site/src/content/docs/**`. Export/build with:

```sh
tusker docs export --site ./site
tusker docs build --site ./site
```

## Working without an agent

Use the tracker by hand:

```sh
cd ~/code/my-app
tusker list --type task
tusker status MEM-T-0007 active
```

You still get markdown tasks, docs, schema validation, Obsidian views, dashboards, and the CLI.

## Design principles

- Markdown is the source of truth. Frontmatter is the machine layer. Generated JSON is a cache.
- A task is an executable contract. A bug is `kind: bug`.
- Risk drives ceremony. Blast radius, not lines of code.
- Agents act, humans gate. Agents create, execute, and attach evidence. Humans verify `risk >= high`.
- Evidence is artifacts, not promises.
- Docs are part of done. Use `domains`, `doc_nodes`, and knowledge delta; do not let stale docs quietly survive.
- Progressive disclosure. The skill entry stays short. Reference files load only when needed.

## Read next

- [`skill/SKILL.md`](/user/start-here/agent-workflow/) — agent contract
- [`skill/references/COMMANDS.md`](/user/reference/commands/) — CLI surface
- [`skill/references/SCHEMA.md`](/user/reference/schema/) — V5 frontmatter
- [`skill/references/WORKFLOW.md`](/user/reference/workflow/) — lifecycle
- [`skill/references/DOCS_PUBLICATION.md`](/user/reference/docs-publication/) — docs nodes, close gate, and publication flow
- [`skill/references/RISK_AND_EVIDENCE.md`](/user/reference/risk-and-evidence/) — risk tiers and evidence
- [`docs/documentation-model.md`](/developer/documentation-model/) — docs philosophy, layout, Diátaxis, freshness, and publication model
