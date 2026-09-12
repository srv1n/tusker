# 02. Build the desktop project and section rails with a shared feature surface

Source key: `shell`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0021`; wave: `W-0021` (held/disarmed).

People select any visible project or primary section directly from the left. Replace the current horizontal strip/top tabs with a 48px project rail and 88px labeled section rail. Deliver the real root shell and the shared full-height surface that Docs/Waves/Board can adopt independently.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 2–5 and 9–12.

1. Refactor existing ProjectStrip logic, preserving queries, visibility filter, checkout selection, initial restore, stable ordering, AddProjectForm, search, attention and utilities. Root mounts the navigation authority once; rail and mobile trigger access it without parallel persistence loops. Preserve the /panel embedded exemption and existing fault banners. Root no longer renders ProjectLayout's horizontal section tabs.
2. Implement initials/collision/full-name access, vertically scrollable project targets, an explicit full-name overlay, pin/move controls, selected/focused project reveal and stable positions. Keep global utilities fixed below the scroller. Preserve All Projects hidden-project recovery and error/empty/loading states. Inspect AddProjectForm callers; retain that existing component without rewriting registration.
3. Implement WorkspaceSurface.tsx with the exact section-4 props. The exported component owns context toggle/open/width, persisted desktop preference, accessible resize and one toolbar. Children fill remaining height and own scrolling. Provide the future mobile project trigger inside this toolbar, plus a route-aware fallback for legacy/global pages; the later responsive ticket owns final breakpoint behavior. No dynamic global header registration or new docking library.
4. Remove obsolete horizontal strip CSS and update directly affected tests only where assertions conflict with the new layout. Keep current restoration tests. Do not claim all content geometry is fixed before feature tickets adopt the surface.
5. Create test/full-height-workspace.browser.mjs with dynamic --case loading of test/full-height-workspace/<case>.mjs, a fixed allowlist shell/docs/waves/board/responsive/all and failure on unknown/zero cases. Each module exports async run({browser, baseURL, outputDir}); helper module owns browser/explicit common API setup and geometry assertions. Modules return named results; all requires every module. Per-feature modules own response overrides and screenshots. Missing downstream modules are expected until their tickets finish; shell case must already run. Do not swallow unknown API calls with generic success.
6. Capture a before image before changing the shell and after-shell images at 1440/1920 widths. Report route selections and utility reachability. Leave installed app/daemon untouched.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: state. Dependencies are hard because consumers need those exports and the same owned source baseline. Shell consumes state and establishes WorkspaceSurface plus browser helper exports for all feature tickets. It owns root/router/shared CSS/export barrel and shell harness only. Docs/Waves/Board do not edit those shared files. Responsive later owns those same shell files after all features complete. Proposed new source paths are WorkspaceSurface.tsx and workspace.css; retain existing filenames for the rail if that avoids unnecessary rename churn.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

Runner output contract: TUSKER_WORKSPACE_OUTPUT_DIR optionally overrides the default report root, resolved relative to repository root; each case writes beneath its own case directory. Case helpers needed by responsive are named exports from the feature case modules. No top-level side effects on importing a case.

## Owned paths

- `internal/serve/ui/src/routes/__root.tsx`
- `internal/serve/ui/src/router.tsx`
- `internal/serve/ui/src/features/workbench/navigation/ProjectStrip.tsx`
- `internal/serve/ui/src/features/workbench/navigation/ProjectStrip.css`
- `internal/serve/ui/src/features/workbench/navigation/WorkspaceSurface.tsx`
- `internal/serve/ui/src/features/workbench/navigation/workspace.css`
- `internal/serve/ui/src/features/workbench/navigation/index.ts`
- `internal/serve/ui/test/full-height-workspace.browser.mjs`
- `internal/serve/ui/test/full-height-workspace/helpers.mjs`
- `internal/serve/ui/test/full-height-workspace/shell.mjs`
- `internal/serve/ui/test/project-strip-shell.test.ts`
- `internal/serve/ui/test/navigation-simplification.test.ts`
- `internal/serve/ui/tests/project-switching.test.ts`
- `internal/serve/ui/test/project-strip.browser.mjs`
- `docs/reports/full-height-workspace/shell`

## Acceptance

- **A1** — At desktop widths, root has exactly one project rail and one section rail, no project strip/top section row, and no blank outer top gutter; visible items require one click to select.
- **A2** — 0/1/13/50 projects, long/duplicate names, hidden projects and multiple checkouts are usable; stable ordering, keyboard focus reveal and explicit overlays work without whole-page overflow.
- **A3** — Existing registration/recovery, search shortcut, attention, both settings scopes, secondary routes, valid restore and embedded /panel remain reachable with correct route ownership.
- **A4** — WorkspaceSurface exposes the settled props, one toolbar and optional context pane, including desktop collapse/resize and safe persisted width; its children have the remaining space.
- **A5** — The shell browser case runs real routed components and records geometry/interactions with explicit fixtures; all-case dispatch fails if a module or case is missing.

## Verification recipes

Covers **A1,A2,A3,A4,A5**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Check shell bounding boxes, click counts, active routes, focus, resize and errors at 1440/1920. Exercise WorkspaceSurface in a bounded rendered component case until feature routes adopt it; label that limit.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case shell
```

Covers **A3**. Supplemental existing state/navigation regressions; update only superseded layout assertions. Browser evidence is required for visual/accessibility outcomes.

```sh
cd internal/serve/ui && bun test test/project-strip-state.test.ts test/project-strip-shell.test.ts tests/project-switching.test.ts
```

## Required result

Write `docs/reports/full-height-workspace/shell/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
