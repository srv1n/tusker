---
schema: tusker.epic/v5
id: NXT
title: Tusker v5 redesign and migration
type: epic
status: active
owner: tusker
summary: Move Tusker from the current story/bug/D-note model to the v5 task-contract model without breaking docs export.
doc_nodes: [spec/v5-overview, spec/migration-v5, reference/docs-pipeline, reference/runtime]
created: 2026-04-29
updated: 2026-04-29
---

# NXT · Tusker v5 redesign and migration

## Thesis

Tusker should stay repo-native and Obsidian-readable, but the tracker model must move from
human-PM vocabulary to agent-legible execution contracts.

## Scope

In:
- rename story → task
- collapse bugs into task kind
- keep status canonical in task frontmatter
- move durable docs into `tusker/docs/`
- introduce `_config/docs-map.yaml`
- add docs close gate and waiver flow
- keep existing docs export/publication working
- add explicit migration and smoke coverage

Out:
- rich web control plane
- multi-tenant SaaS workflow
- radical rewrite of the daemon before the data model is stable
- unlimited new validator rules on day one

## Success metrics

- a greenfield repo can bootstrap the v5 layout and create epic/task/doc notes
- a legacy repo migrates with deterministic ID/path rewrites and no silent metadata loss
- docs export still emits navigation, canon, and llms artifacts after migration
- `tusker close` blocks unresolved docs impact
- README, skill, templates, and CLI all speak the same v5 language
- smoke tests cover greenfield and migration flows

## Canon

- `../../docs/spec/v5-overview.md`
- `../../docs/spec/migration-v5.md`
- `../../docs/reference/docs-pipeline.md`
- `../../docs/reference/runtime.md`

## Task stack

1. `NXT-T-0001` — Freeze v5 architecture and compatibility boundary
2. `NXT-T-0002` — Introduce the v5 note model and parser support
3. `NXT-T-0003` — Replace templates, paths, bootstrap layout, and views
4. `NXT-T-0005` — Add docs-map routing and controlled vocab support
5. `NXT-T-0006` — Ship validator phase 1
6. `NXT-T-0004` — Build the explicit migration engine
7. `NXT-T-0007` — Add docs-impact hook, waiver flow, and close gate
8. `NXT-T-0008` — Keep docs export/publication alive under the new model
9. `NXT-T-0009` — Trim CLI surface and compatibility aliases
10. `NXT-T-0010` — Rewrite SKILL, README, and repo guidance
11. `NXT-T-0011` — Realign daemon/runtime with Markdown-first truth
12. `NXT-T-0012` — Add fixtures, migration smoke tests, and release checklist

## Dependency map

```text
0001 -> 0002, 0003, 0005
0002 -> 0004, 0006, 0009, 0011
0003 -> 0004, 0010
0005 -> 0004, 0006, 0007, 0008
0006 -> 0007, 0009
0004 -> 0008, 0012
0007 -> 0009, 0012
0008 -> 0012
0009 -> 0010, 0011
0011 -> 0012
```

## Open questions

- Whether doc publish metadata should live primarily in page frontmatter or node config after the transition window.
- How long legacy command aliases should remain before removal.
- Whether medium-risk knowledge delta should be warning-only or hard-required after the first release.
