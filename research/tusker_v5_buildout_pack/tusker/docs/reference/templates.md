---
schema: tusker.doc/v5
id: reference/templates
title: Template contract
type: doc
node: reference/templates
audience: developer
kind: reference
domains: [schema, workflow, obsidian]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/templates
publish_description: Epic, task, bug, and doc template contract.
created: 2026-04-29
updated: 2026-04-29
---

# Template contract


Tusker v5 uses four canonical template shapes:

- epic
- task
- bug
- doc

## Design intent

- Epic = workstream boundary and success metrics.
- Task = executable contract.
- Bug = task variant with stricter repro/regression shape.
- Doc = durable page, not work-in-flight.

## Task contract split

The hard split stays:

```text
human contract
---
execution output
```

That avoids two drifting files while still keeping the human-authored part readable.

## Required sections by tier

See `reference/validator`, but the template should always expose the full shape with comments
so an agent has room to fill it in when risk rises.
