# WUX-T-0006 — TaskInspector report

## What was built

Real, reusable `TaskInspector` component + isolated sample-data preview:

- `internal/serve/ui/src/features/workbench/inspector/TaskInspector.tsx` — panel per the
  fixed contract (`task`, `run`, `selectedTaskId`, `loading`, `error?`, `executionIdentity?`,
  `onClose`, `onOpenTask`). Content order per spec §7: title + actual stage, blocker/decision,
  intent, active attempt, accepted result, available evidence, disclosures (acceptance,
  exact verification, non-goals, technical metadata), full-task navigation.
- `inspectorLogic.ts` — pure helpers: `resolveVisibleTask` (late A→B responses resolve to
  null, caller renders loading), `identityDisplay` (unobserved/partial → `Unavailable`, never
  a guessed model), `actualStage` (durable status refined — never replaced — by live run facts;
  worker success ≠ accepted completion), `acceptedDelivery` (only an accepted proofStatus
  becomes an "accepted result"; presence is not acceptance).
- `inspector.css` — 420px right panel above 1100px viewport width, full-width overlay with
  dismiss scrim below. No global styles touched.
- `previews/wux/inspector/` (`index.html`, `main.tsx`, `fixtures.ts`) — five labeled sample
  fixtures (decision, failed, accepted, long-contract, unavailable) plus scenarios: normal,
  loading, error, late-response A→B, unobserved identity. Host-side focus restore is
  demonstrated in the preview shell (close returns focus to the origin button). `HumanActionCard`
  (compact) is embedded where its contract fits; the inspector itself stays read-only
  (Close / Open-full-task only — selecting never launches work).
- `test/wux-inspector.test.ts` — three required behavior checks, all executing:
  `inspector late task response rejected`, `inspector missing identity is unavailable`,
  `inspector evidence keeps acceptance` (logic + server-rendered markup assertions).

## Proof

- `cd internal/serve/ui && bun test test/wux-inspector.test.ts` → **PASS** (3 pass, 24 expects).
- `cd internal/serve/ui && bun run typecheck` → **PASS** (`tsc --noEmit`, clean).
- Preview bundle compiles: `vite build` of the preview root → `/tmp/wux-inspector-dist` ✓
  (scratch only; repo `dist/` untouched).

## Browser proof: BLOCKED (no screenshots, no interaction recording)

Not claimed. Exact blockers on this host:

- No listening sockets: `vite dev --port 5184/5173` fails with
  `listen EPERM: operation not permitted`; unsandboxed retry is denied
  (approval prompts disabled), so the named isolated-localhost preview URL cannot be served.
- `npx` is broken (`EPERM` on npm cache), so the ledger's `npx --yes playwright` command
  cannot run; `bunx playwright` resolves but has no browser to drive.
- Headless Chrome (`--headless --screenshot`) exits silently with no output file;
  headless Firefox prints its banner but writes no screenshot file. No screenshots were
  produced, so no fresh screenshot-only critique exists either (`critique.md` absent).
- `bun add` (happy-dom / playwright-core for a DOM harness) fails with
  `bun is unable to write files to tempdir: PermissionDenied`, so no DOM interaction
  run was possible; keyboard/focus/Escape behavior is therefore proven only at the
  markup level (panel `role="dialog"`, Escape→`onClose` handler, `tabIndex={-1}` + focus
  on selection, native `details`/`summary` disclosures, real `<button>` controls) plus the
  SSR assertions in the committed tests.

To complete on a capable host: serve `bun run dev -- --host 127.0.0.1 --port 5184 --strictPort`,
open `http://127.0.0.1:5184/previews/wux/inspector/index.html`, exercise A1–A4 per the packet
(including keyboard-only traversal and 1440/1024/390px widths), save
`docs/reports/wux/inspector/{desktop,tablet,narrow}.png`, and obtain the fresh
screenshot-only critique as `critique.md`. Until then, do not treat the fixture preview
as live integration (non-goal, upheld).

## Integration notes for WUX-T-0009

- Mount only while selected; fetch per `selectedTaskId` and pass payloads through unchanged
  (race guard is inside). Host owns prior focus, origin node/card, graph transform, and the
  full-task route behind `onOpenTask`. `executionIdentity` must be observed, stage-specific
  facts (provider/model/transport/stage + `observed`); pass `observed: false` when in doubt.
- No shared files touched: no edits to router, Sidebar, Delivery/TaskScreens, domain types,
  api/queries, manifests, global CSS, or backend. Owned paths only.

## 2026-09-07 evidence refresh

The environment blocker no longer reproduced. Chrome-channel Playwright
captured `desktop.png` (1440x1000), `tablet.png` (1024x768), and `narrow.png`
(390x844) from the real preview through the shared Vite server on port 5187.
