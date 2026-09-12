# Build tier assignments and preserve project/task routing visibility

Recommended agent: **Terra medium**.

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

Depends on 02 stable API and eligibility rules; parallel with 03. Own a separate Tiers tab component, project Models/Agents mapping surface, and necessary task route/level presentation corrections. Do not edit profile editor, global Permissions or shared API types. Agree tab mounting with 03 before code changes.

## Work

Render Tier 1 Light, Tier 2 Standard, Tier 3 Demanding as three compact rows with Worker and Reviewer selectors. Options are eligible enabled profiles with readable labels. Other eligible profiles do not become fallback implicitly. Optional ordered fallbacks are disclosed on demand; don't show empty fallback controls on every resting row. Prefer one clear Save changes action for the editing scope, with guarded error handling and unsaved-state protection.

Add readable links to edit profiles/eligibility. Membership shown here derives from the same profile field edited in Profiles; do not invent a second local membership list. Missing worker/reviewer mappings show an actionable Choose profile state. Disabled/unavailable existing assignments stay visible with their problem; no silent substitute. Zero eligible options links to Add profile with a tier preselection, not an auto-created fake profile.

Projects inherit global assignments with per-lane Override and Reset. Global membership is not independently duplicated per project. Existing local profiles remain scoped and selectable only where valid. Tasks show effective tier (Standard default where unauthored) and expected worker/reviewer near Play, retain explicit overrides and provenance, and show actual recorded identity during execution/review. Do not expose full profile access flags on every ticket; profile detail is reachable. Prevent any UI claim that changing a tier updates an active attempt.

Preserve existing task authoring/import, reviewer distinction, fallback rules and authority checks. An eligible list is not a dispatch queue. When backend cannot resolve the route, show the actionable blocker rather than a model guessed from tier labels.

## Acceptance and verification

- One profile can be eligible in all tiers; each tier can choose different worker/reviewer and explicit fallback order.
- Editing eligibility in Profiles is reflected here after refresh/live update; removing assigned membership follows backend refusal/replacement semantics without UI divergence.
- Project A override leaves globals/project B intact; reset restores inherited effective values.
- Tier-only task preview agrees with CLI, explicit override/reset works and running/review identity stays bound to the actual attempt.
- Unavailable reviewer is visibly unresolved, not shown as a working built-in default.
- Behavioral UI tests cover save/reload, eligibility filters, conflicts, missing mappings, inheritance and active snapshots. Use existing bun test and typecheck; capture actual routed global/project/ticket screenshots under docs/reports/agent-profiles/tiers-ui/.
- Update user-facing tier/task docs with implemented behavior. Avoid backend resolver edits; route issues go to 02 with reproducible evidence.
