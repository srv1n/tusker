# 06. Finish tablet and phone navigation without stacked top bars

Source key: `responsive`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0025`; wave: `W-0021` (held/disarmed).

People use the same hierarchy on smaller screens without squeezing multiple sidebars into the content. Desktop keeps the rails, tablet opens context as an overlay, and phone has one project trigger in the feature toolbar plus bottom section navigation.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 2–5 and 9–12.

1. Work only after Docs and Board (which includes Waves) adopt WorkspaceSurface. Implement the exact 1200/768 breakpoints in the shared shell/layout. Phone has no visible or focusable project/section rails; the project trigger joins feature toolbar controls. Global/legacy routes get one fallback phone toolbar, never a duplicated feature header. Preserve embedded /panel.
2. Complete a single active navigation overlay with explicit close, Escape/outside dismissal, background inertness and focus restoration. Picker lists visible projects and global/project actions with clear settings scopes. Context drawers close on successful item selection; dirty navigation cancellation remains under the feature's save guard. Use the section-4 inspector/navigation mutual-exclusion rule without discarding stored selection.
3. Implement bottom Waves/Board/Docs with aria-current, 44px phone targets, reserved 52px minimum height and safe-area padding. Long feature titles/secondary controls may truncate/overflow explicitly; primary save/error state and navigation stay reachable. Do not shrink text to force a fit.
4. Cross breakpoints with document edits, open overlays, keyboard focus and nonzero scroll. Close ephemeral navigation UI, release traps, preserve editor/component identity and desktop pane preference. Verify keyboard focus moves only when its old control disappears.
5. Implement responsive module at both sides of 768/1200 and at 390/320 phone widths, plus 200% zoom, reduced motion and dark mode. Use the existing docs/waves/board fixtures through their module helper exports; avoid editing their owned cases or shared production feature logic. Return feature-specific defects to their owners and rerun affected cases.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: docs, board. Dependencies are hard because consumers need those exports and the same owned source baseline. Responsive follows Docs and Board and takes serialized ownership of shell files from Shell. It owns responsive cases, not state models or feature/editor code. Root/router/shared layout changes must preserve all earlier scenario checks. No native window/titlebar integration changes.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/src/routes/__root.tsx`
- `internal/serve/ui/src/router.tsx`
- `internal/serve/ui/src/features/workbench/navigation/ProjectStrip.tsx`
- `internal/serve/ui/src/features/workbench/navigation/ProjectStrip.css`
- `internal/serve/ui/src/features/workbench/navigation/WorkspaceSurface.tsx`
- `internal/serve/ui/src/features/workbench/navigation/workspace.css`
- `internal/serve/ui/test/full-height-workspace/responsive.mjs`
- `docs/reports/full-height-workspace/responsive`

## Acceptance

- **A1** — At 1200+ rails/context dock as configured; at 768–1199 context overlays; below 768 one toolbar and bottom sections replace rails with no stacked project/section strips.
- **A2** — All navigation overlays trap/release/restore focus correctly, are mutually exclusive, close on selection/Escape/outside click and leave background inert only while open; hidden rails cannot receive focus.
- **A3** — Breakpoint changes preserve desktop preferences, content scroll/selection and dirty editor identity; phone targets and safe-area reservation prevent obscured content or whole-page horizontal overflow.
- **A4** — All project/section/settings/secondary destinations remain reachable by keyboard and touch at 320/390 widths and enlarged text; embedded /panel keeps its original compact layout.

## Verification recipes

Covers **A1,A2,A3,A4**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Run specified boundary widths, focus/Tab/Escape/click cases, inspect hidden focusables and bottom content rectangles. Dirty-editor and restore checks are actual interactions, not screenshots alone.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case responsive
```

Covers **A1,A2,A3,A4**. Start Vite from internal/serve/ui with bun run dev -- --host 127.0.0.1 --port 5195 --strictPort. Reuse an already-installed Playwright via TUSKER_PLAYWRIGHT_MODULE. These are fixture-browser checks; no real service mutation. Prefix shell commands with rtk as instructed by AGENTS.md. Rerun integrated browser modules after shared responsive changes. Any missing or zero-case module fails; preserve per-case results.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5195 node internal/serve/ui/test/full-height-workspace.browser.mjs --case all
```

## Required result

Write `docs/reports/full-height-workspace/responsive/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
