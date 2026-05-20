# Schema

Frontmatter is the machine layer. The note body is the human layer. V7 makes **task** the execution unit and separates lifecycle state from runtime state.

## Note kinds

- `epic` — workstream boundary, success metrics, canon pointer.
- `task` — executable change contract. Bugs are tasks; keep `kind: task` and use a task classification field only if the installed CLI supports one.
- `gate` — explicit blocker or approval requirement.
- `evidence` — durable proof object required by proof mode.
- `attempt` — concise execution summary, not raw transcript.
- `decision` — durable decision/proposal record.
- `knowledge` / `domain` — project knowledge routes and canon.

Use `kind` as the V7 object discriminator. Legacy records may use `type`; normalize to effective kind before making workflow decisions.

## Task frontmatter

```yaml
---
schema: tusker.task/v7
kind: task
id: SAM-T-0008
project: tusker
epic: SAM
title: Add source move manifest proof
status: ready
readiness: ready
priority: p1
risk: medium
size: m
domains: [migration, proof]
proof_mode: card
proof_status: pending
proof_required:
  - focused_test
evidence_budget: 1
next_owner: agent
next_source: task
next_ref: SAM-T-0008
next_action: Satisfy acceptance and request review.
agent_action: continue
state_rev: 1
created_at: "2026-05-18T00:00:00Z"
updated_at: "2026-05-18T00:00:00Z"
---
```

Required for current work:

- `schema`, `kind`, `id`, `title`, `epic`, `status`, `readiness`
- `priority`, `risk`, `size`, `domains`
- `proof_mode`, `proof_status`
- `next_owner`, `next_action`
- `created_at`, `updated_at`

Recommended:

- `project`
- `proof_required`
- `evidence_budget`
- `blocked_by`, `blocks`
- `next_source`, `next_ref`
- `agent_action`
- `state_rev`

Do not add legacy mirror fields or empty optional lists just for decoration.

## Task status

```text
idea | backlog | ready | review | rework | done | cancelled | superseded
```

Meanings:

| Status | Meaning |
|---|---|
| `idea` | Captured but unshaped. |
| `backlog` | Shaped future work. |
| `ready` | Shaped current work that may be picked up if readiness/owner permit. |
| `review` | Implementation is ready for independent review, closeout, or human gate. |
| `rework` | Review found specific changes needed. |
| `done` | Accepted and closed. |
| `cancelled` | Intentionally abandoned. |
| `superseded` | Replaced by another task or decision path. |

Do not use `active` as a V7 task status. Runtime claim/running state belongs in leases/runtime stores.

## Readiness

```text
ready | blocked_by_gate | blocked_by_dependency | waiting_on_review | waiting_on_ci | held | done | cancelled | superseded
```

`held` plus `next_owner: human:*` is the current-compatible human-wait state and is a hard stop for agents.

## Ownership and action fields

```yaml
next_owner: agent | agent:<name> | reviewer:<name> | human:<name> | external:<service>
next_source: task | gate | dependency | review | closeout
next_ref: SAM-G-0001
next_action: Accept or waive SAM-G-0001.
agent_action: continue | request_review | stop_until_human_response | stop_until_external_response
```

Agent-runnable task:

```text
status in ready|rework
readiness == ready
next_owner == agent or agent:<name>
agent_action != stop_until_human_response
```

## Closeout fields

When machine work is complete and only human/external blockers remain, use the current-compatible human-wait shape:

```yaml
status: review
readiness: held
machine_status: complete
human_status: pending
closeout_status: machine_complete_waiting_for_human
next_owner: human:sarav
next_source: gates
next_ref: SAM-G-0001
next_action: Accept, waive, or reject the listed gates.
agent_action: stop_until_human_response
validation_fingerprint: sha256:...
last_validation_result: pass
```

Closeout checkpoints or packets should include:

- task ID;
- state fingerprint/state revision;
- validation command and result;
- machine/reviewer/human/external missing proof lists;
- open gates by owner;
- packet path;
- created_by and created_at.

## Gate frontmatter

```yaml
---
schema: tusker.gate/v7
kind: gate
id: SAM-G-0003
status: open
owner: human:sarav
gate_kind: verification
blocking: true
blocks:
  - SAM-T-0013
covers:
  - A1
action: Run manual smoke against the source manifest.
verification: Human accepts or waives the smoke result.
created_at: "2026-05-18T00:00:00Z"
updated_at: "2026-05-18T00:00:00Z"
---
```

Manual smoke, human signoff, approval, credentials, unavailable device/env, and external service dependencies should be gates unless the task explicitly makes them agent-owned.

## Proof fields

```yaml
proof_mode: inline | card | artifact | audit
proof_status: pending | partial | satisfied | waived
proof_required:
  - focused_test
  - broad_test
  - human_signoff
proof_required_owner:
  human_signoff: human:sarav
evidence_budget: 0
raw_artifacts_allowed: false
```

Default interpretation:

| Requirement | Default owner |
|---|---|
| `focused_test`, `broad_test`, `static_check`, `typecheck`, `lint` | machine |
| `human_signoff`, `manual_smoke`, `security_approval`, `release_approval`, `product_decision` | human unless explicitly agent-owned |
| `ci_green`, `external_probe` | external unless runnable in current environment |

## Task body sections

Minimal task:

```text
## Agent capsule
## Intent
## Acceptance contract
## Verification
## Evidence
```

Medium/high work may add:

```text
## Scope
## Deliverables
## Canon
## Code/system anchors
## Constraints
## Escalate if
## Knowledge delta
```

Critical work may add `## Rollback`.

Knowledge delta table:

| Topic | Before | After | Audience | Target knowledge |
|---|---|---|---|---|

## Generated/runtime data

Generated indexes, dashboards, runtime stores, leases, raw logs, and scratch output are not canonical task truth.

Do not hand-edit:

```text
_system/generated/**
.tusker-local/**
.tusker/scratch/** except to write raw debug artifacts
runner raw logs
```

## Hard invariants

Validator failures that matter early:

- `ID_COLLISION` — two notes share an ID.
- `PATH_MISMATCH` — path does not match ID/path convention.
- `PATH_ESCAPE` — note or artifact references a path outside the vault/repo boundary.
- `UNKNOWN_KIND` — note kind is not recognized.
- `INVALID_STATUS` — status is outside the V7 enum.
- `INVALID_READINESS` — readiness is outside the V7 enum.
- `AGENT_READY_OWNER_MISMATCH` — agent-ready queue contains non-agent-owned work.
- `HUMAN_WAIT_AGENT_ACTION_MISSING` — waiting-on-human task lacks stop action.
- `PROOF_GAP_UNOWNED` — missing proof cannot be classified by owner.
- `MISSING_KNOWLEDGE_DELTA` — high-risk durable understanding changed without knowledge delta.
- `DOCS_IMPACT_UNRESOLVED` — docs-impact gate has no apply/noop/waive.
