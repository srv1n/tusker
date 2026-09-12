# 05. Give Board the remaining canvas and restore its view state

Source key: `board`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0024`; wave: `W-0021` (held/disarmed).

People see board columns immediately below one toolbar and can switch away and back without losing mode or position. The current shared Shell caps the canvas at 1180px and TaskBoard owns another controls row. Adopt the shared surface and retain existing grouping and card behavior.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 4–5, 8 and 10–12.

1. Start from the Waves worker's completed WorkExperience.tsx changes. Replace only WorkBoard's remaining Shell usage with WorkspaceSurface and remove the now-unused Shell helper only after checking all remaining callers. Keep the Waves integration intact.
2. Move TaskBoard board/list controls and available filters into toolbar via a small explicit prop/slot boundary. Keep the existing controlled mode callbacks, grouping, cards and inspector. No default contextual pane and no invented saved views or tag service.
3. Let board use all available width/height; preserve current responsive grouping rather than changing lifecycle columns. Use one vertical content scroller; any needed horizontal scrolling stays inside the board. Persist actual mode, supported filters, scroll and valid selection with the state helpers.
4. Implement the board case module using populated and empty projects, long titles, absent tag capability and task removal. Exercise mode switch, card inspector close, project return and back navigation. Test that all columns/cards remain reachable and that changing mode does not affect stored task status or send an execution request.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Luna or Terra. Required upstream outputs: waves. Dependencies are hard because consumers need those exports and the same owned source baseline. Board follows Waves because both edit WorkExperience.tsx; it can overlap any still-running Docs work. Own TaskBoard and board-specific tests only. Request shared-surface corrections through the owner; responsive integrates shared shell after this ticket. Existing WUX-T-0008/0009 contracts are surrounding behavior, not permission for broad board/backend redesign.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx`
- `internal/serve/ui/src/features/workbench/board/TaskBoard.tsx`
- `internal/serve/ui/test/wux-board.test.ts`
- `internal/serve/ui/test/full-height-workspace/board.mjs`
- `docs/reports/full-height-workspace/board`

## Acceptance

- **A1** — Board uses the remaining canvas, has no 1180px content cap or duplicate toolbar, and its first column/list heading starts at y<=80px at normal desktop scale.
- **A2** — Existing board/list grouping, card/inspector actions and capability-gated filters remain usable; every column is reachable with scrolling local to the content region.
- **A3** — Mode, available filters, scroll and valid selection survive project/section round trips; deleted selections close safely, empty/error states stay distinct and navigation never changes task state.

## Verification recipes

Covers **A1,A2,A3**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Measure available width and first heading, exercise board/list/inspector and project return, assert no unexpected mutating API calls.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case board
```

Covers **A2,A3**. Supplemental existing grouping/filter checks; DOM/browser interactions establish restored view behavior.

```sh
cd internal/serve/ui && bun test test/wux-board.test.ts
```

## Required result

Write `docs/reports/full-height-workspace/board/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
