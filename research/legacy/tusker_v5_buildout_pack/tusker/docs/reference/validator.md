---
schema: tusker.doc/v5
id: reference/validator
title: Validator rollout and failure codes
type: doc
node: reference/validator
audience: developer
kind: reference
domains: [workflow, schema, migration]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: reference/validator
publish_description: Validator rollout, failure codes, and section requirements.
created: 2026-04-29
updated: 2026-04-29
---

# Validator rollout and failure codes


Tusker v5 keeps validator ambition separate from validator enforcement.

## Phase 1 hard failures

### `UNKNOWN_DOC_NODE`
A task references a `doc_node` that does not exist in `_config/docs-map.yaml`.

### `DOCS_IMPACT_UNRESOLVED`
A task is being closed while one or more targeted documentation nodes have neither
an applied update, a verified no-op, nor an explicit waiver.

### `MISSING_KNOWLEDGE_DELTA`
At `risk >= high`, the task has no knowledge delta or the table is empty.

## Knowledge delta validation rule

The validator should reject tautologies like:

```text
Before: implementation existed
After: implementation updated
```

A valid row must express a real reader-facing delta, for example:

```text
Before: docs pages were epic-scoped D-notes under epics/<ACR>/
After: docs are global pages under tusker/docs/ with node routing from docs-map.yaml
```

or

```text
Before: none
After: close-time docs waivers are supported per target doc node
```

## Section requirement policy

Use warnings first for most missing sections, then harden later if the failure becomes common.
Do not ship a giant wall of hard rules just because the schema can express them.

## Suggested future hard rules

- `ID_COLLISION`
- `PATH_MISMATCH`
- `MISSING_ACCEPTANCE_ROWS`
- `MISSING_VERIFICATION_PLAN`
- `MISSING_DEMO_ASSET` for UI feature tasks at medium+
- `DOC_PAGE_MISSING_VERIFY_DATE`
