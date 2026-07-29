---
schema: tusker.design-note/v1
kind: plan
status: proposed
authority: normative
date: 2026-07-28
parent: "[[00-index]]"
related:
  - "[[03-shell-and-today]]"
  - "[[04-plan-and-authorization]]"
  - "[[07-settings-and-runner-policy]]"
  - "[[09-api-and-state-contracts]]"
tags:
  - tusker/ux
  - tusker/implementation
  - tusker/acceptance
---

# Implementation and acceptance

## Strategy

Do not execute a big-bang visual rewrite over unstable product projections.
Build vertical slices that replace one operator journey at a time while
preserving current routes as redirects and current APIs as compatibility
surfaces.

The first dogfood loop must be:

```text
plan appears automatically
→ PM reviews outcomes
→ PM starts exact delivery once
→ daemon drains one low-risk wave
→ PM watches one delivery
→ PM receives artifact-first result
```

No YAML path or copied hash is permitted in this loop.

## Phase 0 — design and contract ratification

Deliver:

- complete frames/wireflows from [[00-index]];
- approved vocabulary and information architecture;
- product projection schemas;
- state/error catalog;
- settings schema/provenance contract;
- daemon ownership decision for Mac;
- test fixtures for happy, attention, degraded, stale, and offline states.

Exit:

- Product, design, and engineering can trace every visible control to an API and
  authority rule.
- Old UX docs have explicit supersession metadata if this pack is ratified.

## Phase 1 — shell and Today

Backend:

- global/project Today projections;
- unified health summary;
- typed attention derivation;
- event invalidation.

Frontend:

- new shell and four project destinations;
- Global Today;
- Project Today;
- route redirects;
- notification/health popovers.

Exit:

- current Overview/Ops outcome information is represented without empty boards
  or control-plane cards;
- one broken project does not hide healthy siblings;
- cached/offline behavior works.

## Phase 2 — plan inbox and one-click authorization

Backend:

- indexed plan identities;
- plan review by stable ID;
- semantic diff;
- read-only preflight;
- idempotent start with server-held fingerprint;
- authorization receipt.

Frontend:

- plan inbox;
- review, requirements, proof, flow, exclusions;
- DAG drill-down;
- authorization sheet;
- stale-plan re-review.

Exit:

- plan selection requires no pasted path;
- authorization requires no typed hash;
- start does not enable automation/promotion/release/spend;
- replay returns the existing delivery.

## Phase 3 — delivery detail and artifact brief

Backend:

- delivery aggregate/projection;
- requirement/proof/artifact grouping;
- typed delivery timeline;
- task technical drill-down;
- repair classification.

Frontend:

- delivery list/detail;
- phase strip;
- artifact viewer;
- task/proof/review drill-down;
- pause/resume and bounded repair.

Exit:

- user can understand an active and completed delivery without opening Runs;
- implementation, integration, promotion, and release are distinct.

## Phase 4 — PM-first runner setup and Settings

Backend:

- provider-neutral discovery catalog;
- runner health;
- four-role setup read/preview/write;
- route preview;
- versioned settings schema;
- effective values/provenance;
- impact preview.

Frontend:

- Basic and Advanced Settings;
- recommended model roles;
- exact profile/routing drill-down;
- automation/capacity/notification policy;
- staged authority changes.

Exit:

- a fresh project never begins in an unexplained empty runner state;
- explicit profiles are preserved;
- generated defaults are deterministic and conservative;
- automation remains off until enabled.

## Phase 5 — Diagnostics and field robustness

Backend:

- installed capability manifest;
- runner preclaim health;
- typed wait/stall/infrastructure state and deadlines;
- bounded retry/TTL;
- canonical worktree registration;
- boarding census/receipt;
- doctor projection;
- human override receipt.

Frontend:

- health overview;
- daemon/runner/queue/run/workspace detail;
- doctor and safe repair;
- capability report;
- dedicated override flow/audit.

Exit:

- every field incident in [[08-daemon-diagnostics-and-recovery]] is detectable,
  machine-readable, and actionable;
- no retry loops forever;
- no task record is silently invisible to boarding.

## Phase 6 — scheduled promotion and release surface

Backend:

- complete scheduled policy settings;
- shadow/stage/promote timeline;
- full-gate and red packet projection;
- release profile and authority receipt;
- notification deduplication.

Frontend:

- promotion/release detail;
- policy impact review;
- morning outcome;
- exactly scoped notifications.

Exit:

- default branch moves only after exact full-green proof;
- release remains separately authorized;
- routine green departures remain quiet.

## Route migration

| Existing | Migration |
|---|---|
| Global Needs | Redirect to Global Today with attention anchor |
| Project Overview | Redirect to Today |
| Work | Redirect to Plan DAG or Deliveries, preserving task deep link |
| Delivery review | Redirect to Plan selected by stable identity |
| Ops | Redirect to Diagnostics; outcome anchors route to Today/Delivery |
| Docs/Knowledge variants | Consolidate under Knowledge |
| Run detail | Redirect to Delivery task or Diagnostics run based on intent |
| Details/settings | Consolidate Settings and preserve tab-to-section mapping |

Keep redirects for at least one compatibility cycle and instrument unresolved
links locally.

## Data migration

- Do not delete/reinitialize repositories as a general upgrade strategy.
- Detect config/schema capability and provide previewed migration.
- Preserve canonical tasks/specs/decisions/evidence and runtime receipts.
- Generated caches/UI indexes may rebuild.
- Project unregister/re-register is allowed only when it preserves vault/repo
  truth and the user sees the consequence.
- Legacy projects with work in flux require non-destructive migration.

## Testing pyramid

### Contract tests

- projection schemas and enum evolution;
- stable error codes/remedies;
- settings provenance and reset;
- plan identity/staleness/idempotency;
- authority separation;
- event resumption/invalidation.

### State-machine tests

- claim/review/land/close/successor exactly once;
- crash at every durable boundary;
- wait/stall/retry/TTL;
- partial project failure isolation;
- promotion drift;
- release failure;
- override exact revision.

### UI tests

- every screen state with deterministic fixtures;
- keyboard and focus;
- stale mutation;
- offline/reconnect;
- error boundary containment;
- long text, zero/many items;
- permissions/refusal;
- responsive layouts.

### End-to-end dogfood

Use a disposable or low-risk repository:

1. discover/register with automation off;
2. generate recommended lower-tier profiles;
3. produce a small documentation/code plan;
4. plan appears in inbox;
5. review and start from UI;
6. enable only armed-delivery automation;
7. daemon claims the correct task;
8. proof/review complete;
9. delivery integrates without default-branch surprise;
10. UI shows artifacts and audit;
11. restart app/daemon during work and prove replay;
12. inject broken runner and stale wait, then repair via Diagnostics.

Run scheduled promotion in shadow before stage/promote.

## Performance budgets

- Today cached paint: < 500 ms on a typical local project.
- Fresh projection: < 1 s without full corpus/task-log parsing.
- Navigation with cached data: < 150 ms perceived.
- Search initial results: < 300 ms.
- Event-to-visible update: < 1 s when connected.
- Plan review for 100 tasks: < 2 s and graph interaction remains responsive.
- Diagnostics doctor may run longer but streams typed check progress and is
  cancellable.

The daemon should stat/cache operational records rather than parse every
repository document on every tick.

## Observability for the redesign

Local, privacy-preserving product metrics if enabled:

- time from plan discovery to review;
- plan review to start;
- frequency of stale-plan refusal;
- Today-to-action path;
- Diagnostics entry reason;
- repair success;
- settings abandonment;
- raw-log disclosure rate;
- repeated navigation loops.

Success means users spend less time in Diagnostics and technical disclosures,
not that those surfaces disappear.

## Rollback

- New projections and routes ship behind a local feature flag during dogfood.
- Old read paths remain until parity is proven.
- Disabling new UI does not change daemon authority or task state.
- Completion/promotion feature flags stop fresh actions while preserving live
  work and receipts.
- No rollback deletes canonical records.

## Product acceptance

1. A PM can orient, authorize, monitor, and understand delivery without CLI or
   task/runtime knowledge.
2. Primary navigation contains Today, Plan, Deliveries, Knowledge only.
3. A normal screen exposes at most three major groups.
4. Healthy emptiness is calm and compact.
5. Plan discovery and authorization require no pasted YAML path or hash.
6. Role-based model policy is visible and configurable.
7. Automation, promotion, release, triage, and spending are independently
   opt-in.
8. The daemon is app-owned in the normal Mac workflow and diagnosable.
9. Every field robustness incident has typed detection and a bounded remedy.
10. Artifacts and acceptance precede tasks, logs, and receipts.
11. All technical facts remain reachable and auditable.
12. Existing in-flight repositories can migrate without destructive reinit.

## Design review checklist

- Does every frame map to the screen inventory in [[00-index]]?
- Does every action map to [[09-api-and-state-contracts]]?
- Does every confirmation follow
  [[10-guardrails-authority-and-confirmations]]?
- Is internal state hidden but reachable?
- Are offline, stale, invalid, partial, and empty states designed?
- Are model roles semantic rather than provider priority?
- Are daemon health and project automation visually distinct?
- Is release visibly separate from promotion?
- Can the complete core flow be performed by keyboard?

## Engineering handoff rule

Once the design is approved, convert each phase into a Tusker V2 delivery plan:
tasks declare observable acceptance, exact proof, artifact contract,
dependencies, owned paths, and intended model role/complexity. Import remains
inert. Authorize one dogfood delivery at a time until the UI itself proves the
next one can be safely managed.
