# WUX-T-0004 report — grouped wave overview

Sample-data preview and focused behavior checks. No leaf claims the app is
integrated; live API integration belongs to WUX-T-0009.

## Changed files (owned paths only)

- `internal/serve/ui/src/features/workbench/overview/groupWaves.ts` — pure
  grouping: total nonduplicating partition into Needs you / Running /
  Ready to start / Planned / Completed / Status unavailable, plus
  search/completed filtering. Exports `groupWaves` for the integration owner.
- `internal/serve/ui/src/features/workbench/overview/WaveOverview.tsx` —
  component implementing the exact contract props (`waves`, `tasks`, `runs`,
  `startability`, `descriptions`, `query`, `showCompleted`, callbacks,
  `loading`, `error`). One row per wave; the title itself is the navigation
  control (no Open column, no ticket inventory). Descriptions render only
  from supplied authored data; missing descriptions leave no filler.
- `internal/serve/ui/src/features/workbench/overview/overview.css` — local
  styles only; no global edits.
- `internal/serve/ui/src/features/workbench/overview/index.ts` — exports.
- `internal/serve/ui/src/features/workbench/overview/previewFixtures.ts` —
  labeled sample-data builders (preview + tests; never production).
- `internal/serve/ui/previews/wux/overview/` — isolated preview (`index.html`,
  `main.tsx`, `fixtures.ts`) with Mixed/Empty/Loading/Error scenarios,
  search + completed controls, and `data-wux-ready="true"`.
  `vite.preview.config.ts` is build-only proof tooling for hosts where the
  dev server cannot bind a port; it changes no shared file.
- `internal/serve/ui/test/wux-overview.test.ts` — 10 behavioral tests.
- This report.

No edits outside owned paths. Sibling `wux-*.test.ts` files in `test/` are
concurrent workers' work, untouched.

## Truthfulness decisions (spec section 5)

- Running requires a fresh held execution (`leaseStateRaw` claimed/starting/
  running, `liveness === "fresh"`, not terminal). Durable `in_progress` /
  `review` status without a fresh run never implies liveness; stale runs are
  ignored.
- Ready requires `startability[wave] === "ready"`. Armed authorization,
  all-tasks-ready, or open status never imply runnable.
- Completed requires `landedAt` or a terminal status
  (landed/closed/delivered/completed). `fullyDrained` and all-tasks-done are
  ignored — draining is not success. `cancelled`/`superseded` render in
  history as Cancelled/Superseded, never as a success label.
- Stale authorization (`stale` flag or `state === "stale"`) routes to Status
  unavailable with a freshness-loss note; never to ready/completed.
  Authoritative completion is checked first: a landed wave stays completed
  history even when its authorization read is stale, since landing is a
  canonical-store fact, not an uncertain observation.
- Row descriptions use only the supplied `descriptions` record, never
  `brief.outcome.summary` (which may be an execution summary, not authored
  intent).

## Checks

Host: darwin/arm64, bun 1.3.14. From `internal/serve/ui`:

| Check | Result |
|---|---|
| `bun test test/wux-overview.test.ts` | PASS — 10 pass, 0 fail, 50 expects. Required names all execute: `overview partitions mixed states`, `overview unknown is not ready`, `overview unavailable section preserves every wave`, `overview completed is not drained`, plus 2 filter/unassigned and 4 real-component SSR render tests. |
| `bun run typecheck` (`tsc --noEmit`) | PASS, no output. |
| `bunx vite build --config previews/wux/overview/vite.preview.config.ts` | PASS — static bundle builds (proves the preview compiles). |

First actionable failure: none for the above.

## Screenshot proof — BLOCKED, not claimed

No screenshots, no browser-interaction recording, and no screenshot-only
critique were performed on this host. Exact blocker:

- The sandbox denies all socket binds (`node ... listen` → `EPERM`;
  `Vite dev --port 5182` → `EPERM`), so the packet's isolated preview
  server and its `playwright screenshot http://127.0.0.1:5182/...` command
  cannot run here. An escalated-sandbox retry was refused (no approver).
- Headless browsers do not survive this host: Google Chrome and Chromium
  abort on launch (SIGABRT, exit 134, empty stderr); Firefox headless exits
  1 without writing a screenshot. Repeated attempts visibly disturbed the
  user's desktop browsers; no further browser launches were made.
- `docs/reports/wux/overview/` therefore contains this report only:
  `desktop.png`, `tablet.png`, `narrow.png`, and `critique.md` are absent
  and must not be assumed to exist.

To complete visual proof on an unblocked host, from `internal/serve/ui`:

```sh
bun run dev -- --host 127.0.0.1 --port 5182 --strictPort
# then, with the preview open:
npx --yes playwright screenshot --channel=chrome \
  --viewport-size=1440,1000 --wait-for-selector='[data-wux-ready="true"]' \
  'http://127.0.0.1:5182/previews/wux/overview/index.html' \
  ../../../docs/reports/wux/overview/desktop.png
# repeat at 1024x768 (tablet.png) and 390x844 (narrow.png);
# exercise search, completed toggle, and Mixed/Empty/Loading/Error scenarios.
```

## Remaining integration work (for WUX-T-0009)

- Import `WaveOverview` and `groupWaves` from the owned directory; adapt
  live queries (`useWaves`, `useTasks`, `useRuns`) and supply the
  authoritative `startability` record, authored `descriptions`, and
  navigation callbacks (`onOpenWave`, `onOpenUnassigned`).
- Query/filter state lives with the parent so it survives detail/back.
- Backend handoff: authored wave intent, complete startability predicate,
  and per-stage freshness/provenance are still missing contracts — the
  component reports them as unavailable rather than inferring them.

## 2026-09-07 evidence refresh

The environment blocker no longer reproduced. Chrome-channel Playwright
captured `desktop.png` (1440x1000), `tablet.png` (1024x768), and `narrow.png`
(390x844) from the real preview through the shared Vite server on port 5187.
