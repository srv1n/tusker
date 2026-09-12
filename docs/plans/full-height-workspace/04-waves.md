# 04. Move Waves navigation left and start the graph below one toolbar

Source key: `waves`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0023`; wave: `W-0021` (held/disarmed).

People see useful wave rows or graph nodes near the top rather than after a large introduction and Execution plan banner. Add a contextual wave list to detail and preserve the existing top-down graph, query/state truth and inspector.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 4–5, 7 and 10–12.

1. Trace WorkOverview/WorkWave and all Shell callers. Replace the overview/detail wrappers with WorkspaceSurface; leave WorkBoard functional for its dependent ticket. Use the existing WaveOverview grouping and search/completed semantics. Move overview controls into toolbar without changing status precedence or startability.
2. Add the proposed WaveContext.tsx using cached useWaves summaries, filter, current wave and overview link. No fetching full member details for every sidebar item. Read section 7 for active-outside-filter and loading/error behavior. Do not copy wave lists into navigationState.
3. Put ID/title, Details outcome, truthful compact status, Flow/Results and existing guarded Start action in the toolbar. Keep complete title/outcome reachable. Remove WaveFlow's separate Execution plan header. Keep graph warnings and real execution errors visible; normal healthy geometry excludes only actual warning/error banners.
4. Make graph region fill remaining height and scroll itself. Preserve layoutTopDownFlowGraph and existing node geometry/edges; no graph engine, zoom system or horizontal layout change. Fit the first node within y<=80px at default scale. Preserve selection and inspector return. Persist real scroll, chosen view and valid selection through workspace helpers; URL view wins and background completion cannot yank Flow into Results.
5. Implement waves case with 5-task chain, parallel join, 30 long-title nodes, dependency warnings, failed reads, Results and a live fixture update. Observe actual scroll and node selection after project switching/inspector close. Exercise Start state/errors through fixtures only; record any existing command-authority defect separately rather than broadening execution changes.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: shell. Dependencies are hard because consumers need those exports and the same owned source baseline. Waves consumes shell and can run beside Docs. It owns WorkExperience.tsx before Board; Board must wait to prevent shared-file collision. flowGraph.ts and TaskInspector.tsx are read-only dependencies: WUX-T-0005/0006 own their domain behavior. Request an ownership amendment if a demonstrated layout defect truly needs those files; routine redesign of graph/inspector is excluded.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx`
- `internal/serve/ui/src/features/workbench/integration/integrationModel.ts`
- `internal/serve/ui/src/features/workbench/flow/WaveFlow.tsx`
- `internal/serve/ui/src/features/workbench/overview/WaveOverview.tsx`
- `internal/serve/ui/src/features/workbench/results/WaveResults.tsx`
- `internal/serve/ui/src/features/workbench/navigation/WaveContext.tsx`
- `internal/serve/ui/test/wux-flow.test.ts`
- `internal/serve/ui/test/wux-integration.test.ts`
- `internal/serve/ui/test/wux-overview.test.ts`
- `internal/serve/ui/test/full-height-workspace/waves.mjs`
- `docs/reports/full-height-workspace/waves`

## Acceptance

- **A1** — Overview rows and detail graph start within y<=80px in healthy desktop states; a single feature toolbar replaces the hero and Execution plan banner, and graph fills remaining height.
- **A2** — Wave context list has stable real IDs/order, useful filtering/current-item handling and distinct loading/empty/error states, with one-click wave selection and no per-wave detail fetch explosion.
- **A3** — Top-down dependencies, warnings, node state/selection, full titles/outcomes, Flow/Results, safe existing actions and task inspection remain available without inferred readiness or completion.
- **A4** — Graph scroll, view and valid selected task survive inspector and project/section round trips; URL selection wins, and background updates do not reset scroll or force Results.

## Verification recipes

Covers **A1,A2,A3,A4**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Assert canvas and first-node boxes, sidebar request count, all branch nodes reachable, Details content, state transitions, selected inspector and scroll restoration.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case waves
```

Covers **A3,A4**. Current graph/status/integration behavior is a preserved baseline; source checks are supplemental only.

```sh
cd internal/serve/ui && bun test test/wux-flow.test.ts test/wux-integration.test.ts test/wux-overview.test.ts test/wux-results.test.ts
```

## Required result

Write `docs/reports/full-height-workspace/waves/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
