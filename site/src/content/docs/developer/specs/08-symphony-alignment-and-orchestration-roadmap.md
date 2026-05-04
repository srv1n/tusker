---
title: "08 - Orchestration Roadmap"
description: "Tusker V5 keeps Symphony-style orchestration as an internal runtime layer on top of a task-native markdown tracker."
tusker:
  audience: "developer"
  canonical_status: "historical"
  deprecated: true
  owner_epic: "ORC"
  publish_path: "developer/specs/08-symphony-alignment-and-orchestration-roadmap"
  publish_section_title: "Specs"
  route: "/developer/specs/08-symphony-alignment-and-orchestration-roadmap/"
  source_kind: "repo_doc"
  source_path: "docs/specs/08-symphony-alignment-and-orchestration-roadmap.md"
  summary: "Tusker V5 keeps Symphony-style orchestration as an internal runtime layer on top of a task-native markdown tracker."
  superseded_by: "/user/start-here/agent-workflow/"
  tags:
    - "specs"
  updated: "2026-04-29"
  verified_at: "2026-04-28"
---

# 08 - Orchestration Roadmap

Tusker V5 keeps Symphony-style orchestration as an internal runtime layer on top of a task-native markdown tracker.

## Locked Decisions

| Area | Decision |
|---|---|
| Tracker truth | V5 Markdown frontmatter and body |
| Work item | `type: task`; bug work is `kind: bug` |
| Docs | `tusker/docs/**` plus `_config/docs-map.yaml` |
| Public CLI | 11-command V5 surface |
| Runtime store | Internal store for attempts, turns, sessions, and event streams |
| Close gate | `verify` plus docs impact resolution before `close` |

## Runtime Principles

- Runtime dispatches only V5 tasks in `active` or `rework`.
- Runtime never treats old note types as current work.
- Runtime never stores lease or process state in task frontmatter.
- Runtime may write durable evidence and move a task to `review`.
- Runtime never closes a task.

## Roadmap Slices

| Slice | Goal | Acceptance |
|---|---|---|
| Workspace integrity | every run executes in its prepared workspace | cwd proof exists in runtime evidence |
| Policy enforcement | workflow risk and concurrency policy are enforced | skipped tasks carry clear reasons |
| Continuation | same task can continue across turns without losing context | attempts and turns are recorded |
| Review packet | successful implementation produces a proof packet | task evidence links to the packet |
| Supervisor policy | continue, resume, fork, or stop decisions are deterministic | decisions are visible in runtime artifacts |
| Extension bridge | optional tools are scoped by workflow/task policy | calls are audited and task-local |
| Operator docs | users understand the task workflow without runtime commands | docs teach V5 commands only |

## Public/Private Split

Users work through:

```bash
tusker init
tusker new task
tusker status
tusker evidence
tusker docs check
tusker verify
tusker close
tusker validate
```

Runtime observability is surfaced through generated dashboards, evidence files, and docs. It must not become another public workflow tree.

## Success Metrics

- A fresh vault creates V5 templates and views only.
- `validate` rejects current notes without V5 schemas.
- `reindex` emits task/doc/epic indexes only.
- Runtime sees task notes, not old work item types.
- README, skill docs, templates, and exported site all teach the same V5 workflow.
