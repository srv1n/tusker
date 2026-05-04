---
title: "Operator Intervention"
description: "Use the public V5 task commands for manual intervention."
tusker:
  audience: "developer"
  publish_path: "developer/internals/operator-intervention"
  publish_section_title: "Internals"
  route: "/developer/internals/operator-intervention/"
  source_kind: "repo_doc"
  source_path: "skill/docs/OPERATOR_INTERVENTION.md"
  summary: "Use the public V5 task commands for manual intervention."
  tags:
    - "internals"
  updated: "2026-04-29"
---

# Operator Intervention

Use the public V5 task commands for manual intervention.

## Reset A Task

```bash
tusker status <TASK-ID> active --actor <name> --reason "<why work can resume>"
```

Use this when a task was blocked or rework is complete.

## Cancel A Task

```bash
tusker status <TASK-ID> cancelled --actor <name> --reason "<why cancelled>"
```

Cancellation is terminal. Create a new task if the work needs to continue under different scope.

## Verify And Close

```bash
tusker evidence <TASK-ID> log <file-or-url> --note "<what this proves>"
tusker docs check <TASK-ID>
tusker status <TASK-ID> review --actor <name>
tusker verify <TASK-ID> --by <verifier>
tusker close <TASK-ID> --by <reviewer>
tusker validate
```

Never delete prior evidence or work-log history. Append the new truth.
