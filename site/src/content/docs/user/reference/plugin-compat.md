---
title: "Obsidian Bases"
description: "Tusker writes task-native Bases views into _system/views/."
tusker:
  audience: "user"
  publish_path: "user/reference/plugin-compat"
  publish_section_title: "Reference"
  route: "/user/reference/plugin-compat/"
  source_kind: "repo_doc"
  source_path: "skill/references/PLUGIN_COMPAT.md"
  summary: "Tusker writes task-native Bases views into _system/views/."
  tags:
    - "reference"
  updated: "2026-04-30"
---

# Obsidian Bases

Tusker writes task-native Bases views into `_system/views/`.

## Views

- `Tasks.base`: all `type: task` items, grouped by status with Active/Review/Open views.
- `BugTasks.base`: `type: task` and `kind: bug`.
- `Epics.base`: active epics.
- `Docs.base`: docs publication pipeline.

## Expected Fields

- `status`: `draft, backlog, ready, active, blocked, review, rework, done, cancelled`
- `kind`: `feature, bug, refactor, migration, security, docs, chore, research, incident`
- `risk`: `low, medium, high, critical`
- `priority`: `p0, p1, p2, p3`

## Visual Rule Of Thumb

- `status == "active"`: active work.
- `status == "review"`: waiting for verification or close.
- `kind == "bug"`: show in the bug-task view.
