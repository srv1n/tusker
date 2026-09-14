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

Read-only source inspection found named profiles containing harness/model/effort and execution policy, a layered resolver, and `tusker runner route <TASK-ID> --lane execute|review --json`. Reuse these. Global profile routing is not currently enabled merely because a global configuration layer exists. Current task complexity values are routine, standard, complex and frontier. Earlier inspection found mock Settings rows; September 9 inspection finds a live Models form inside App Settings → Runner profiles, but project/ticket usability and installed acceptance remain incomplete; existing route/catalog commands do not establish a complete authoritative model list for every transport.

Likely source owners: `cmd/tusker/runner_profiles.go`, `runner_catalog.go`, `runner_route_preview.go`, `commands_v7.go`, `v7_validation.go`, wave authoring, run persistence, serve projections/actions and `internal/serve/ui/src/features/settings`. Verify names and active edits before implementation. Never replace the current dirty baseline.

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


## September 9 completion contract: models people can configure

This section extends the existing implementation contract and supersedes the earlier three-ticket delivery sequence where it describes parallel UI work. It is a design handoff, not evidence of shipped behavior. Reconcile WUX-T-0010, WUX-T-0011 and WUX-T-0012 before implementing: reuse delivered behavior, attach remaining work to those contracts, and never infer completion from their backlog labels alone.

### Product decisions and rationale

The operator wants to add/remove coding-agent choices, assign three tiers globally, override them by project or task, and know who will execute/review before starting. Existing confirmed manual setup, inheritance, explicit overrides, and start-time resolution remain authoritative. The following interaction details are implementation defaults proposed by this specification, not claims of a new user interview.

- Use visible names **Tier 1 · Light**, **Tier 2 · Standard**, **Tier 3 · Demanding**. Keep existing stored values light/standard/demanding. Numbers express work demands, not a universal model quality ranking.
- Every task has an effective work tier, but users need not fill a new required field on every ticket. Preserve an authored tier; otherwise visibly use Standard. Review tier follows work tier unless explicitly overridden. Do not overwrite existing explicit review assignments.
- A named profile is the existing reusable choice of coding agent, transport, model, reasoning effort and permission preset. Do not add a parallel account/provider database.
- Buying a subscription happens outside Tusker. Add profile selects an installed supported agent and uses its existing authentication. An unfamiliar product such as the user's example “Flock Code” is not automatically supported: show unsupported adapter clearly. Do not assume the intended vendor or fake a generic adapter.
- Global Settings gets a directly visible **Models** destination; do not bury this under Runner profiles. Project Settings gets **Models** with inherited values. Reuse the underlying profile editor.
- Tier primary choices use selectors with readable agent/model/effort labels, not comma-separated profile-name text. Ordered fallbacks live behind an optional disclosure with add/remove/reorder controls. One primary is sufficient.
- A task shows Tier, Will execute, and Will review beside its start controls. Advanced reveals explicit execution/review profile overrides and a separate review tier. Show provenance and any higher-precedence legacy rule that wins; never pretend a saved choice is effective when it is not.
- An active task shows **Running with** actual recorded identity and separate reviewer state. Predicted routes are labelled as such and re-resolved at start. Daemon dispatch is the mechanism; an “agent owner” label is not a model assignment.

### Add, edit, disable and remove

Add profile: name → supported installed coding agent/transport → model → effort → existing permission preset → Save. Offer discovered choices when available; otherwise accept exact manual values and label them unverified. Authentication/configuration failures remain actionable. Save does not launch a model; paid/live validation is a separate explicit action. Refresh does not erase unknown IDs or unsaved edits.

Edit profile affects future resolution only. Preserve active cycle snapshots and historical identities. A referenced profile cannot silently disappear.

Disable makes a profile unavailable for new dispatch, retains references/history, and displays affected tier mappings and queued tasks. Existing active work is not cancelled. Resolve subsequent eligible fallback only under the existing explicit fallback policy; otherwise show a blocked route. Check disabled availability again at dispatch even when a preview was previously valid.

Remove deletes configuration only, never a subscription, installed executable, credentials, evidence or run history. Show a reference summary first. If referenced by a mapping or unstarted explicit task override, refuse removal with links to those references; the user must replace/remove references first. Historical snapshots do not prevent removing unused configuration. For references in unavailable projects that cannot be checked, report incomplete reference checking and offer Disable instead of claiming safe deletion. Apply revision guards so newly added references cannot race removal. Prefer this bounded refusal over building bulk migration machinery.

### UI layout

Global Models: three compact tier rows, each with Worker and Reviewer selectors; then Profiles list with Add, Edit, Disable/Enable and Remove. Keep conformance/diagnostics behind details. Project Models: same three rows, inherited source label, per-lane Override and Reset to global. Do not mix a global/project scope dropdown into a screen whose navigation already establishes scope.

Task: Tier selector near Play; worker/reviewer preview immediately below; optional Advanced overrides. Inherit/reset is explicit. A save error retains edits. Missing configuration blocks Play with a direct Models link. Editing a running task cannot silently change its active cycle: label changes as applying to a future run or refuse unsupported changes clearly.

### Shared implementation requirements

Use existing profiles, modelLevels API, task authoring and route resolver. Agree one request/response contract before parallel UI edits. It must expose stable profile identity, display fields, enabled/availability state, effective tier, route provenance, override state, selected transport when known, validation blockers, and revision guards. Unknown identity stays unknown. Use existing semantics where equivalent fields already exist; no duplicate router.

UI and CLI must share validation for profile lifecycle, tier mapping, project resets, ticket tier/override authoring, and preview. Before adding a command inspect capabilities/help; publish verified command syntax with implementation, not speculative commands from this design. Current docs/system behavior must be updated by the owning implementation ticket once shipped.

## Detailed completion packets

Each packet below is a copyable task contract. These are handoff labels, not newly allocated Tusker IDs. Every worker reads this shared contract plus only its own packet. No implementation or dispatch is authorized by this document alone. Preserve unrelated dirty changes and coordinate overlapping source ownership through the user's chosen execution process.

### M1 — Complete profile lifecycle and authoritative configuration contracts

Existing ownership: WUX-T-0010 routing and WUX-T-0011 API. Treat them as the first implementation lane, not competing backend writers.

Outcome: operators can safely add, edit, disable and remove configured agent profiles; UI and CLI agree on effective tier routing.

Scope: audit delivered code against this specification; implement only missing lifecycle/reference checks, inheritance and task override contracts. Publish stable API examples for M2/M3. Preserve legacy explicit routing, snapshot identity, guarded writes and fallback classification. No new adapters or account provisioning.

Implementation pointers (verify current locations): cmd/tusker/runner_profiles.go, serve_model_levels.go, runner_route_preview.go, model_levels_test.go, serve_model_levels_test.go; internal/serve/ui/src/lib/api.ts and types/domain.ts. M1 owns shared API/types. Update docs/system/runners-and-acp.md and CLI reference.

Acceptance:
1. Create a manual profile, map it to a tier, read the same effective route through CLI and API.
2. Invalid or unsupported settings fail without partial writes; unsupported discovery preserves manual values.
3. Project override/reset and task explicit override expose correct provenance; old explicit assignments remain intact.
4. Disable prevents new use without changing active run identity; removal refuses live configuration references and retains historical records.
5. Stale writes and removal/reference races fail safely.
6. Unstarted tasks observe new settings; active cycles retain their resolved identity. No implicit model/transport fallback.

Verification: extend substantive TestModelLevels and TestServeModelLevels behavioral checks, including removal and snapshot cases. Run go test ./cmd/tusker -run 'Test(ModelLevels|ServeModelLevels)' -count=1 and record nonzero executed tests. Report pre-existing failures separately. Deliver API examples, CLI transcript and owned diff; contract is ready for M2/M3 only when shared types/actions are stable.

### M2 — Make global and project Models settings usable

Existing ownership: WUX-T-0012 settings. Depends on M1 contract availability. Can run alongside M3 after that boundary.

Outcome: a person can configure three worker/reviewer tiers without memorizing profile names and override one project without changing another.

Scope: directly discoverable Models navigation, profile editor/list lifecycle actions, three tier rows, explicit fallback ordering, inherited project values and reset. Reuse existing settings components. Remove duplicate/confusing controls from Runner profiles once replaced; retain useful diagnostics in a secondary location.

Implementation pointers: internal/serve/ui/src/features/settings/AppSettings.tsx, features/settings/app/ProfilesSection.tsx, project settings route and router.tsx. M2 owns settings/navigation only; request shared client changes from M1 rather than independently editing shared API definitions.

Acceptance:
1. Global Models can be found from Settings without knowing “runner profile” terminology.
2. Add a supported manual profile; select model/effort; save/reload; set Tier 2 worker and reviewer using selectors.
3. Override one project's worker and reset it; global defaults and another project remain unchanged.
4. Unsupported agent/discovery, missing authentication, disabled profiles and no configured profiles have truthful actionable states.
5. Removal lists references or succeeds when unused; disable does not cancel work or delete authentication.
6. Save conflicts/errors retain edits. Keyboard operation, visible focus, long profile names and narrow layout work.

Verification: interaction tests against real component actions, not source-string assertions; existing UI test runner and typecheck. Capture global, project, removal-conflict and narrow screenshots. Use screenshot-only fresh critic if delegated by the user; record critic findings and fix material issues. Provide an actual API save/reload transcript; static mocks alone are insufficient. Update Settings documentation.

### M3 — Put tier selection and worker/reviewer visibility on the ticket

Depends on M1 contract availability; parallel with M2. Reconcile existing WUX-T-0011/0012 scope to avoid duplicate implementation.

Outcome: before Play, the user knows the task tier and expected worker/reviewer; after Play, they see who actually ran.

Scope: task detail/inspector and task creation/edit entry points; effective Standard default, authored tiers, optional overrides and reset, route preview, actual attempt identity. External task import must preserve authored tiers and overrides through the existing authoring path. Display current attempt rather than confusing an earlier worker with its reviewer. Do not add model choice to every DAG node if it makes the graph cluttered.

Implementation pointers: internal/serve/ui/src/features/workbench/inspector/TaskInspector.tsx and the routed task detail/editor discovered by the implementer. Current Technical metadata contains route/level labels; move user-facing decisions into the primary task surface. M3 owns task UI, not Settings or shared API/types.

Acceptance:
1. A task with no authored tier shows Tier 2 · Standard (default) and resolved worker/reviewer without requiring manual configuration.
2. Changing tier saves through the authoritative task path and refreshes the preview; explicit overrides can be applied and reset.
3. Legacy overriding rules are explained; displayed effective route matches CLI preview.
4. Missing/disabled configuration produces a blocker and direct settings link, not an invented model.
5. Once running, actual profile/model/effort/transport is displayed when recorded; settings edits do not relabel the attempt.
6. Worker completion followed by independent review shows the reviewer as current; historical attempts remain distinguishable.
7. Importing an authored tier and reopening the UI preserves that tier and the effective route.

Verification: behavioral UI tests for default, override/reset, blocked, running and reviewing states; task-authoring/API round trip; typecheck. Capture ready and running/review screenshots. Update task authoring and execution visibility docs. No real provider invocation is needed for component tests.

### M4 — Prove configuration through dispatch and close existing work truthfully

Depends on M1, M2 and M3. This is the final acceptance lane, not a fourth concurrent implementation writer.

Outcome: a fresh operator can configure a model choice and observe that exact choice executing/reviewing a task through Tusker.

Scope: supported CLI + rendered UI journey in a disposable fixture; fix bounded integration defects with affected owner; update existing WUX records with actual proof. Do not repair unrelated broad-suite failures or add adapters as part of this packet.

Acceptance journey:
1. Record source revision, dirty build inputs and installed runtime identity.
2. Configure global Tier 2 worker/reviewer; override the worker in project A; demonstrate project B still inherits.
3. Create/import a tier-only task, compare UI preview with CLI, apply/reset an explicit override and reload.
4. Change mapping before start and observe the updated route. Start through supported resident daemon authorization; verify actual worker and independent reviewer identities and completion evidence.
5. During a running cycle, edit configuration and prove the recorded identity stays stable; a later task resolves the new mapping.
6. Exercise disable/removal references and unavailable-primary behavior with deterministic fixtures; do not break real user profiles to test errors.
7. Verify no automatic starts arise from configuration changes. Leave a fresh idle manual fixture and exact verified reseed instructions.
8. Report supported Codex acceptance separately from any unsupported/unqualified external agent. Do not call CLI --help provider conformance.

Use existing repeatable fixture/runner and relevant focused checks. Real model execution must use the resident daemon's supported path, not forbidden nested interactive workers. Provide screenshots, API/CLI comparison, candidate identity, expected/observed table and exact remaining blockers. Update docs/system/serve-ui.md and existing task proof/status through Tusker CLI; never hand-edit generated task records or claim mock tests as live acceptance.

### Sequence and completion boundary

M1 contract/API → [M2 Settings || M3 ticket UI] → M4 acceptance.

M1 can retain routing → API ordering internally. Parallelism is ownership of distinct files, not permission for unsafe simultaneous Git/build operations. If the implementation already exists, the packet becomes verification/wiring/fixes rather than a rewrite. Owner of each lane must be named in the handoff; a generic tracker “next owner: agent” is insufficient accountability.

The work is complete when an operator can find Models, configure and remove supported choices safely, map all three tiers, override a project/task, and see the resolved and actual worker/reviewer in the installed UI. Backend-only execution proof does not close the UI packets.


## Standalone five-packet handoff supersedes packet grouping above

The user requested independently copyable full-context files. Delivery is now five packets under docs/plans/model-configuration-handoffs/: 01-profile-contract.md; 02-harness-presets-and-tests.md; 03-global-project-models-ui.md; 04-ticket-tier-and-routing-ui.md; 05-installed-acceptance.md. Each includes its full common contract inline. These are implementation handoffs, not new tracker IDs. Packet 01 establishes shared contracts; packets 02/03/04 can proceed with distinct ownership against those contracts; packet 05 accepts their integrated result. Settings acceptance requires actual packet 02 integration.

Codex and Muse are required functional presets. Claude Code (interpretation of “Cloud Code”), OpenCode, Cursor and Devin are future integrations, visibly unavailable unless their existing adapters are independently established. Subscription purchase is external. Reuse the existing adapter catalog; presets ship with supported adapter definitions. Installed-harness model/effort discovery is bounded and labelled by source/freshness; exact manual entry covers unsupported discovery. Check setup and explicit potentially paid Run test are separate. A catalog/help check is not a successful model turn. No new adapter, account database or silent fallback is authorized by this delivery.


## Approved profile-first refinement and current handoff

The September 9 discussion refines the earlier model UI: Settings → Agents, Profiles and Tiers tabs. Add profile collects supported agent/transport, discovered or manual model/reasoning, capability-backed access controls, and multiple eligible tiers. A sole transport is automatic/read-only. Profiles encapsulate execution permissions; retire duplicate global editing only with effective-policy-preserving migration. External authorization restrictions still apply. Membership denotes eligibility, not primary assignment or automatic fallback. Tier worker/reviewer defaults and explicit fallback ordering select eligible profiles; project overrides and task explicit choices remain. No project-specific tier-membership layer is added now.

A small versioned capability report uses adapter-owned typed options and explicit supported/unsupported/unknown states; no universal arbitrary flag engine or equivalence guessing. Official documentation and installed protocol/help provide provenance. Non-model metadata discovery can refresh when stale; actual tests remain explicit and bound to profile revision. Codex/Muse are mandatory current integrations; other adapters deferred.

Current standalone stories supersede earlier handoff layouts for this refinement: docs/plans/agent-profile-v2-handoffs/01-capabilities-and-discovery.md, 02-profile-tier-backend.md, 03-profiles-ui.md, 04-tiers-and-task-ui.md, 05-integrated-acceptance.md. Each is self-contained. Sequence: 01 publishes capability contract → 02 publishes configuration/API contract → 03 and 04 in parallel → 05 installed acceptance. Earlier implementation must be reused, not replayed. These files are story handoffs, not newly assigned Tusker IDs. Documentation updates ship with each owning implementation story.
