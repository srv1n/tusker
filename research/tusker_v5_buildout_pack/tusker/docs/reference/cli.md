---
schema: tusker.doc/v5
id: reference/cli
title: Tusker v5 CLI surface
type: doc
node: reference/cli
audience: developer
kind: reference
domains: [cli, workflow]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/cli
publish_description: Primary v5 CLI surface and compatibility aliases.
created: 2026-04-29
updated: 2026-04-29
---

# Tusker v5 CLI surface


Tusker v5 intentionally keeps the primary command surface small.

## Primary commands

```text
tusker new epic
tusker new task
tusker new bug
tusker new doc

tusker status <id> <new>
tusker evidence <id> <kind> <path>

tusker docs check <id>
tusker docs apply <id>
tusker docs waive <id> <node>

tusker verify <id>
tusker close <id>
tusker validate
tusker reindex
```

## Command design rules

1. One obvious path for each common operation.
2. Compatibility aliases are allowed, but they must not dominate help output.
3. Help text should speak in the v5 model.
4. Old commands should either forward cleanly or emit an explicit deprecation warning.

## Compatibility aliases to keep during transition

```text
new-story       -> new task
new-bug         -> new bug
new-doc         -> new doc
set-status      -> status
attach-evidence -> evidence
review verify   -> verify
```

## Commands to avoid proliferating again

- no second parallel hierarchy of “review close approve finalize merge”
- no separate docs verbs for every publication subcase
- no status mutation hidden behind daemon-only behavior

## Notes

`docs check` is a dry-run agentic operation.
`docs apply` applies the accepted patch outcome.
`docs waive` records an explicit no-change decision for a node.
