# Put tier selection and worker/reviewer visibility on the ticket

Recommended agent: **Terra medium**.

You are implementing this task in /Users/sarav/Downloads/side/tusker. Read AGENTS.md and the repository Tusker skill. Implement only this packet, validate it, and return the owned change and proof. This file contains the complete product context required for this task; no common handoff needs to be pasted separately. Inspect existing implementation first and reuse it. Preserve unrelated dirty files and concurrent work. Do not reset, stash, broadly stage, install over another active acceptance run, or dispatch nested workers. Use supported Tusker CLI for tracker updates; do not hand-edit tracker records. Do not create duplicate tasks: reconcile WUX-T-0010, WUX-T-0011 and WUX-T-0012 with your owned slice. Do not infer implementation absence from stale backlog status.

The current product problem is that backend routing has worked in acceptance, but users cannot readily find global/project model settings, configure ticket tiers, or see who will execute and review. Source has model-level endpoints, profile forms inside Runner profiles, and hidden route labels in Technical metadata. This is completion and integration work, not permission to replace the runner architecture.

## Harness scope and catalog maintenance

Codex and Muse are the mandatory functional choices for this delivery. Reuse existing installed adapters and authentication. Muse uses its existing supported configured execution route; a brand label must not create a fictitious transport or imply ACP support. The Add agent/profile preset dropdown lists Codex and Muse as supported only when their actual integration permits it. Claude Code (interpreting the user's “Cloud Code”), OpenCode, Cursor and Devin can be shown in a clearly separated unavailable/future group, with no selectable runnable preset until an adapter exists and passes its prerequisites. Do not implement those four new integrations in this work.

Harness presets are versioned application data backed by implemented adapter identities, not a scraped popularity list. Inspect/reuse the existing runner catalog. Each preset identifies supported transports, setup instructions, and detection/conformance capabilities. Model and effort choices come from a bounded supported installed-harness discovery mechanism with source and last-checked time. Unsupported discovery offers exact manual model/effort entry, preserved across refreshes and marked unverified. Never ship a static list of marketed models as proof of account entitlement. Refresh on explicit request; cached observations remain labelled with freshness. No network model calls merely to render Settings or save a profile.

Provide two separate actions: Check setup (bounded non-model detection/auth-status/capability checks supported by the adapter) and Run test (explicit small real model request with possible usage cost). Report executable detection, authentication known/unknown, discovery, and actual model-turn success separately. --help success is not authentication or live conformance. The real test uses the supported resident daemon path and existing conformance mechanism, not nested interactive codex exec. Record exact profile/model/effort/transport when known, timestamp, result, timeout/failure and actionable next step. Never expose secrets. Keep tests bounded/cancellable and clean only their owned disposable output. Do not enable project automation, start real user tasks, or switch transports silently to make tests pass.

## Escalation and delivery

Resolve routine implementation details using existing patterns. If a missing adapter contract, conflicting authorization rule, or incompatible migration requires a product decision, report the smallest exact question and continue independent work. Do not invent semantics. Return changed files, tests actually run, blockers, and screenshots for UI work. Distinguish source/unit proof from installed/live proof. No automatic human gate for routine code review; missing access is an honest blocker.

## Complete UI and routing contract

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


## Your implementation assignment

Put tier selection and worker/reviewer visibility on the ticket

Depends on packet 01 contract availability; parallel with packet 03. Reconcile existing WUX-T-0011/0012 scope to avoid duplicate implementation.

Outcome: before Play, the user knows the task tier and expected worker/reviewer; after Play, they see who actually ran.

Scope: task detail/inspector and task creation/edit entry points; effective Standard default, authored tiers, optional overrides and reset, route preview, actual attempt identity. External task import must preserve authored tiers and overrides through the existing authoring path. Display current attempt rather than confusing an earlier worker with its reviewer. Do not add model choice to every DAG node if it makes the graph cluttered.

Implementation pointers: internal/serve/ui/src/features/workbench/inspector/TaskInspector.tsx and the routed task detail/editor discovered by the implementer. Current Technical metadata contains route/level labels; move user-facing decisions into the primary task surface. packet 04 owns task UI, not Settings or shared API/types.

Acceptance:
1. A task with no authored tier shows Tier 2 · Standard (default) and resolved worker/reviewer without requiring manual configuration.
2. Changing tier saves through the authoritative task path and refreshes the preview; explicit overrides can be applied and reset.
3. Legacy overriding rules are explained; displayed effective route matches CLI preview.
4. Missing/disabled configuration produces a blocker and direct settings link, not an invented model.
5. Once running, actual profile/model/effort/transport is displayed when recorded; settings edits do not relabel the attempt.
6. Worker completion followed by independent review shows the reviewer as current; historical attempts remain distinguishable.
7. Importing an authored tier and reopening the UI preserves that tier and the effective route.

Verification: behavioral UI tests for default, override/reset, blocked, running and reviewing states; task-authoring/API round trip; typecheck. Capture ready and running/review screenshots. Update task authoring and execution visibility docs. No real provider invocation is needed for component tests.

