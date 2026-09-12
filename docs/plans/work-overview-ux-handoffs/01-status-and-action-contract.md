# Correct wave grouping and actionable status language

Recommended agent: **Terra high**.

## Context and full product contract

Work in /Users/sarav/Downloads/side/tusker. The user wants implementation of this bounded UX ticket. Read AGENTS.md and the Tusker skill, inspect current source/dirty baseline, and preserve other agents' work. No reset, stash, broad staging, new dependencies, nested workers, automatic wave starts or execution-policy changes. Reuse existing components and authoritative data. These files are standalone handoff contracts, not allocated tracker IDs. Reconcile existing WUX work through the supported CLI rather than duplicating completed work. Do not hand-edit tracker records.

The Work overview currently overwhelms users with long wave descriptions, repeated status badges and technical warnings, oversized search/banner elements, internal IDs and factory diagnostics. The target is a restrained Apple-native-feeling list answering: what is running, what can I start, what needs my action? Preserve Waves/Board navigation, project order/expansion, authored information, live updates, authorization, DAG navigation and accessible controls. No new design system, gradients, glows, decorative panels, sound or animation project.

Each wave row has a title, at most one secondary line, and one primary action. Secondary line priority: actionable human request or material blocker; otherwise running progress; otherwise authored short outcome. Omit zero-progress filler. Never summarize with an LLM, fabricate an outcome or overwrite authored text. Full information remains reachable in wave detail. No repeated group badge on every row. Running plus a human gate must remain discernible in the secondary line. Unknown readiness is not readiness; a gate is not generic missing data. A stopped/cancelled wave must not look successfully completed. A wave whose tasks finished but promotion remains outstanding retains the authoritative closeout state.

The list may use Needs attention, Running, Ready to start, Planned, and unavailable/history sections as warranted by real data. Empty groups disappear. Completed/history is collapsed by default, preserving cancelled/superseded distinctions. Search continues matching full title/description. Compact Unassigned count navigates to Board. Technical API errors/commands belong in expandable details; show shared problems once at page level only when genuinely shared. Never hide a material per-wave blocker merely to make the page pretty.

Use existing Start endpoint/authorization controls only when available and authoritative. Otherwise the row action is Open; never label navigation Start or bypass preflight. Review is shown only for an actionable review destination. The overview is a navigation surface, not a new execution coordinator.

## Quality and delivery requirements

Implement only owned files below. Trace active routed components before editing; do not polish an unused preview. Ask the user a precise question only for a real unresolved product/authority conflict; continue independent work. Use current UI tooling: from internal/serve/ui, bun test <your focused test file> and bun run typecheck. Add behavioral checks where logic changes; do not add source-string tests or mirror CSS in assertions. Record exact checks and existing failures; do not repair unrelated suite failures.

Capture before/after rendered screenshots of the actual changed surface, desktop and narrow width, with realistic long names and mixed states. Keep keyboard navigation, visible focus, touch targets, contrast, screen-reader names and no nested interactive elements. Use browser automation without taking over the user's desktop when possible. Do not claim HTTP 200 as rendered acceptance. Return owned changes, evidence, known gaps and concise handoff. Do not install/restart a runtime another team is using; final acceptance owns coordinated install. Screenshots and reports should be placed under docs/reports/work-overview-ux/<ticket>/.

## Your owned task

Own internal/serve/ui/src/features/workbench/overview/groupWaves.ts and its focused tests. Inspect integrationModel.ts/readiness source, but coordinate changes there with ticket 03. Do not edit WaveOverview.tsx or sidebar files. This ticket runs alongside 03; publish the stable grouping contract before 02 finishes integration.

Outcome: each wave receives truthful, useful group/state information, with human attention separated from data unavailability.

Implementation:
- Trace hasFreshHumanNeed, plannedLabel, freshness and completion checks against actual input types. Preserve fresh-run checks and exact backend startability authority.
- Replace user-facing “Not authorized” for ordinary disarmed/unstarted work with “Not started” only when that is the actual situation. Preserve real permission denial and paused/blocked distinctions.
- Human gates must carry a concrete action/reason when available. Missing startability alone never creates a human gate. Unknown readiness must be explicit and cannot enable Play.
- Provide compact structured row information using existing fields or a minimal local projection. Do not parse raw error prose to invent capability facts or add a second state machine.
- Deduplication of shared errors needs structured identity/source; preserve per-wave reasons. If backend data is insufficient, expose unknown and document a bounded missing-field requirement rather than changing execution policy.

Acceptance: fixtures cover fresh running, gate+running, actionable gate, missing readiness, stale data, disarmed, paused, ready, dependency-blocked, completed, cancelled, and pending promotion. Every wave appears once; no false Running/Ready/Completed. Full reason remains available for detail. Existing grouping consumers stay compatible or receive a documented minimal migration. Test actual group/action facts, not wording alone. Publish the field contract and example fixtures for ticket 02. Update the relevant status section of the Work spec only if needed to describe implemented behavior.
