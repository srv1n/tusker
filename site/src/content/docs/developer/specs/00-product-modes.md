---
title: "00 - Product Modes"
description: "Tusker V5 is one binary with one public workflow: a markdown-first task and docs tracker for agent-heavy software work."
tusker:
  audience: "developer"
  canonical_status: "historical"
  deprecated: true
  owner_epic: "ORC"
  publish_path: "developer/specs/00-product-modes"
  publish_section_title: "Specs"
  route: "/developer/specs/00-product-modes/"
  source_kind: "repo_doc"
  source_path: "docs/specs/00-product-modes.md"
  summary: "Tusker V5 is one binary with one public workflow: a markdown-first task and docs tracker for agent-heavy software work."
  superseded_by: "/user/start-here/agent-workflow/"
  tags:
    - "specs"
  updated: "2026-04-29"
  verified_at: "2026-04-28"
---

# 00 - Product Modes

Tusker V5 is one binary with one public workflow: a markdown-first task and docs tracker for agent-heavy software work.

## Modes

| Mode | Purpose | Public surface |
|---|---|---|
| Skill mode | Agents read the Tusker skill and update the vault through the CLI. | `SKILL.md` + V5 CLI |
| Tracker mode | Humans or agents manage epics, tasks, bug tasks, docs, evidence, verification, and close gates. | V5 CLI |
| Runtime mode | Internal orchestration can execute active tasks and record runtime artifacts. | No extra public commands |

Runtime mode is implementation. It must not expand the normal user command set.

## Public CLI

The supported public commands are:

1. `tusker init`
2. `tusker new`
3. `tusker list`
4. `tusker status`
5. `tusker evidence`
6. `tusker docs`
7. `tusker verify`
8. `tusker close`
9. `tusker validate`
10. `tusker reindex`
11. `tusker update`

There are no extra public workflow command trees beyond this list.

## Boundaries

| Concern | Owner |
|---|---|
| Current task status | Markdown frontmatter |
| Acceptance, deliverables, verification, evidence | Markdown body |
| Durable docs | `tusker/docs/**` plus `_config/docs-map.yaml` |
| Generated indexes and dashboards | `_system/generated/**` |
| Runtime attempts, sessions, event streams | Runtime store and artifacts |

## Package Shape

`go build -o dist/tusker ./cmd/tusker` produces the binary. The binary embeds the V5 skill assets, templates, Bases views, repo-contract snippets, docs-map defaults, and workflow defaults.

`tusker init --yes` is the single setup path. It creates or refreshes a V5 vault, writes V5 workflow/templates/views, installs repo pointers, and reindexes.

## Design Rules

- Keep the tracker useful without runtime orchestration.
- Keep runtime state out of frontmatter.
- Keep the public CLI small.
- Treat docs impact as part of close, not cleanup.
- Reject old note models outright; V5 is the only normal model.
