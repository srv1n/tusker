---
subject: model-level-configuration
title: Manual model setup and three work levels
keywords: [models, reasoning, profiles, levels, defaults, fallback]
part_of: work-area-redesign
status: canonical
created: 2026-09-07
read_when: "Implementing model discovery, manual configuration, execution routing or Settings."
skip_when: "Building provider adapters from scratch or enabling automatic work starts."
sources: [work-knowledge-and-retention.md, runner-execution-boundary.md, decisions-2026-09-07-work-area-redesign-grill.md]
decisions_locked: false
updates: [docs/system/runners-and-acp.md, docs/system/cli.md, docs/system/serve-ui.md]
capsule:
  what: "Manual global/project profile mappings for Light, Standard and Demanding work."
  use_when: "Building the functional model configuration flow."
  skip_when: "Designing a recommendation wizard or provider onboarding service."
---

# Manual model setup and three work levels

## Outcome and authority

The operator configures available coding-agent profiles once, maps three work levels to ordered implementation and review choices, and starts tasks without repeating model configuration. External planning agents author detailed task contracts and normally select only a work level. Tusker resolves that level when execution starts and records what actually ran.

The user explicitly prefers simple manual setup now. No suggested-setup wizard, cost optimizer, model recommendation engine or automatic provider installation. Global defaults, task overrides and start-time resolution are confirmed. Per-field project inheritance and keeping a resolved task cycle stable are carried-forward implementation defaults from the proposed round; the user emphasized overrides and resolution on start rather than specifying every persistence detail. Do not convert those defaults into new approval barriers.

This is an implementation specification, not a change to active routing or authorization. Existing runtime team ownership must be reconciled before edits to shared runner files. The linked runner-boundary spec remains authority for harness execution and credential handling.

## Reuse existing mechanisms

Read-only source inspection found named profiles containing harness/model/effort and execution policy, a layered resolver, and `tusker runner route <TASK-ID> --lane execute|review --json`. Reuse these. Global profile routing is not currently enabled merely because a global configuration layer exists. Current task complexity values are routine, standard, complex and frontier. Current Settings profile rows are mocks; existing route/catalog commands do not establish a complete authoritative model list for every transport.

Likely source owners: `cmd/tusker/runner_profiles.go`, `runner_catalog.go`, `runner_route_preview.go`, `commands_v7.go`, `v7_validation.go`, delivery import, run persistence, serve projections/actions and `internal/serve/ui/src/features/settings`. Verify names and active edits before implementation. Never replace the current dirty baseline.

## Discovery contract

List detected harnesses and their supported ACP/CLI execution routes. Obtain model IDs/display names and supported reasoning choices through each harness's supported discovery mechanism. Prefer authoritative installed-harness data; do not scrape guessed command output, treat public model marketing lists as account availability, or infer ACP support from a model name. If only one route supports discovery, preserve its provenance instead of claiming both routes were independently checked.

Return: harness and route identity, models, per-model reasoning values/default when supplied, source, observed time and state (loading/available/unsupported/error/stale). Unsupported discovery is a valid state. Manual entry of an exact model/effort is permitted and labeled unverified until the harness validates it. Preserve unknown configured values on reads; flag them rather than erasing settings during catalog refresh. Do not map reasoning values across harnesses by guessing equivalence. A catalog listing is not proof of authentication, entitlement or a successful run. Expensive/live conformance checks remain explicit.

No credentials in catalog/config output. Discovery must be bounded and cancellable. Reuse cached observations with visible freshness where appropriate; Refresh updates choices without discarding unsaved edits.

## Configuration contract

A profile remains a reusable named harness/model/effort/execution-policy bundle. Do not introduce another provider abstraction. Light, Standard and Demanding each have an ordered implementation profile list and an ordered review profile list. The first configured entry is primary; later entries are explicit fallbacks. One entry is enough. No silent fallback supplied by Tusker.

Global defaults are editable once. Projects inherit fields individually and can override a list, then reset it to inherit. UI and CLI show effective value and provenance. Task-level advanced overrides may select a work level, reviewer level or explicit profile; preserve the existing explicit profile mechanism. Validate ambiguity instead of applying two competing overrides silently. Routine work defaults to Standard only when no authored level exists; an empty profile mapping is an actionable configuration error, never permission to choose an arbitrary model.

Compatibility default: present routine as Light, standard as Standard, and complex/frontier as Demanding. Preserve existing persisted complexity values and explicit profile assignments; do not rewrite historical tasks. Existing frontier-specific routing must not silently downgrade when the new UI displays Demanding. Show a legacy/custom effective assignment where necessary until explicitly changed. Reuse current complexity storage if it can support three-level authoring safely. No fourth human-facing level is required.

Current routing rules/lane defaults can outrank complexity. The effective route preview must expose any such rule, and an operator changing a level mapping must see if that mapping is overridden for a task. Do not display a saved selection as effective when another rule wins. Validate precedence against existing behavior, and migrate conflicting defaults explicitly rather than maintaining two hidden routers.

## Start and fallback semantics

At task start, resolve the effective implementation and review configuration with its revision/provenance. Snapshot it for the task's implementation/review cycle. Unstarted tasks see subsequent settings changes; active attempts never change identity mid-run. Normal retries preserve the resolved cycle unless an explicit reroute creates a new recorded resolution. Record selected profile, harness, model, effort, transport when known, and fallback reason/source; never fabricate missing model or transport metadata.

Try the first configured profile. Move to the next only under an explicit allowed fallback policy and a classified availability failure, preserving the reason. Do not reinterpret a failing task/test, review rejection or uncertain partially executed attempt as model unavailability. Unknown launch outcome requires reconciliation before another attempt to avoid duplicate work. Exhaustion returns an actionable unavailable state. Never silently change transport or model because a preferred route failed; an explicitly configured fallback profile can express that change.

Default safe fallback conditions are missing/unavailable executable/profile or definitive pre-execution refusal. Any broader transient/provider-error policy must reuse the runtime team's classified failure contract. Authentication errors remain visible and never trigger credential extraction or installation.

Manual starts remain manual. Configuring levels or fallback does not enable automation or authorize another wave.

## Settings and CLI experience

Global Settings has a compact Models section with three rows. Each row shows implementation and reviewer primary profiles; Edit reveals ordered fallbacks and profile details. Profile editing exposes harness, supported route, model and reasoning choices from discovery, with manual entry when unsupported. No placeholder/mock profiles in production. Empty state says what must be configured with a direct edit action.

Project Settings shows inherited effective values and clear per-field Override/Reset actions. Task Advanced exposes optional overrides and a read-only effective-route explanation. Work views show actual execution identity when available, not an assumed level-to-model label. Save validates before writing and preserves unsaved changes on errors; concurrent modifications must not overwrite silently. Basic keyboard access, visible focus, long names and narrow widths are required. Reuse existing Settings components and style conventions; screenshot each changed screen and use the fresh screenshot-only critic rubric from the Work spec.

CLI parity: discover/refresh catalogs; list/read/write profiles; read/write/reset global and project level mappings; set task overrides through supported task authoring; preview effective execute/review routes; inspect recorded run identity. Extend existing verbs where available. Before adding commands, inventory `tusker capabilities --json`; publish exact syntax and structured schemas with the implementation. JSON output exposes source/provenance, availability and actionable validation errors; human output stays compact. CLI and UI use the same validator/resolver/write path. Atomic updates preserve unknown compatible fields and support explicit revision checks for concurrent edits.

## Delivery and acceptance

Three dependent tickets keep shared backend ownership sequential; this deliberately avoids simultaneous edits to runner configuration and serve contracts by separate workers. Coordinate with the existing runtime owner before beginning shared files.

1. **Manual configuration and effective routing:** reuse discovery/profile/resolver code; add supported global/project writes and three-level authoring, compatibility/precedence behavior, explicit fallback lists and stable run resolution. Publish CLI contracts. Add focused behavioral tests in `cmd/tusker/model_levels_test.go`: discovery unsupported/error/provenance; reasoning validation; inherited fields/reset; explicit overrides; legacy frontier preservation; unstarted versus active resolution; fallback eligibility/exhaustion and uncertain-attempt protection. Update runner and CLI docs. Command: `go test ./cmd/tusker -run TestModelLevels -count=1`. Tests must execute substantive assertions, not pass via zero matched names.
2. **Live configuration API and execution identity:** expose catalog/config CRUD and capability flags through shared validators, route provenance, task level/overrides and actual run identity. Add `cmd/tusker/serve_model_levels_test.go` covering CLI/API agreement, guarded writes, stale revisions, missing identity and actual fallback provenance. Command: `go test ./cmd/tusker -run TestServeModelLevels -count=1`. Preserve existing route/query clients until the UI migrates. Update serve UI docs.
3. **Manual model Settings UI:** replace mock cards with live data, global/project three-row setup, profile selection/effort, ordered fallbacks, reset inheritance and task Advanced overrides. Add `internal/serve/ui/test/model-level-settings.test.ts` with real interaction tests for inherited/overridden/empty/error/unsupported discovery and save conflict states. Commands: `cd internal/serve/ui && bun test test/model-level-settings.test.ts`, then `bun run typecheck`. Produce `docs/reports/model-levels/settings/desktop.png` and narrow screenshot, screenshot-only critique and `report.md` with live/manual gaps. UI fixture rendering alone is not API integration proof.

End-to-end acceptance: configure a global Standard primary and reviewer, override one project, author a Standard task with no model name, inspect route, change config before start and observe the new choice, then change config during execution and observe stable recorded identity. Test explicit task override and unavailable-primary fallback separately using the repeatable-testing owner's deterministic profiles. Do not launch real work merely to claim proof. Record candidate, commands, expected/observed results and unresolved boundaries.

## Deferred

Automatic recommendations, automatic installation, cost/quality optimization, provider-account provisioning, a new harness, automatic cross-wave starts and universal discovery guarantees. Existing harness conformance and repeatable testing remain with their current owners.

<!-- tusker:delivery-import:a5231d57d6cdcbee:begin -->

## Work streams

- `[[WUX-T-0011]]` implements delivery source `api`.
- `[[WUX-T-0010]]` implements delivery source `routing`.
- `[[WUX-T-0012]]` implements delivery source `settings`.

- `[[W-0014]]` is the imported delivery wave.

<!-- tusker:delivery-import:a5231d57d6cdcbee:end -->
