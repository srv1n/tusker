# WUX-T-0007 results reader proof

Date: 2026-09-07
Host: macOS arm64 (`uname -m`), local checkout `/Users/sarav/Downloads/side/tusker`
Browser: Chrome channel through Playwright CLI; preview URL `http://127.0.0.1:5185/previews/wux/results/index.html`

## Required checks

| Check | Result | Evidence |
|---|---|---|
| `cd internal/serve/ui && bun test test/wux-results.test.ts` | PASS | 4 tests, 20 expectations; all four required behavior names execute. |
| `cd internal/serve/ui && bun run typecheck` | PASS | `tsc --noEmit` completed with exit 0. |
| `bun run dev -- --host 127.0.0.1 --port 5185 --strictPort` | PASS | Vite served the named preview on `127.0.0.1:5185`. |
| `npx --yes playwright screenshot --channel=chrome --viewport-size=1440,1000 --wait-for-selector='[data-wux-ready="true"]' http://127.0.0.1:5185/previews/wux/results/index.html docs/reports/wux/results/desktop.png` | PASS | `desktop.png`, 1440×1000. |
| Same screenshot command at `--viewport-size=1024,900` | PASS | `tablet.png`, 1024×900. |
| Same screenshot command at `--viewport-size=390,844` | PASS | `narrow.png`, 390×844. |

First actionable failure: none.

## Browser interaction evidence

Using the isolated local Chrome tab:

- Selected `Build the grouped wave overview`; the live status became `Task WUX-T-0004 opened.`
- Activated `Open flow`; the live status became `Flow opened for W-0013.`
- Activated `Evidence gap`; the reader replaced the accepted artifact list with `Evidence gap: no accepted evidence is attached` and retained the warning that drained work and successful processes do not prove acceptance.
- Returned to `Accepted evidence`; the reader restored the three canonical artifact records, including `Image · single`, `Asset unavailable`, and `Unsupported asset` states.

## Fresh screenshot-only critique

This critique was performed after the final render from the three screenshots only, without reading source or DOM:

- Desktop: PASS. The delivered outcome leads the page, acceptance/review facts are scannable, and the Flow action is visible without competing with the result.
- Tablet: PASS. The header action remains visible and the result cards retain readable two-column evidence/acceptance grouping.
- Narrow: PASS. Header controls wrap, summary facts stack, card content stays inside the viewport, and no horizontal clipping is visible.
- Follow-up: the reader is intentionally long and requires scrolling to reach lower task cards and evidence; no content is hidden or collapsed into a celebratory status.

## Acceptance mapping

- A1: delivered outcome, task intent, acceptance rows, independent review facts, and the explicit Flow action are rendered.
- A2: image records stay `Image · single` without pairing provenance; missing and unsupported assets remain explicit.
- A3: supplied performance text retains `182 ms`, `30 nodes`, `Chrome 140`, and `macOS`; the UI adds no missing measurement context.
- A4: the drained/no-accepted-evidence scenario exposes the gap and never emits a global Verified status from drained work, successful processes, or artifact presence alone.
