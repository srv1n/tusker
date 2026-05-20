# Tusker v5 final implementation pack

This pack converts Tusker from the current `Epic → Story/Bug/Doc` model into the v5 model:

```text
Epic → Task
Bug = task kind
Doc = durable knowledge page
doc_nodes = exact docs routing
Diátaxis = documentation authoring discipline
Markdoc = optional publishing/rendering layer
Media = transcript-backed evidence/docs/promo asset layer
Docs evals = proof that docs work for humans and agents
```

Use this pack as the handoff bundle for coding agents. Start with `BACKLOG_INDEX.md`, then read `SPEC/TUSKER_V5_FINAL_SPEC.md`.

## First batch

```text
ARC-T-0001
ARC-T-0002
ARC-T-0003
ARC-T-0004
ARC-T-0005
DOC-T-0001
VAL-T-0001
```

## Hard constraints

```text
1. Keep task contracts plain Markdown + YAML frontmatter.
2. Keep current task status in task frontmatter for Obsidian readability.
3. Put audit/runtime/generated state in _system/events, _system/runs, _system/generated.
4. Do not create per-task sidecar JSON mirrors.
5. Use controlled domains + doc_nodes, not freeform tags, for docs automation.
6. If doc_nodes are present, closure must resolve docs impact or record a waiver.
7. Diátaxis belongs on docs pages and docs-map nodes, not tasks.
8. Markdoc is optional publish/render infrastructure, not canonical source.
9. Every important video must have transcript, chapters, step list, claims, and source evidence.
```
