# Replace wave cards with a quiet actionable list

Recommended agent: **Terra medium**.

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

Own overview/WaveOverview.tsx, overview/overview.css and relevant component tests. Do not edit groupWaves.ts or WorkExperience.tsx. Consume ticket 01's stable output; presentation scaffolding can proceed meanwhile, final integration waits for 01. Coordinate any callback extension with ticket 03 rather than editing its files concurrently.

Outcome: users can scan the overview without paragraphs, repeated badges or technical commands.

Implementation:
- Replace separate rounded cards with plain rows and subtle separators. Title is dominant, one secondary line at most, one primary action. Avoid miniature fonts as a substitute for deleting clutter.
- Remove repeated state badges, 0-of-N progress filler and raw CLI hints from rows. Keep actionable state/freshness visible under the shared contract. Open exposes full authored content and diagnostic detail.
- Use compact accessible Search with no redundant visible label; preserve full-description matching. Move unassigned banner into a small Unassigned · N control in the local toolbar; keep existing Board callback.
- Replace Show completed checkbox with an accessible collapsed history/completed section using existing controlled state. Distinguish no work, no search matches and hidden historical matches; do not say no waves exist when only completed waves are hidden.
- Use native buttons/links, no nested row button plus inner button. Start only if existing supported callback and readiness authority can be wired; otherwise Open is the honest first delivery. Preserve error/loading state with concise language and expandable technical detail.

Acceptance: mixed-state desktop screenshot reads as a list; rows never show paragraph+progress+badge+warning stacks. Long titles remain accessible and wrap sensibly at narrow width; full descriptions remain readable in detail. Search, unassigned navigation, completed toggle, keyboard activation and screen-reader labels work. Per-wave blocker is not lost through deduplication. Component tests exercise these interactions against the actual props; record desktop/narrow screenshots and typecheck. Avoid typography-wide or unrelated theme changes.
