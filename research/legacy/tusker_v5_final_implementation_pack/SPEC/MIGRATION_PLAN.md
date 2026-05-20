# Migration plan

## From current model

```text
Epic
├── Story  MEM-S-0001
├── Bug    MEM-B-0002
└── Doc    MEM-D-0003
```

## To v5

```text
Epic
└── Task   MEM-T-0001

Docs
└── durable pages in tusker/docs/ or configured repo docs path
```

## Story → Task

```text
MEM-S-0001.md → MEM-T-0001.md
schema: tusker.story/* → tusker.task/v5
type: story → task
change_type → kind
```

## Bug → Task kind bug

```text
MEM-B-0002.md → MEM-T-0002.md
type: bug → task
kind: bug
```

## D-note → Doc page

```text
tusker/epics/MEM/MEM-D-0003.md
  ↓
tusker/docs/memory/<slug>.md
```

`type: doc` remains. Work lifecycle fields are removed. Publication metadata moves to `_config/docs-map.yaml` unless it is truly local to the page.

## Frontmatter cleanup

Remove:
- record_id
- epic_record_id
- related_record_ids
- transitions
- dispatch_state
- claimed_by
- claimed_at
- run_attempts
- attested_by
- attested_at
- signoff_by
- signoff_at
- source_tasks
- docs enum

Keep:
- id
- title
- type
- kind
- epic
- status
- priority
- risk
- domains
- doc_nodes
- created
- updated

## Migration commands

```bash
tusker migrate v5 --dry-run
tusker migrate v5 --apply
tusker migrate rollback <migration-id>
```

Dry-run must produce:
- file renames
- field mappings
- D-note moves
- docs-map node proposals
- warnings requiring human review
