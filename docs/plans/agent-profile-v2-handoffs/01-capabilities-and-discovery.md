# Research and implement typed agent capabilities and discovery

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

Start first. Own adapter catalog/discovery/conformance metadata and focused tests. Inspect cmd/tusker/runner_catalog.go, runner_conformance.go, acp_conformance_test.go and adapter implementations. Publish the minimal stable capability contract to stories 02/03; they may begin against examples once agreed. Shared UI types/API additions need one named owner, normally story 02. No Settings JSX ownership.

## Work

1. Inventory the current Codex and Muse adapter paths, installed versions, supported transports, discovery calls and test mechanisms. Determine what is actually callable, not merely mentioned in docs.
2. Research current primary documentation for those exact harnesses/protocols: model discovery, reasoning choices, sandbox/filesystem/network/approval flags, transport selection and authentication-status probes. Use official documentation/repositories and supported local help/protocol negotiation. Record URLs, observed version/date and exact command/RPC. Do not execute guessed flags or scrape interactive terminal interfaces. Official API model lists must not be substituted for a subscription harness's admitted list without explicit documented equivalence.
3. Produce a capability matrix: field, transport, supported values, default, discovery mechanism, source, and unknown/unsupported limitations. Investigate future integrations only enough to document that support is deferred; do not expand into their implementation.
4. Define a small versioned typed capability report using existing types where possible: adapter identity/version; transports; discovery state/source/time; model IDs/display names; per-model reasoning capability; permission option descriptors. A supported feature with unknown values differs from unsupported. Empty/error/stale discovery must not erase saved configuration.
5. Use discriminated option types for bounded supported controls: boolean, enum, or constrained scalar only where needed. Keep adapter-specific IDs and a small adapter-owned extension object; do not build a generic form/schema platform. Common concepts may be shared, but each adapter translates them to verified flags/RPC parameters. Unsupported combinations are rejected; no equivalence guessed between similarly named permission modes.
6. Implement bounded discovery/caching/refresh for Codex and Muse where a supported mechanism exists. Cache by relevant adapter version, transport and non-secret configuration context, not one global model list. Cancel/time out subprocesses cleanly. Refresh stale metadata in the background only for operations established to be non-model and side-effect-free; explicit Refresh uses the same path. A failed refresh retains previous data labelled stale.
7. Define Check setup versus explicit Test profile. Setup detection may return unknown auth; test must use the exact selected configuration. Every result is bound to the profile revision/config fingerprint so edits mark previous proof outdated. Report unsupported live testing honestly if no supported route exists.

## Acceptance and verification

- A source-backed matrix and exact request/response examples exist at docs/reports/agent-profiles/capabilities.md.
- Codex/Muse return truthful discovered or unsupported states, with reasoning tied to the selected model/transport. There is no gpt-5.x placeholder catalog and no inferred entitlement.
- Tests cover unsupported discovery, stale cache retention, transport/version cache separation, timeouts/orphan cleanup, invalid options, secret redaction and test fingerprint invalidation.
- Metadata calls never invoke a live model; conformance dispatch is an explicit separate operation.
- Run focused existing catalog/conformance tests plus new substantive tests (suggested names TestAgentCapabilityDiscovery and TestAgentProfileTestIdentity); record nonzero executed tests. Broader live proof belongs to story 05.
- Update docs/system/runners-and-acp.md or its actual successor with implemented facts, provenance and unsupported discovery behavior.
