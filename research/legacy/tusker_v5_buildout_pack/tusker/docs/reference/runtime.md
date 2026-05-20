---
schema: tusker.doc/v5
id: reference/runtime
title: Runtime state, events, and generated caches
type: doc
node: reference/runtime
audience: developer
kind: canon
domains: [runtime, workflow]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/runtime
publish_description: Current-state frontmatter, audit events, runs, and generated caches.
created: 2026-04-29
updated: 2026-04-29
---

# Runtime state, events, and generated caches


Tusker v5 keeps a very hard boundary between:

- current task truth
- audit history
- runtime attempt state
- generated caches

## Current truth

Current task state lives in the task Markdown frontmatter:

```yaml
status: draft | ready | active | blocked | review | rework | done | cancelled
```

## Audit history

Append-only event files:

```text
tusker/_system/events/YYYY-MM.jsonl
```

Use them for:

- who changed what
- when a close or waiver happened
- daemon activity
- migration reports
- review events

Do **not** require them to reconstruct what a task currently is.

## Runtime state

```text
tusker/_system/runs/<TASK-ID>/<attempt>.json
```

Use this for daemon/runner state that is not user-authored.

## Generated caches

```text
tusker/_system/generated/*
```

These are disposable products of reindex/export/validation.
