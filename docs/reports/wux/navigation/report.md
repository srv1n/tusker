# WUX-T-0003 — persistent project and wave navigator · evidence report

Source key: `navigation`. Sample-data preview; **not** live integration.

## Changed files (owned paths only)

- `internal/serve/ui/src/features/workbench/navigation/navigationState.ts` (new)
  Versioned persistence record + tolerant reader/writer and pure helpers:
  order/move/reorder (drag and keyboard share one path), multi-expand,
  per-project last path, opaque per-project view state, single-pass restore
  resolution with loop-free fallbacks, loading/empty/error wave distinction.
- `internal/serve/ui/src/features/workbench/navigation/ProjectNavigation.tsx` (new)
  Real component implementing the packet contract (`projects`, `waveLinks`,
  `waveReads`, `currentPath`, `onNavigate`, `onAddProject`,
  `onExpandedProjectsChange`). Max five wave links per project with Show more,
  no nested ticket inventory, no per-project refresh icons, truncated names
  with full accessible names, ≥32px rows, keyboard Move up/down + Alt+Arrow
  reorder, drag reorder with focus restore and polite position announcements.
- `internal/serve/ui/src/features/workbench/navigation/index.ts` (new, barrel)
- `internal/serve/ui/previews/wux/navigation/index.html` + `main.tsx` (new)
  Isolated sample-data preview: 18 projects, long names, 7→5 deduped links,
  empty/loading/failed reads, interaction log.
- `internal/serve/ui/test/wux-navigation.test.ts` (new)
- `docs/reports/wux/navigation/report.md` (this file), `critique.md`

No edits outside owned paths. Concurrent dirty baseline work was preserved.

## Proof

Host: macOS arm64, bun 1.3.14, node v25.6.1.

| Covers | Check | Result |
|---|---|---|
| A1,A2,A3,A4 | `cd internal/serve/ui && bun test test/wux-navigation.test.ts` | **PASS** — 4 pass, 0 fail, 38 assertions. Required names all execute: `navigation order survives reload`, `navigation deep link wins`, `navigation missing project fallback`, `navigation wave loading differs from empty`. |
| A1,A2,A3,A4 | `cd internal/serve/ui && bun run typecheck` | **Owned files clean** (zero errors in `workbench/navigation`, preview, or test). Full-tree run is intermittently red on other workers' in-progress files outside owned paths (`workbench/overview/groupWaves.ts`, `lib/api.ts` runner-conformance edit); left untouched per parallel-safety rules. Typecheck is supporting proof only. |
| A1,A2,A3,A4 | SSR render smoke of the real component (`renderToString`, 18 projects) | **PASS** — 6/6: 18 rows, Add project, long-name hover text, move buttons, no refresh clutter, live region. Throwaway script, kept out of the repo. |
| A1,A2,A3,A4 | Static bundle `vite build` of the preview entry | **PASS** — built in ~490ms, entry + styles + fonts bundled. Temp build config removed afterwards. |
| A1,A2,A3,A4 | Headless browser interaction + desktop/tablet/narrow screenshots + fresh screenshot-only critique | **NOT PERFORMED — blocked.** See below. No screenshots are claimed. |

## First actionable failure / blocker

Browser proof is blocked on this host, and per explicit user direction no
further browser attempts will be made:

- The sandbox denies `listen()`/`bind()` (`EPERM`, verified with python
  `socket.bind` and `vite dev --port 5181/5199`), so the packet's preview
  server command cannot run here.
- All Chromium variants abort at startup under this sandbox: Playwright
  headless shell (SIGSEGV), full chromium-1243 (SIGABRT), and the system
  Google Chrome (SIGABRT). Each attempt crashed the browser process; no pages
  were ever driven and no user browser state was touched, but the crash
  noise is the reason for the stop order.
- Pre-existing repo test `tests/factory-operations.test.ts`
  ("real Chromium render…") fails here with a 30s timeout for the same
  environmental reason. That file is tracked and unmodified by this task;
  full `bun test` is otherwise 164 pass / 1 environmental fail.

Because of this, A-row interaction evidence rests on the bun behavioral
tests (state/restore/fallback logic, which is where A1/A2/A3 decisions live)
plus the SSR render smoke — not on driven browser interaction. The rendered
screenshots (`desktop.png`, `tablet.png`, `narrow.png`) and an independent
screenshot critique are still owed and must be captured on a host where a
dev server and a browser can run, using the exact packet commands.

## Coverage notes per acceptance row

- **A1**: `reorderProject`/`moveProject` share one ordering path (test asserts
  identical output); focus restores to the moved header; position announced
  via polite live region; order/expansion/last-path persist across reload;
  new projects append, removed ones drop.
- **A2**: `resolveNavigationTarget` — deep link wins; saved screen restores;
  external URLs never restore; removed project → first project's Work with
  notice; empty → Add-project state. Wave selection expands the parent and
  records the visit.
- **A3**: `waveSectionKind` keeps loading/empty/error/ready distinct; links
  deduped by stable ID and capped at five with Show more → project Work view.
- **A4**: 14px labels, ≥32px targets, truncate + `title` + accessible names,
  visible focus rings, keyboard-only reachability (Tab/Enter/buttons, no
  drag-only path), width-agnostic layout for narrow screens.

## Remaining integration work (for WUX-T-0009)

Integration supplies `waveLinks`/`waveReads` from cached per-project reads
for the expanded subset emitted via `onExpandedProjectsChange`, feeds
`currentPath` from the router, routes `onNavigate` through real navigation,
and writes graph/view state through `recordViewState` into this same record.

## 2026-09-07 evidence refresh

The environment blocker no longer reproduced. The exact Chrome-channel
Playwright capture succeeded through the shared Vite preview at port 5187 for
`desktop.png` (1440x1000), `tablet.png` (1024x768), and `narrow.png` (390x844).
The earlier blocker text above is retained as run history, not current state.
