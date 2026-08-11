---
capsule:
  what: "Bootstrap Tusker's provider-neutral harness catalog, semantic runner profiles, V2 authoring path, and low-tier dogfood proof."
  use_when:
    - "Implementing the smallest safe slice needed before Tusker can orchestrate its own follow-on work."
  skip_when:
    - "Enabling automation, starting a daemon, arming a wave, dispatching paid work, releasing, or promoting main."
---

# Harness discovery and profile bootstrap

Status: implementation contract
Date: 2026-07-26

## Product outcome

A freshly initialized or existing Tusker repository can discover the model and
effort combinations exposed by installed local harnesses, generate a small set
of editable semantic runner profiles, preview how a task would be routed, and
author a valid V2 delivery DAG without hand-writing Tusker internals.

This slice makes the factory *ready to opt into*. It does not opt a repository
into automation, start a daemon, arm a wave, dispatch a model, move a ref,
release, spend, or configure credentials.

## Requirements

| ID | Outcome |
| --- | --- |
| R1 | `tusker runner catalog --json` returns a provider-neutral, machine-readable capability catalog with harness, model, supported effort, service-tier (when known), visibility, default, discovery source, confidence, command version, and observation time. |
| R2 | Codex discovery uses the installed CLI's live catalog when available and has an explicit bundled/offline catalog source for profile generation; neither source is an execution fallback from an ACP attempt. Claude reports only documented aliases/efforts as declared capabilities because its CLI has no model-list API. The output never pretends both sources have equal certainty. |
| R3 | Fresh initialization and an explicit reconcile/preview command can produce a small semantic role set instead of a model × effort Cartesian explosion. Existing explicit profiles are preserved. Automation remains disabled before and after. |
| R4 | Runner validation accepts the effort levels actually exposed by supported harnesses, and routing can use a model-neutral task complexity (`routine`, `standard`, `complex`, `frontier`) without embedding a provider model in task contracts. |
| R5 | `tusker delivery plan` emits `tusker.delivery-plan/v2`, source keys, requirement placeholders, acceptance, verification, artifacts, owned paths, and dependency examples that pass the V2 parser once filled. Installed Tusker guidance tells planning agents to use that path by default for multi-unit work. |
| R6 | A read-only route preview explains the selected semantic role/profile and exact precedence without claiming or dispatching a task. |
| R7 | A disposable repository proves initialization, catalog inspection, profile generation, V2 DAG doctor/review/dry-run, and route preview with automation still off. Any actual resident-daemon launch remains a separate human/operator action. |

## Product decisions

### Capability catalog

- The catalog is a runtime, machine-local observation. It is not committed to a
  project and is never treated as permanent provider truth.
- Codex is `live` when the installed CLI returns its catalog. An explicitly
  requested bundled catalog source is `bundled`, with lower freshness; it only
  informs profile generation and never launches or replaces the ACP runner.
- Claude is `declared` until its CLI exposes a machine-readable catalog.
- Discovery failures are represented per harness. One missing harness does not
  erase healthy results from another.
- Catalog inspection is read-only and must not authenticate, install, mutate
  configuration, or launch a model session.

### Semantic profiles

Generate only these roles when the underlying capability exists:

1. `planner`
2. `execute-fast`
3. `execute-standard`
4. `execute-complex`
5. `execute-frontier`
6. `review-independent`
7. `repair-complex`

Profiles are project policy, not provider inventory. The generator chooses
balanced defaults from discovered capabilities, records the chosen harness,
model, and effort in ordinary editable `tusker.yaml`, and preserves user-owned
profiles on reconcile. It must not automatically choose the cheapest model for
all work or the frontier model for all work.

The first dogfood proof deliberately selects lower-tier execution defaults.
Escalation to a frontier profile is explicit policy and not exercised here.

### Routing

Task contracts may declare `complexity: routine|standard|complex|frontier`.
Routing precedence remains explicit task profile override, project routing
rule, lane role, then project default. Complexity maps to a semantic role; the
role maps to a configured profile. Preview must show every step and source.

### V2 authoring

Planning agents write one V2 delivery plan with source-key dependencies. Tusker
allocates final task and wave identities on inert import. Epics describe
outcomes, waves bind execution authority, and the dependency graph unlocks the
next frontier. A lone task is simply a wave of one.

## Acceptance boundaries

- No command in this work enables project automation, enables a registry
  project, arms a wave, starts/installs a daemon, dispatches a worker, changes
  release/promotion policy, moves `main`, pushes, spends, or configures secrets.
- Existing `tusker.yaml` profiles and routing rules are preserved unless the
  user explicitly requests replacement.
- Generated defaults are deterministic for the same capability snapshot and
  explain why each model/effort was selected.
- The CLI contract is the source of truth. Serve UI consumes it in a subsequent
  V2 delivery plan dogfooded through this bootstrap.

## Verification

Focused Go tests cover catalog parsing/fallback, missing harness isolation,
profile generation/preservation, effort and complexity validation, routing
precedence/preview, V2 scaffold shape, and automation-off invariants.

The disposable-repository proof records:

1. automation off before initialization/reconcile;
2. discovered catalog provenance;
3. generated semantic profiles;
4. a four-frontier V2 DAG (`hello` and `goodbye` in parallel, router, docs and
   end-to-end proof in parallel, final integration gate);
5. doctor and import dry-run success, plus a complete product review whose
   Start boundary is correctly blocked because automation remains off;
6. route previews selecting lower-tier execution profiles; and
7. automation still off, no daemon/service, no runs, and no ref movement.

## Accepted production feedback for the next factory wave

The canonical requirements from the merge-train incident now live in
`docs/specs/12-opt-in-scheduled-promotion.md` §8. They cover typed human
break-glass close, runner health before claim, installed-binary capability
discovery, canonical cross-worktree task registration, a first-class boarding
census and atomic receipt, and distinct waiting/infrastructure/stall states.

They do **not** widen this bootstrap implementation. This cut establishes
capability discovery, semantic profiles, V2 authoring, route preview, and
automation-off dogfood. A separate reviewed V2 wave must implement the §8
requirements in their documented dependency order.

<!-- tusker:delivery-import:50c2a909a997cdc9:begin -->

- `[[ORC-T-0053]]` implements delivery source `explain-runner-route`.
- `[[ORC-T-0051]]` implements delivery source `harness-catalog-profile-bootstrap`.
- `[[ORC-T-0054]]` implements delivery source `lower-tier-disposable-dogfood`.
- `[[ORC-T-0052]]` implements delivery source `v2-planning-scaffold`.

- `[[W-0005]]` is the imported delivery wave.

<!-- tusker:delivery-import:50c2a909a997cdc9:end -->
