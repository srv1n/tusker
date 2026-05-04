# Tusker v5 buildout pack

This pack is the handoff bundle for moving the current Tusker repo from the existing
`story / bug / doc` model to the v5 transitional model:

- `task` replaces `story`
- `bug` becomes `type: task, kind: bug`
- task `status` stays canonical in frontmatter
- per-task sidecars stay out
- docs remain `type: doc`, but move into `tusker/docs/`
- `domains + doc_nodes` become the controlled routing layer
- docs are a close gate
- `knowledge delta` becomes the bridge between implementation and docs
- the CLI surface is trimmed to the small set of commands that matter

This pack is intentionally written in the **target v5 contract shape** rather than the current
Tusker schema. That is deliberate. The first implementation tasks are the schema, template,
and migration tasks that let the repo catch up to the pack.

## Contents

```text
tusker/
├── _config/
│   ├── WORKFLOW.md
│   └── docs-map.yaml
├── _system/templates/
│   ├── epic.md
│   ├── task.md
│   ├── bug.md
│   └── doc.md
├── docs/
│   ├── spec/
│   │   ├── v5-overview.md
│   │   └── migration-v5.md
│   └── reference/
│       ├── cli.md
│       ├── docs-pipeline.md
│       ├── obsidian.md
│       ├── runtime.md
│       ├── skill.md
│       ├── templates.md
│       └── validator.md
└── epics/NXT/
    ├── NXT.md
    └── NXT-T-0001.md ... NXT-T-0012.md
```

## Recommended implementation order

```text
NXT-T-0001  Freeze v5 architecture and compatibility boundary
NXT-T-0002  Introduce the v5 note model and parser support
NXT-T-0003  Replace templates, paths, bootstrap layout, and views
NXT-T-0005  Add docs-map routing and controlled vocab support
NXT-T-0006  Ship validator phase 1
NXT-T-0004  Build the explicit migration engine
NXT-T-0007  Add docs-impact hook, waiver flow, and close gate
NXT-T-0008  Keep docs export/publication alive under the new model
NXT-T-0009  Trim the CLI and add compatibility aliases
NXT-T-0010  Rewrite skill, README, and repo-facing guidance
NXT-T-0011  Realign daemon/runtime state with the Markdown-first model
NXT-T-0012  Add fixtures, migration smoke tests, and release checklist
```

## Parallelism

Safe early parallel lanes:

- `0002` and `0003` can run in parallel after `0001`.
- `0005` can run in parallel with `0002`.
- `0006` can start once `0002 + 0005` exist.
- `0004` should wait for `0002 + 0003 + 0005`.
- `0008` should wait for `0004 + 0005`.
- `0012` should be the final hardening lane.

## Important note on compatibility

The current repo still exposes the older model in public docs and code:
repo-local Markdown in `tusker/`, `story / bug / doc` note types, big frontmatter,
and a docs publication pipeline centered on D-note metadata. The implementation plan in
this pack assumes that reality rather than pretending the codebase is already clean.
