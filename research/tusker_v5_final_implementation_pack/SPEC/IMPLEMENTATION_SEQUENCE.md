# Implementation sequence

## Phase 1 — core schema and model

```text
ARC-T-0001 Rename story to task
ARC-T-0002 Collapse bug into task kind
ARC-T-0003 Trim task frontmatter and remove docs enum
ARC-T-0004 Define task body contract
ARC-T-0005 Move docs out of epic folders
DOC-T-0001 Add docs-map v5
VAL-T-0001 Enforce known domains/doc_nodes
```

## Phase 2 — migration

```text
MIG-T-0001 Migration dry-run/report
MIG-T-0002 Story S IDs → Task T IDs
MIG-T-0003 Bug B files → task kind bug
MIG-T-0004 D-notes → docs pages and docs-map
MIG-T-0005 Frontmatter cleanup
MIG-T-0006 Sample vault/fixtures
MIG-T-0007 Rollback
```

## Phase 3 — closure gates

```text
DOC-T-0004 Docs impact hook
DOC-T-0005 Knowledge delta parser
VAL-T-0002 Docs impact unresolved gate
VAL-T-0003 Structured knowledge delta validator
VAL-T-0006 Risk-tier section validation
CLI-T-0001 Trim command surface
CLI-T-0002 Task lifecycle commands
CLI-T-0003 Docs commands
```

## Phase 4 — skill/templates/Obsidian

```text
SKL-T-0001 Root skill rewrite
SKL-T-0002 Skill references
SKL-T-0003 Templates
SKL-T-0004 Diátaxis guidance
OBS-T-0001 Dashboards
OBS-T-0002 Docs/media/eval views
```

## Phase 5 — docs/media/evals/publishing

```text
DOC-T-0002 Diátaxis doc template
DOC-T-0003 Agent docs templates
DOC-T-0006 Docs catalog
DOC-T-0007 llms generation
MED-T-0001 media-map
MED-T-0002 video companion
MED-T-0004 promo claim ledger
EVAL-T-0001 docs eval schema
EVAL-T-0002 runner skeleton
MRK-T-0001 Markdoc spike
```

## Phase 6 — daemon

```text
ORC-T-0001 Daemon task scheduling
ORC-T-0002 status + events + runs
ORC-T-0003 doc_node locks
ORC-T-0005 close gate enforcement
```
