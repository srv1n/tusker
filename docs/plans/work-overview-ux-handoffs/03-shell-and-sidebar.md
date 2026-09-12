# Remove surrounding navigation and diagnostic clutter

September 10 update: the user replaced the expandable project sidebar with a horizontally scrolling project strip and icon-only shell controls. For project navigation and shell placement, [.tusker/specs/work-area-redesign.md section 4](../../../.tusker/specs/work-area-redesign.md#4-navigation-and-persistence) supersedes the sidebar/expansion requirements below. Use the compact navigation tasks indexed in [.tusker/specs/work-area-build-packets.md](../../../.tusker/specs/work-area-build-packets.md). Other Work and diagnostic-restraint requirements remain applicable; do not rebuild an expanded sidebar from this historical handoff.

Recommended agent: **Luna high**.

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

Own integration/WorkExperience.tsx, integration/StreamStatus.tsx and the active Sidebar.tsx or ProjectNavigation.tsx after tracing routing; shared global CSS only if strictly necessary and scoped. Do not edit overview files or project registry/deduplication logic. Another team has project grouping changes: preserve them, and if it owns the same component defer that file or agree a small patch boundary through the user. Can run alongside 01 and 02 with these file boundaries.

Outcome: Work has a compact meaningful header and sidebar, while diagnostics remain reachable where needed.

Implementation:
- Replace/remove the raw project identifier above Work. Use the human project name where context is needed; avoid repeating it excessively when sidebar selection is obvious.
- Keep Waves/Board navigation and useful existing controls; reduce oversized empty header space without squeezing content.
- Healthy live updates should use a quiet labelled indicator; exact last-event time belongs in tooltip/details. Disconnected/stale status remains visibly explicit, accessible beyond color, and must not imply freshness.
- Remove always-visible factory-health prose and unrelated missing-file diagnostics from the sidebar footer. Preserve access in Settings/troubleshooting and a quiet accessible affected-project indicator. If no destination exists, supply a small expandable detail rather than deleting the information.
- Preserve project drag order, remembered expansion/selection, grouping work, navigation and repair actions. Do not auto-collapse other projects against stored preferences. No backend cleanup or branch changes.
- Supply any existing start/action callback plumbing requested by ticket 02 only through current supported actions; do not invent new automatic execution.

Acceptance: screenshot has no raw project ID or footer log text; all previous navigation and diagnostic information is still reachable. Healthy vs disconnected live state is truthful. Existing project persistence and route tests pass; keyboard focus remains visible. Confirm inactive-project issues no longer dominate active Work. Verify global changes do not break Documents or Settings at narrow widths. Provide before/after screenshots and a short map of where relocated information now lives.
