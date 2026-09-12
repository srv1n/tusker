# Verify the installed simplified Work experience

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

Depends on tickets 01, 02 and 03. Own integration acceptance tests, reports and bounded follow-up fixes agreed with their owners. Do not start another UI redesign or change lifecycle policy.

Outcome: the actual installed Work page is noticeably calmer, preserves required information, and is usable without instructions.

Implementation and acceptance:
1. Record canonical revision, dirty build inputs and installed app/CLI/daemon identity. Coordinate one build/install after UI changes converge; preserve unrelated work and active testing.
2. Render actual routed Work, not only preview fixtures. Use the supported repeatable fixture to cover ready, running, human gate, blocked, unavailable and historical states; deterministic component fixtures may supplement hard-to-reach states and must be labelled as such.
3. Exercise search, history toggle, Unassigned→Board, row→wave DAG/detail, project navigation and reconnect/freshness behavior. Prove the full description/blocker moved off overview remains reachable. Test keyboard and narrow view.
4. Where live execution is authorized in a disposable fixture, use the supported resident daemon Start action and verify SSE updates. Do not dispatch nested workers, enable background automation or consume the final manual fixture. If not authorized, report the live check pending rather than starting real work.
5. Capture screenshots. Invoke a fresh screenshot-only critic subagent (this packet explicitly requests that bounded review) with just the screenshot, no code/history: identify intended minimalist aesthetic, compare to a top studio execution, critique composition and details, penalize overexplaining/decorative AI-looking elements, give specific bold feedback and a score /10. Fix material issues in owned/coordinate-approved paths; capture and critique each visual revision. A score does not replace functional checks.
6. Run relevant behavioral UI tests and bun run typecheck; repeat only affected checks after fixes. Update docs/system/serve-ui.md to describe shipped behavior and update existing applicable WUX proof through CLI. Do not manufacture task closure if evidence/gates remain open.
7. Deliver installed URL, before/after desktop and narrow screenshots, critic result, exact test results, relocated-information map and any blocked checks. Leave a fresh idle fixture for the user's manual review if seeding is in your authorized scope.

Acceptance is not HTTP 200 or a prettier static mock. It is rendered, working navigation with truthful statuses and reduced visible text. Do not silently remove authorization, diagnostics, information access or accessibility to satisfy a screenshot.
