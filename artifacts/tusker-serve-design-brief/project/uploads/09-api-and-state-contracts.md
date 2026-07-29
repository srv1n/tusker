---
schema: tusker.design-note/v1
kind: spec
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[04-plan-and-authorization]]"
  - "[[05-deliveries-and-delivery-detail]]"
  - "[[07-settings-and-runner-policy]]"
  - "[[08-daemon-diagnostics-and-recovery]]"
  - "[[10-guardrails-authority-and-confirmations]]"
tags:
  - tusker/api
  - tusker/state
  - tusker/ux
---

# API and state contracts

## Scope

This document separates:

- **Current API** — endpoints exposed by the Serve implementation and consumed
  by the existing UI;
- **Target product API** — additions/normalizations required by this UX.

Existing endpoints remain compatibility surfaces until migrated. Target
endpoints should be versioned under `/api/v1` or expose an explicit schema
version in every response. Do not make the frontend reconstruct product state
by scraping multiple raw records.

## General API rules

### Project scoping

New project APIs use path scoping:

`/api/v1/projects/{projectId}/...`

Compatibility query scoping (`?project=`) remains accepted during migration.
The server validates that object IDs belong to the scoped project.

### Response envelope

Projections:

```json
{
  "schema": "tusker.project-today/v1",
  "generated_at": "2026-07-28T10:30:00Z",
  "project_id": "tusker",
  "source_revision": "opaque-revision",
  "data": {}
}
```

Mutations:

```json
{
  "schema": "tusker.action-result/v1",
  "ok": true,
  "receipt_id": "RCP-...",
  "object_revision": "opaque-revision",
  "effective": {},
  "reason": "Delivery started"
}
```

### Optimistic concurrency

Every mutable durable object exposes an opaque revision. Mutations send:

- `If-Match: <revision>` or an equivalent typed `expected_revision`;
- `Idempotency-Key` for start, retry, integration, promotion, release, and
  override;
- exact reviewed identity for plan authorization.

Stale mutation returns `409 STALE_REVISION` with current revision and a bounded
semantic change summary. The client must not resubmit invisibly.

### Errors

```json
{
  "schema": "tusker.error/v1",
  "code": "RUNNER_UNHEALTHY",
  "message": "The Build routine runner is unavailable.",
  "object": {"kind": "runner_profile", "id": "routine"},
  "retryable": false,
  "remedies": [
    {"kind": "navigate", "label": "Review runner", "target": "..."}
  ],
  "details": {}
}
```

Required stable classes:

- `VALIDATION_FAILED`
- `STALE_REVISION`
- `AUTHORITY_REQUIRED`
- `POLICY_REFUSED`
- `AUTOMATION_DISABLED`
- `DAEMON_OFFLINE`
- `RUNNER_UNHEALTHY`
- `CAPABILITY_UNAVAILABLE`
- `INFRASTRUCTURE_BLOCKED`
- `ATTEMPT_EXHAUSTED`
- `DEPENDENCY_BLOCKED`
- `GATE_UNSATISFIED`
- `REGISTRATION_CONFLICT`
- `WORKSPACE_UNSAFE`
- `RESOURCE_EXHAUSTED`
- `NOT_FOUND`
- `CONFLICT`
- `INTERNAL`

Messages are product-safe. Technical details are structured and bounded.

### No silent side effects

GET, HEAD, route preview, plan review, preflight, doctor, census, impact
preview, and capability discovery are read-only. They cannot claim, dispatch,
arm, enable, move refs, satisfy gates, release, or spend.

## Current API inventory

These are current implementation surfaces as of this pack.

### Current reads

| Endpoint | Purpose |
|---|---|
| `GET /api/delivery/plans?project=` | Discover delivery plan files/inbox |
| `GET /api/delivery/review?plan=&project=` | Validate and project one plan review |
| `GET /api/daemon` | Daemon/Serve status |
| `GET /api/projects` | Registered project summaries |
| `GET /api/needs?project=` | Derived attention items |
| `GET /api/digest` | Cross-project digest |
| `GET /api/summary?project=` | Project summary |
| `GET /api/morning-brief?project=&date=` | Scheduled-promotion brief |
| `GET /api/factory-operations?project=` | Factory outcome/control projection |
| `GET /api/runs?project=&all=` | Current/recent runtime records |
| `GET /api/runs/{taskId}` | Run detail for a task |
| `GET /api/epics?project=` | Epic summaries |
| `GET /api/waves?project=` | Wave summaries |
| `GET /api/waves/{waveId}` | Wave detail |
| `GET /api/gates?task=` | Gate list |
| `GET /api/gates/{gateId}` | Gate detail |
| `GET /api/evidence?task=` | Evidence list |
| `GET /api/evidence/{evidenceId}` | Evidence detail |
| `GET /api/decisions?epic=` | Decision list |
| `GET /api/decisions/{decisionId}` | Decision detail |
| `GET /api/feedback` | Feedback list |
| `GET /api/feedback/{feedbackId}` | Feedback detail |
| `GET /api/attempts?task=` | Attempt list |
| `GET /api/attempts/{attemptId}` | Attempt detail |
| `GET /api/tasks?project=` | Task capsules |
| `GET /api/tasks/{taskId}` | Task detail |
| `GET /api/docs?project=` | Legacy document list |
| `GET /api/docs/{repoPath}` | Legacy document detail |
| `GET /api/docgraph?project=` | Knowledge corpus graph |
| `GET /api/docgraph/doc?project=&subject=` | Rendered corpus document |
| `GET /api/roster` | Worker/run roster |
| `GET /api/review/batch?project=` | Review-ready task batch |
| `GET /api/stream` | Server-sent event stream |

### Current mutations

| Endpoint | Purpose |
|---|---|
| `POST /api/delivery/start?project=` | Revalidate, import, and arm exact reviewed plan |
| `POST /api/projects` | Register a repository/vault |
| `POST /api/projects/{id}/automation` | Enable/disable project automation |
| `POST /api/projects/{id}/settings` | Currently limited workspace/concurrency update |
| `POST /api/projects/{id}/refresh` | Targeted project refresh |
| `POST /api/runs/{taskId}/redrive` | Retry/requeue according to runtime policy |
| `POST /api/runs/{taskId}/interrupt` | Interrupt a live run |
| `POST /api/runs/{taskId}/acknowledge` | Retire a settled failed run from attention |
| `POST /api/tasks/{id}/status` | Guarded lifecycle transition |
| `POST /api/tasks/{id}/run` | Queue/direct task action where supported |
| `POST /api/tasks/{id}/discard` | Dry-run or apply discard |
| `POST /api/tasks/{id}/close` | Guarded objective close |
| `POST /api/tasks/{id}/land` | Guarded task landing |
| `POST /api/waves/{id}/land` | Guarded wave landing |
| `POST /api/gates/{id}/satisfy` | Satisfy with required evidence |
| `POST /api/gates/{id}/waive` | Waive with required reason/authority |
| `POST /api/gates/{id}/obsolete` | Obsolete with reason |
| `POST /api/evidence` | Add evidence |
| `POST /api/feedback` | Add feedback |
| `POST /api/daemon/start` | Start daemon service path |
| `POST /api/daemon/stop` | Stop daemon service path |
| `POST /api/daemon/resume` | Resume daemon |
| `POST /api/daemon/limits` | Change daemon limits |
| `PUT /api/docgraph/doc?project=&subject=` | Optimistic document save |

The current UI also contains mock-only settings/frontmatter behavior. It must
not be treated as a persisted API contract.

## Target projection APIs

### App shell and Today

| Method and endpoint | Schema | Notes |
|---|---|---|
| `GET /api/v1/today` | `tusker.global-today/v1` | Cross-project attention, working deliveries, recent delivery |
| `GET /api/v1/projects/{id}/today` | `tusker.project-today/v1` | Project release-manager brief |
| `GET /api/v1/search?q=&project=&types=` | `tusker.search-results/v1` | Product objects first; exact IDs supported |
| `GET /api/v1/notifications` | `tusker.notifications/v1` | Delivered notifications, not underlying attention state |
| `POST /api/v1/notifications/{id}/read` | action result | Marks delivery receipt read only |

`project-today/v1` contains at most:

- `summary`;
- `health`;
- `attention[]`;
- `active_deliveries[]`;
- `recent_deliveries[]`;
- `primary_action`;
- `freshness`.

### Projects and onboarding

| Method and endpoint | Purpose |
|---|---|
| `POST /api/v1/projects/discover` | Read-only repository/Tusker/Git/harness discovery |
| `POST /api/v1/projects` | Register after reviewing discovery |
| `GET /api/v1/projects` | Registry with health and attention count |
| `GET /api/v1/projects/{id}` | Identity and effective policy summary |
| `DELETE /api/v1/projects/{id}/registration` | Remove registry entry; never delete repo/vault |
| `GET /api/v1/projects/{id}/migration` | Compatibility/migration preview |
| `POST /api/v1/projects/{id}/migration` | Apply exact confirmed migration |
| `POST /api/v1/projects/{id}/refresh` | Targeted reconciliation |

### Plans

| Method and endpoint | Purpose |
|---|---|
| `GET /api/v1/projects/{id}/plans` | Indexed plan inbox; no path entry |
| `GET /api/v1/projects/{id}/plans/{planId}` | Full product review projection |
| `GET /api/v1/projects/{id}/plans/{planId}/diff?from=` | Semantic plan diff |
| `POST /api/v1/projects/{id}/plans/draft` | Request a planner draft; no execution |
| `POST /api/v1/projects/{id}/plans/{planId}/preflight` | Read-only fresh preflight |
| `POST /api/v1/projects/{id}/plans/{planId}/start` | Exact reviewed authorization/import |
| `GET /api/v1/projects/{id}/plans/{planId}/receipt` | Authorization/history |
| `POST /api/v1/projects/{id}/plans/{planId}/archive` | Archive inbox entry only |

Start request:

```json
{
  "expected_plan_revision": "opaque",
  "reviewed_identity": "opaque",
  "actor": "human:<local-authority>",
  "automation_behavior": "use_current_project_policy"
}
```

The server owns the fingerprint. No user-entered confirmation hash.

### Deliveries

| Method and endpoint | Purpose |
|---|---|
| `GET /api/v1/projects/{id}/deliveries` | Active/history delivery summaries |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}` | Artifact-first detail |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}/requirements` | Requirement/proof coverage |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}/dag` | Task graph projection |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}/timeline` | Typed product events |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}/artifacts` | Compact artifact index |
| `GET /api/v1/projects/{id}/deliveries/{deliveryId}/integration` | Census/gates/promotion/release |
| `POST /api/v1/projects/{id}/deliveries/{deliveryId}/pause` | Pause new work |
| `POST /api/v1/projects/{id}/deliveries/{deliveryId}/resume` | Resume after fresh readback |
| `POST /api/v1/projects/{id}/deliveries/{deliveryId}/cancel` | Impact preview + explicit cancel |

### Tasks, reviews, gates, proof

Compatibility task/gate APIs remain, but product-normalized additions are:

| Method and endpoint | Purpose |
|---|---|
| `GET /api/v1/projects/{id}/tasks/{taskId}` | Product-first task detail |
| `GET /api/v1/projects/{id}/tasks/{taskId}/technical` | Attempts, workspace, refs, exact proof |
| `POST /api/v1/projects/{id}/tasks/{taskId}/rework` | Typed findings/reason |
| `POST /api/v1/projects/{id}/tasks/{taskId}/retry` | Policy-aware retry |
| `POST /api/v1/projects/{id}/tasks/{taskId}/discard/preview` | Dependency and receipt impact |
| `POST /api/v1/projects/{id}/tasks/{taskId}/discard` | Confirm exact preview |
| `POST /api/v1/projects/{id}/gates/{gateId}/respond` | Typed human decision/evidence |
| `POST /api/v1/projects/{id}/tasks/{taskId}/human-override/preview` | Dedicated authority/risk preview |
| `POST /api/v1/projects/{id}/tasks/{taskId}/human-override` | Exact revision-bound override |

No generic UI endpoint sets task status to `done`.

### Knowledge

| Method and endpoint | Purpose |
|---|---|
| `GET /api/v1/projects/{id}/knowledge` | Canonical index/filter/search |
| `GET /api/v1/projects/{id}/knowledge/graph` | Scoped relation graph |
| `GET /api/v1/projects/{id}/knowledge/{subject}` | Rendered document + backlinks |
| `PUT /api/v1/projects/{id}/knowledge/{subject}` | Revision-aware save |
| `POST /api/v1/projects/{id}/knowledge/{subject}/validate` | Read-only body/frontmatter/link validation |
| `POST /api/v1/projects/{id}/knowledge/{subject}/rename/preview` | Backlink impact |
| `POST /api/v1/projects/{id}/knowledge/{subject}/rename` | Atomic confirmed rename/update |

The existing docgraph save `409` and `422` behavior should carry forward.

## Settings APIs

### Read and effective projection

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/settings` | App defaults, provenance, supported controls |
| `GET /api/v1/projects/{id}/settings` | Effective project settings and sources |
| `GET /api/v1/projects/{id}/settings/schema` | Versioned field/control/validation metadata |
| `POST /api/v1/projects/{id}/settings/impact` | Read-only consequence and affected active authorizations |
| `PATCH /api/v1/projects/{id}/settings` | Apply validated narrow changes |
| `POST /api/v1/projects/{id}/settings/reset` | Remove named override |
| `GET /api/v1/configuration/sources` | Global/project/local file/source health |

Settings are nested but patchable by stable paths:

```json
{
  "automation": {
    "enabled": false,
    "dispatch_scope": "armed_waves",
    "completion_reactor": "disabled"
  },
  "capacity": {
    "max_active_runs_per_project": 2,
    "max_live_worktrees": 4
  },
  "roles": {
    "plan": "profile:planner",
    "build_routine": "profile:routine",
    "build_hard": "profile:hard",
    "independent_review": "profile:review"
  },
  "scheduled_promotion": {
    "version": 1,
    "mode": "disabled",
    "release": {"authorized": false},
    "model_triage": {"authorized": false}
  }
}
```

Each leaf returns:

```json
{
  "effective": 2,
  "source": "project",
  "source_path": "tusker.yaml",
  "overridden": false,
  "editable": true,
  "requires_confirmation": false
}
```

### Runner/profile APIs

| Endpoint | Read-only? | Purpose |
|---|---:|---|
| `GET /api/v1/runners/catalog` | Yes | Discovered provider-neutral models, efforts, versions, provenance |
| `POST /api/v1/runners/discover` | Yes | Fresh safe probe; no dispatch |
| `GET /api/v1/runners/profiles` | Yes | App profiles and versions |
| `POST /api/v1/runners/profiles` | No | Create validated profile |
| `PATCH /api/v1/runners/profiles/{id}` | No | Version profile; preserve active references |
| `DELETE /api/v1/runners/profiles/{id}` | No | Refuse while referenced unless replacement selected |
| `GET /api/v1/projects/{id}/runner-setup` | Yes | Four semantic roles + provenance |
| `POST /api/v1/projects/{id}/runner-setup/preview` | Yes | Reconcile recommendations, preserve explicit choices |
| `POST /api/v1/projects/{id}/runner-setup` | No | Write machine-local role/profile mapping |
| `GET /api/v1/projects/{id}/runner-setup/route?...` | Yes | Explain role/profile/precedence; no claim |
| `GET /api/v1/projects/{id}/runners/health` | Yes | Preclaim-equivalent health |

### Permissions and notifications

| Endpoint | Purpose |
|---|---|
| `GET/PATCH /api/v1/settings/permissions` | Machine presets, locks, denylist, connectors |
| `GET/PATCH /api/v1/projects/{id}/settings/permissions` | Project narrowing/authorized overrides |
| `GET/PATCH /api/v1/settings/notifications` | Delivery channels and global policy |
| `GET/PATCH /api/v1/projects/{id}/settings/notifications` | Project overrides |
| `POST /api/v1/settings/notifications/test` | Test selected channel; no factory mutation |

## Diagnostics APIs

| Endpoint | Schema/purpose |
|---|---|
| `GET /api/v1/health` | App/runtime aggregate |
| `GET /api/v1/projects/{id}/health` | Project check summary |
| `GET /api/v1/daemon` | Ownership, liveness, build, capacity |
| `POST /api/v1/daemon/restart` | App-supervised restart with PID/readback |
| `POST /api/v1/daemon/pause` | Pause new background scheduling |
| `POST /api/v1/daemon/resume` | Resume after health readback |
| `GET /api/v1/capabilities` | Installed authoritative manifest |
| `GET /api/v1/projects/{id}/queue` | Frontier and wait reasons |
| `GET /api/v1/projects/{id}/runs` | Typed runtime summaries |
| `GET /api/v1/projects/{id}/runs/{runId}` | Liveness/process/attempt detail |
| `POST /api/v1/projects/{id}/runs/{runId}/interrupt` | Exact live run interrupt |
| `POST /api/v1/projects/{id}/runs/{runId}/retry` | Bounded retry |
| `POST /api/v1/projects/{id}/runs/{runId}/acknowledge` | Clear settled attention only |
| `GET /api/v1/projects/{id}/workspaces` | Worktree identity/lease/disk/registration |
| `POST /api/v1/projects/{id}/workspaces/{id}/cleanup/preview` | Preserve/impact check |
| `POST /api/v1/projects/{id}/workspaces/{id}/cleanup` | Exact confirmed cleanup |
| `POST /api/v1/projects/{id}/doctor` | Read-only fresh divergence sweep |
| `POST /api/v1/projects/{id}/doctor/repairs/preview` | Safe repair impact |
| `POST /api/v1/projects/{id}/doctor/repairs` | Apply selected repairs only |
| `GET /api/v1/projects/{id}/boarding-census` | Stable per-candidate readiness/reasons |
| `GET /api/v1/projects/{id}/audit` | Searchable durable receipts |

## Event stream

The current `/api/stream` remains while target clients consume versioned typed
events. An event is an invalidation plus a concise state change, not the sole
source of truth.

```json
{
  "schema": "tusker.event/v1",
  "id": "opaque-monotonic-id",
  "type": "delivery.phase_changed",
  "at": "2026-07-28T10:30:00Z",
  "project_id": "tusker",
  "object": {"kind": "delivery", "id": "D-0005"},
  "revision": "opaque",
  "invalidate": ["project_today", "delivery:D-0005"],
  "summary": "Checking the work"
}
```

Required event families:

- runtime health/ownership;
- project registration/configuration;
- plan discovered/changed/validated/authorized;
- delivery phase/attention/completed;
- task frontier/claim/progress/review/rework/completion;
- run wait/stall/infrastructure/exhaustion;
- gate/decision/override;
- integration/promotion/release;
- knowledge changed/conflict;
- notification delivered/read;
- doctor finding/repair.

Clients resume with last event ID, deduplicate, invalidate targeted queries, and
periodically reconcile. Missing events cannot corrupt state.

## State invariants

1. Product projections are derived from canonical records and runtime store;
   they are not new sources of truth.
2. Delivery completion is not inferred from worker exit.
3. Objective review is typed and attempt-bound.
4. One exact task/workspace has one modifying owner.
5. Successor unlock is exactly once and dependency-derived.
6. Authorization is revision/fingerprint-bound.
7. Promotion revalidates expected main and exact candidate immediately before
   movement.
8. Human override is distinct from proof/review pass.
9. Acknowledging attention does not mutate underlying product state.
10. App/daemon restart is replay-safe.

## Security and transport

- Local API binds to loopback by default.
- Mutations verify origin/host and use CSRF-resistant same-origin transport.
- Remote deployments require authenticated transport and actor identity.
- Secrets are referenced by host-owned handles, never returned to UI/models.
- Logs and evidence are bounded and redacted.
- Filesystem APIs are rooted in registered allowlisted paths.
- Audit receipts are append-only.

## Acceptance

- Every target screen can load from one primary projection plus optional
  drill-down calls.
- Current endpoints have an explicit migration/disposition.
- Settings return effective value and provenance.
- Plans start by identity, not user-entered path/hash.
- Read-only operations have no authority side effects.
- Errors are stable, typed, actionable, and safe to show.
- Event loss or UI restart cannot create duplicate claims or actions.
