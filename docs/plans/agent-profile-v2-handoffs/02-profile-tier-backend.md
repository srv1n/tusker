# Implement complete profiles, tier eligibility and routing contracts

Recommended agent: **Terra high**.

## Assignment and product context

Implement this bounded story in /Users/sarav/Downloads/side/tusker. Read AGENTS.md and the repo Tusker skill. Inspect current source, task records and dirty baseline before edits. Preserve unrelated work: no broad reset, stash, stage, cleanup or silent install. Use supported CLI for tracker changes. Reconcile WUX-T-0010/0011/0012 and the earlier model-configuration packets; do not reimplement delivered functionality or allocate duplicate work solely because old statuses are stale. This file is a complete handoff; no other common prompt must be pasted. Read source/docs as needed to verify implementation facts.

The operator found the delivered settings overwhelming: placeholder-like model IDs, internal profile names, separate permission/diagnostic forms and repeated metadata obscure actual configuration. The approved direction is Settings → Agents with Profiles and Tiers tabs. A profile is one complete coding-agent configuration. Profiles are shown as a quiet list with a prominent Add profile action, readable generated names, compact transport/model/effort/access summary, tier memberships and test state. Full configuration appears in an editor, not repeated cards. No broad design system or new account database.

Add/edit flow: coding agent → actual supported transport → model/reasoning → access options → tier memberships → Save or explicit Save and test. A sole supported transport is selected automatically and shown read-only; multiple genuinely supported transports permit selection. Discovery should supply current installed-harness choices where possible, with provenance and freshness. Exact manual entry remains available when discovery is unsupported; mark unverified rather than inventing model IDs. Unknown existing values are preserved. A model list is not proof of authentication, entitlement or successful execution.

Required functional integrations are Codex and Muse, reusing their actual installed adapters and auth. Claude Code (the user's “Cloud Code”), OpenCode, Cursor and Devin are future integrations, not permission to implement five new adapters. Show them unavailable only if useful; do not suggest they already run. Muse's brand/profile must map to its real existing execution route rather than a made-up transport. Do not assume every CLI supports a list-models flag or that every ACP server exposes the same capabilities.

Profiles may be eligible for Tier 1 Light, Tier 2 Standard and Tier 3 Demanding simultaneously, or none while being prepared. Membership is many-to-many and has one authoritative representation. Tier membership means eligible, not selected or automatically used as fallback. Each tier independently selects primary worker and reviewer and optional explicit ordered fallbacks from its eligible enabled profiles. Adding membership never replaces assignments. Shared defaults are global; projects can override worker/reviewer assignments and reset. Do not add project-specific membership lists in this iteration. Preserve existing project-local profiles as scoped legacy entries rather than flattening them globally. Tasks author a tier, Standard visibly by default, with optional explicit overrides; the profile is reused, not copied into each ticket.

Profile permissions encapsulate filesystem/network/approval behavior. The separate duplicated Permissions editor is retired only after effective existing values are represented without widening or silently tightening access. Independent project/system authorization still constrains execution; selecting a profile never grants authority to bypass it. Different model/effort/access configurations are different profiles. Stable IDs are distinct from editable readable display names.

Changing settings affects future resolutions; active cycles preserve their resolved snapshots. At dispatch revalidate profile availability and authorization. Missing/disabled choices block honestly or follow existing explicitly allowed availability fallback policy. Task failure or review rejection is not permission to switch models. Preserve independent review and existing start authorization; saving settings does not start tasks or arm waves.

## Implementation discipline and proof

Reuse existing profile, catalog, resolver, conformance, revision-guard and API mechanisms. No universal arbitrary shell-command or raw-flags editor. Never print secrets or accept secret values into discovery/report JSON. Interactive sessions do not launch nested codex exec workers or daemons; real test requests go through supported resident-daemon conformance/execution mechanisms with explicit authorization. Metadata refresh must not secretly invoke a model, install software or change credentials.

Use the least code that satisfies the typed contracts. Coordinate shared file ownership through the user; do not silently edit another active lane. If an exact capability or migration meaning is ambiguous, report the narrow question with evidence, continue independent work, and do not invent support. Commit only owned changes if committing; never add AI attribution. Return actual tests/results, unresolved blockers, changed files and documentation. Existing broad-suite failures are not permission to weaken assertions.

## Ownership and dependencies

Begin after story 01 publishes capability shapes. Own profiles, persistence/migration, tier memberships, CLI/API and shared frontend types. Inspect cmd/tusker/model_levels.go, runner_profiles.go, serve_model_levels.go and associated tests, internal/serve/ui/src/lib/api.ts and types/domain.ts. Do not edit profile or tier JSX. Publish stable API examples for stories 03/04 before they integrate.

## Work

Use the existing profile record with a versioned extension if required, conceptually: stable ID, display name, adapter ID, transport, model ID, optional reasoning, typed access/options, enabled state and eligible tiers. Separate test observations from configuration. Exact serialized field names should fit existing code; no duplicate profile store.

Implement one authoritative membership representation with a tier projection. Tier worker/reviewer primary and fallback references must be enabled and eligible. Use guarded atomic writes: removing membership referenced by an assignment is refused with affected references, or performed in the same explicitly submitted transaction as replacement. No silent replacement, fallback promotion or arbitrary first-profile selection. Disable may leave a visibly blocked assignment; removal refuses unresolved active configuration references. Historical snapshots survive. Unavailable projects make reference checking incomplete; offer Disable instead of unsafe removal.

Migrate existing mappings by deriving membership from their current tier references, preserving explicit routes, stable IDs, local scope and reasoning/permissions. Existing unreferenced profiles may have zero memberships. Materialize the previously effective permission configuration where necessary so removal of the global editor does not change actual access. Unknown legacy policies remain visible and non-editable/blocked for incompatible changes until resolved; never silently map them to Full access. Existing task explicit profile overrides remain valid; membership constrains tier lists, not previously valid explicit overrides.

Profile edits, tier assignment updates and tests use revision guards. Persist missing versus default reasoning distinctly if the adapter distinguishes them. Changing transport/model revalidates dependent options; do not retain hidden invalid flags. Backend validates using story 01 capabilities even if UI validation is bypassed. Invalid fields and stale revisions return actionable structured errors without partial writes.

Provide CLI/API parity for profile lifecycle, eligibility, tier primaries/fallbacks, project overrides/reset, effective route preview and profile-bound test requests. Reuse current verbs; document verified help/syntax rather than inventing commands. Profile creation does not require membership or a paid test. Tests record exact revision, not a blanket tested badge forever. Saving disabled/future unsupported configurations must not make them runnable.

## Acceptance and verification

- Round trips preserve complete configurations through CLI/API/restart; one profile belongs to multiple tiers without duplication.
- Membership/assignment races, removal, disable, reset, inherited values, unavailable references and stale writes behave as specified.
- Migration fixture demonstrates unchanged effective execute/review routes and permissions, including project-local and legacy explicit task assignments.
- New tasks resolve current mappings at start; active cycles retain identity; future dispatch rechecks disabled status and external policy restrictions.
- Tests exercise actual resolver/write behavior, not snapshots alone. Extend TestModelLevels and TestServeModelLevels suites and add focused migration/membership cases; run go test ./cmd/tusker -run 'Test(ModelLevels|ServeModelLevels|AgentProfileMigration)' -count=1 with actual matching tests.
- Publish typed API examples and update runner/CLI docs. Report unresolved legacy semantics precisely; do not fix them by changing authorization.
