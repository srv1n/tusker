---
title: "Failure Classes"
description: "Runtime failures belong in runtime logs and evidence, not task frontmatter."
tusker:
  audience: "developer"
  publish_path: "developer/internals/failure-classes"
  publish_section_title: "Internals"
  route: "/developer/internals/failure-classes/"
  source_kind: "repo_doc"
  source_path: "skill/docs/FAILURE_CLASSES.md"
  summary: "Runtime failures belong in runtime logs and evidence, not task frontmatter."
  tags:
    - "internals"
  updated: "2026-04-29"
---

# Failure Classes

Runtime failures belong in runtime logs and evidence, not task frontmatter.

| Class | Meaning | Task Action |
|---|---|---|
| `transient` | network, rate limit, temporary tool outage | retry when infrastructure recovers |
| `deterministic` | repeatable test/type/assertion failure | keep task active or move to blocked with evidence |
| `blocked-by-human` | missing product/security/credential decision | `tusker status <TASK-ID> blocked --reason "<needed decision>"` |
| `budget-exceeded` | token/time/cost cap hit | split work or reduce scope before resuming |

When a failure changes what future readers need to know, add evidence and update the task's knowledge delta.
