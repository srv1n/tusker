# WUX-T-0005 evidence report — readable interactive wave graph

Fixture preview only. Every task, state and model in the preview is labeled
sample data. No live integration is claimed.

## Changed files (owned paths only)

- `internal/serve/ui/src/features/workbench/flow/flowGraph.ts` — pure graph
  model: dependency classification, lane-aware display states, cycle
  detection, layered layout, viewport math.
- `internal/serve/ui/src/features/workbench/flow/WaveFlow.tsx` — the
  `WaveFlow` component exactly per the packet contract.
- `internal/serve/ui/src/features/workbench/flow/fixtures.ts` — labeled
  sample-data fixtures (chain, branches+join, external, unresolved, cycle,
  mixed, 30 long-title, 100-node, live-update pair, no-deps).
- `internal/serve/ui/src/features/workbench/flow/index.ts` — barrel export.
- `internal/serve/ui/previews/wux/flow/index.html` + `main.tsx` — isolated
  preview (scenario switcher, inspector state, event log, measurements,
  `data-wux-ready="true"`; supports `?scenario=<key>`).
- `internal/serve/ui/test/wux-flow.test.ts` — 8 focused behavior checks.
- This report. No shared routes, stores, styles, manifests or backend files
  were touched.

## Checks (from `internal/serve/ui`)

| Check | Result |
|---|---|
| `bun test test/wux-flow.test.ts` | PASS — 8 pass, 0 fail, 57 expects. Required names all execute: `flow preserves selected viewport`, `flow external and cyclic dependencies`, `flow live update preserves topology`. |
| `bun run typecheck` | PASS (`tsc --noEmit` clean). Supporting proof only. |
| SSR structural probe (real component via `react-dom/server`, per fixture: node controls, titles, edge paths, warnings, state text; live-update stability) | PASS — all fixtures, run from workspace scratch copy, probe kept at `/tmp/wux-flow-ssr.tsx` (not committed). |
| Preview production bundle (`vite build`, preview input only) | PASS — builds in ~300 ms. |

Host: Darwin arm64, bun 1.3.14.

## Acceptance mapping

- A1 (directed readable nodes/edges, truthful states, verified model labels):
  covered by `flow external and cyclic dependencies`, `flow display states
  are truthful`, `flow model labels use verified identity only`, plus the SSR
  probe rendering every fixture. PASS at logic + static-render level.
- A2 (pan/zoom/fit/reset, keyboard/list access, position preserved):
  viewport math covered by `flow preserves selected viewport` and `flow
  viewport math stays readable`; topology/viewport stability by `flow live
  update preserves topology`. Interactive click-through NOT exercised (see
  blocker). Partial.
- A3 (external/cycle/unavailable inspectable; unconfirmed refs unresolved):
  covered by `flow external and cyclic dependencies`, `flow unloaded members
  stay inspectable`, and the `unresolved`/`mixed` fixtures rendering
  Unresolved/Missing/Unavailable nodes with warnings. PASS at logic +
  static-render level.
- A4 (chain/branches/join/external/cycle/30-long-title evidence; 30- and
  100-node measurements with host/browser): fixtures cover every required
  shape. Model+layout timings measured in bun (median-of-7): 30 nodes /
  33 edges 0.10 ms; 100 nodes / 123 edges 0.21 ms; static HTML 43,831 /
  117,888 bytes. Browser paint timings unavailable (see blocker). Partial.

## Blocker (no screenshots claimed)

Rendered screenshots, headless interaction, and the fresh screenshot-only
critique could not be produced in this sandbox:

- `bun run dev -- --host 127.0.0.1 --port 5183 --strictPort` fails with
  `listen EPERM: operation not permitted`; escalated execution is unavailable
  (approval prompts disabled), so no local server can run.
- Headless Chromium/Chrome cannot render here either: any real navigation
  (`file://`, trivial or bundled) dies with `Abort trap: 6` (exit 134)
  before first paint, while `about:blank` starts. `--no-sandbox`,
  `--single-process`, fresh `--user-data-dir`, and `--disable-gpu` do not
  change it; no screenshot or `--dump-dom` output is ever produced.

Capture command for a host with browser support (from `internal/serve/ui`):

```bash
bun run dev -- --host 127.0.0.1 --port 5183 --strictPort
npx --yes playwright screenshot --channel=chrome \
  --viewport-size=1440,1000 --wait-for-selector='[data-wux-ready="true"]' \
  http://127.0.0.1:5183/previews/wux/flow/index.html \
  ../../../docs/reports/wux/flow/desktop.png
```

Repeat at 1024×768 (tablet.png) and 390×844 (narrow.png), exercise each
scenario in the preview (chain, branches, external, unresolved, cycle,
mixed, thirty, live update with Apply/Rewind), then obtain the independent
screenshot-only critique as `critique.md`. Screenshots are intentionally
absent from this directory until then.

## First actionable failure

None in scope. All runnable checks pass; remaining proof is the browser
evidence above, which needs a host where Chromium can start renderers.

## Integration notes for WUX-T-0009

- Import `{ WaveFlow }` from `features/workbench/flow`; feed `memberIds`
  from the wave, `TaskDetail[]` via lazy detail reads, `runs` from run
  queries, and `dependencyFacts` only from confirmed existence results.
- Viewport is controlled: persist it per project in the navigation
  `viewStateByProject` record (single store, no competing copy).
- `topologyKey(graph)` detects topology change; keep the viewport on
  state-only refreshes.

## 2026-09-07 evidence refresh

The environment blocker no longer reproduced. Chrome-channel Playwright
captured `desktop.png` (1440x1000), `tablet.png` (1024x768), and `narrow.png`
(390x844) from the real preview through the shared Vite server on port 5187.
