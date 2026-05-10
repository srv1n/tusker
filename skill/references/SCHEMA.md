# Schema

Frontmatter is the machine layer. The note body is the human layer. V5 makes **task** the execution unit and keeps docs as durable knowledge pages.

## Note types

- `epic` — workstream boundary, canon pointer, success metrics. Lives at `epics/<ACR>/<ACR>.md`.
- `task` — executable change contract. Lives at `epics/<ACR>/<ACR>-T-NNNN.md`.
- `doc` — durable knowledge page. Lives under `docs/<domain>/<slug>.md`.
- `note` — free-form vault page with no lifecycle.

Bug is not a separate execution type in V5. Use `type: task` and `kind: bug`.

## Task frontmatter

```yaml
---
schema: tusker.task/v5
id: NXT-T-0002
title: Introduce the v5 note model
type: task
kind: migration
epic: NXT
status: blocked
priority: p0
risk: high
size: l
domains: [schema, migration]
doc_nodes: [spec/v5-overview, spec/v5-adoption]
blocked_by: [NXT-T-0001]
block_reason: Waiting for the note model migration to land.
delegation: execute
ai_assistance: heavy
ai_tools: [codex]
created: 2026-04-29
updated: 2026-04-29
---
```

Required before active work:

- `schema`, `id`, `title`, `type`, `kind`, `epic`, `status`
- `size`, `risk`, `priority`
- `domains`
- `created`, `updated`
- `ai_assistance`

Recommended:

- `doc_nodes`
- `delegation`
- `blocked_by`, `blocks`
- `block_reason` when `status: blocked`
- `ai_tools`
- `tags`

Do not add legacy tracker fields such as `record_id`, `schema_version`, `requester`, attestation/signoff fields, record-id mirror fields, or empty optional lifecycle fields. V5 uses the stable `id` plus generated runtime stores; old mirror metadata is noise.

Runtime/dispatch fields:

- Runtime state belongs in generated/runtime stores, not task frontmatter.

Review fields:

- `verified_by`, `verified_at`
- `verification_summary`
- `closed_by`, `closed_at`
- `close_summary`
- `docs_resolution`

## Task body contract

Task bodies are risk-scaled. Keep the first screen small and put durable truth
in summaries, evidence links, and knowledge deltas. Do not keep an append-only
work diary by default; status transitions, verification fields, evidence
summaries, and review packets carry the durable audit trail.

Every task starts with:

```text
## Agent capsule
## Intent
## Acceptance contract
## Evidence
```

Medium tasks add:

```text
## Scope
## Deliverables
## Verification plan
```

High and critical tasks add:

```text
## Canon
## Code/system anchors
## Constraints
## Escalate if
## Knowledge delta
## Verification log
```

Critical tasks also add `## Rollback`.

Use a structured knowledge delta table:

| Topic | Before | After | Audience | Target doc nodes |
|---|---|---|---|---|

`MISSING_KNOWLEDGE_DELTA` should be a hard failure for `risk >= high` when the task changes durable understanding.

## Doc frontmatter

```yaml
---
schema: tusker.doc/v5
id: spec/v5-overview
title: Tusker v5 implementation spec
type: doc
node: spec/v5-overview
audience: developer
mode: explanation
agent_layer: capsule
kind: canon
domains: [schema, workflow, docs]
source_of_truth: [research/tusker_v5_implementation_spec.md]
stale_when_paths: [cmd/tusker/**, skill/**]
canonical_status: approved
last_verified_at: 2026-04-29
publish: true
publish_lane: internal
publish_path: spec/v5-overview
publish_description: Transitional implementation spec for Tusker v5.
created: 2026-04-29
updated: 2026-04-29
---
```

Required:

- `schema`, `id`, `title`, `type`, `node`, `audience`, `kind`
- `domains`
- `created`, `updated`

Recommended:

- `mode`: `tutorial | how-to | reference | explanation`
- `agent_layer`: `none | capsule | standalone`
- `source_of_truth`
- `stale_when_paths`
- `canonical_status`: `draft | approved | deprecated | historical`
- `last_verified_at`
- `owner_epic`
- `superseded_by`
- `publish`, `publish_lane`, `publish_path`, `publish_description`, `publish_order`, `redirect_from`
- `related`, `tags`

Docs should be authored under `tusker/docs/**`. Published site files are generated output.

## Status enums

### `epic.status`

`draft`, `backlog`, `ready`, `blocked`, `active`, `review`, `rework`, `done`, `cancelled`

### `task.status`

`draft`, `backlog`, `ready`, `blocked`, `active`, `review`, `rework`, `done`, `cancelled`

Status meanings:

| Status | Meaning | Pickable? |
|---|---|---|
| `draft` | Not fully shaped. | No |
| `backlog` | Shaped future work, outside the current release/sprint. | No |
| `ready` | Shaped current work. | Yes, only when unblocked |
| `blocked` | Current work waiting on Tusker dependencies or an external blocker. | No |
| `active` | Claimed and in progress. | Already claimed |
| `review` | Implementation is ready for verification. | No |
| `rework` | Verification or review found more work. | Yes, only when unblocked |
| `done` | Accepted and closed. | No |
| `cancelled` | Intentionally abandoned. | No |

When `status: blocked`, expose `blocked_by` for Tusker task dependencies and `block_reason` for the human/external reason. Do not hide blockers in body text only.

### `doc.status`

`draft`, `review`, `approved`, `published`, `archived`

Status transitions are append-only in `transitions[]`. Use `tusker status`; never hand-edit transitions.

## Other enums

- `kind`: `feature`, `bug`, `refactor`, `migration`, `security`, `docs`, `chore`, `research`, `incident`
- `risk`: `low`, `medium`, `high`, `critical`
- `size`: `s`, `m`, `l`, `xl`
- `priority`: `p0`, `p1`, `p2`, `p3`
- `delegation`: `execute`, `explore`, `escalate`
- `ai_assistance`: `none`, `light`, `moderate`, `heavy`
- `ai_tools`: array of strings
- `domains`: broad work/doc areas
- `doc_nodes`: exact targets from `_config/docs-map.yaml`
- `doc.audience`: `developer`, `user`, `operator`, `release`, `support`, `agent`, `internal`
- `doc.mode`: `tutorial`, `how-to`, `reference`, `explanation`
- `doc.agent_layer`: `none`, `capsule`, `standalone`
- `doc.kind`: `canon`, `companion`, `guide`, `runbook`, `release`, `support`, or a local extension

## Linking conventions

- Task to epic: `epic: NXT` or `epic: "[[NXT]]"`; prefer the repo's existing convention.
- Task to task: `blocked_by`, `blocks`, `related`.
- Task to docs: `doc_nodes` for automation targets; body links for human reading.
- Doc identity: `node` is the stable docs node, e.g. `reference/commands`.
- Canon: epic points to its canon doc or repo spec; task `## Canon` links to the exact source.

## Generated indexes

`_system/generated/` is derived. Regenerate with `tusker reindex`.

Common indexes:

- `epics.index.json`
- `tasks.index.json`
- `docs.index.json`
- `links.index.json`
- `publication.index.json`
- `dashboard.json`
- `summary.json`

Never hand-edit generated indexes.

## Published docs manifests

`tusker docs export` writes:

- `site/src/generated/content-manifest.json`
- `site/src/generated/canon-manifest.json`
- `site/public/canon-manifest.json`
- `site/src/generated/routes-removed.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

Agents should read `site/public/canon-manifest.json` before treating old docs as current truth.

## Hard invariants

Validator failures that matter early in V5:

- `ID_COLLISION` — two notes share an `id`
- `PATH_MISMATCH` — path does not match the canonical ID/path rule
- `PATH_ESCAPE` — a note references a path outside the vault
- `UNKNOWN_DOC_NODE` — a task names a `doc_nodes` entry missing from `_config/docs-map.yaml`
- `DOCS_IMPACT_UNRESOLVED` — docs-impact gate has neither applied changes nor explicit waiver
- `MISSING_KNOWLEDGE_DELTA` — high-risk task changed durable understanding without a knowledge delta
- `CONFIG_INVALID` — docs-map schema, node path, Diátaxis metadata, or freshness metadata is invalid

## Filename rules

- Epic index: `epics/<ACR>/<ACR>.md`
- Task: `epics/<ACR>/<ACR>-T-NNNN.md`
- Doc: `docs/<domain>/<slug>.md`
