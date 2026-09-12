# Compact project navigation: qualification report

## Verdict

The requested UI work is implemented and fixture-qualified. The generated UI
build passes. The resident Serve process on port 7420 still serves the previous
shell, so live interaction and installed Mac relaunch remain open gates.

## Delivered

- Replaced the full-window permanent sidebar with one native horizontal project strip.
- Added backward-compatible pins, mount-time recency and per-project return-location restoration.
- Kept project identity and checkout ownership scoped to the selected chip; global Settings/Today do not inherit the last project visually.
- Made Search and both settings controls icon-only while preserving accessible names and tooltips.
- Preserved Add project, refresh/recovery, Work/Documents, Plan, Board, Trains, Diagnostics, attention and the embedded panel route.
- Added focused state/shell tests, isolated browser proof and before/after screenshots.

## Verification

| Gate | Command | Result |
|---|---|---|
| State and shell | `bun test test/project-strip-state.test.ts test/project-strip-shell.test.ts test/wux-navigation.test.ts` | PASS: 9 tests, 81 expect calls |
| TypeScript | `bun run typecheck` | PASS |
| Production asset | `bun run build` | PASS: Vite build completed |
| Rendered interaction | `node internal/serve/ui/test/project-strip.browser.mjs` | PASS: desktop, 0/1/13/50 projects, narrow overflow, keyboard, persistence/pins, settings rendering, scope, back/forward and regressions |
| Live interaction | `TUSKER_PROJECT_STRIP_LIVE_URL=http://127.0.0.1:7420 node internal/serve/ui/test/project-strip.browser.mjs --live` | OPEN: resident UI did not expose `[data-project-strip]`; no write or restart attempted |

## Evidence boundary

The fixture browser run is deterministic evidence on the local Vite server and
records GET-only traffic. It now waits for both Settings pages to render and
fails on uncaught browser errors or the React error boundary. The former port
5193 "live" result was only a Vite shell with no projects and is withdrawn.
The resident Serve UI and installed Mac bundle were not refreshed or restarted,
so both runtime gates remain open. Tracker automation remains disarmed.

See [state proof](./state.md), [browser proof](./shell.md), and the rendered
[before](./before-desktop.png) and [after](./after-desktop.png) screenshots.
