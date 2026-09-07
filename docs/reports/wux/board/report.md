# WUX-T-0008 board preview evidence

## Scope

Fixture preview only. The component is reusable and controlled by its caller; it does not write tags or mutate task status. Live API integration remains WUX-T-0009 work.

## Checks

Host: macOS arm64, Chrome 152.0.7977.76, Bun 1.3.14, Vite 8.1.3.

| Check | Result | Evidence |
|---|---|---|
| `bun test test/wux-board.test.ts` | PASS | 3 tests, 5 assertions; all required names executed. |
| `bun run typecheck` | PASS | `tsc --noEmit` completed successfully. |
| `bun run dev -- --host 127.0.0.1 --port 5186 --strictPort` | PASS | Preview served at `http://127.0.0.1:5186/`. |
| Desktop screenshot command from the task contract | PASS | `desktop.png`, 1440×1000. |
| Tablet screenshot | PASS | `tablet.png`, 1024×768. |
| Narrow screenshot | PASS | `narrow.png`, 390×844. |
| Narrow list screenshot | PASS | `narrow-list.png`, 390px viewport after switching to List. |
| Headless interaction pass | PASS | Epic absent; two selected tags matched ALL; Clear all restored 6/6; List view rendered; keyboard Enter opened contextual inspection; narrow list remained available. |

First actionable failure: none in the final checks. An initial Playwright locator used the card's board-only accessible label after switching to List; the test was corrected to target the list item by task ID and then passed. This was test-driver setup, not product behavior.

## Interaction record

1. Opened `http://127.0.0.1:5186/previews/wux/board/index.html` and waited for `[data-wux-ready="true"]`.
2. Verified no exact `Epic` label and confirmed the Active group was present.
3. Selected `frontend` and `accessibility`; observed `2 of 6 tasks`.
4. Selected `Clear all`; observed `6 of 6 tasks`.
5. Switched to List; focused the `WUX-T-0002` row and pressed Enter; contextual inspection appeared.
6. Resized to 390×844; the List view remained rendered and usable.

## Screenshot-only critique

Review input was the rendered screenshot only; no source or prior critique was used.

- Intent inferred: a quiet, compact work board for scanning state and applying optional topic filters.
- Composition: strong heading-to-filter-to-content hierarchy; desktop grouping is easy to scan and the six status states remain discoverable without repeated empty columns.
- Fine detail: typography, borders and restrained blue state accent are consistent. The narrow list is readable, but its full-width status pills add a little visual weight.
- Specific improvement: let the narrow list use a shorter inline state treatment if the production integration shows the same density; keep the current version for the first pass because state remains clearer than a color-only mark.
- Score: 8.2/10.

## Acceptance mapping

- A1: Board/List controls, compact state presentation, no Epic selector or required topic grouping.
- A2: Selecting a task calls the supplied `onSelectTask` contract; no local status mutation or new endpoint exists.
- A3: Controlled optional tags support ALL matching and Clear all; `tagsAvailable=false` renders an unavailable state and does not filter or persist local tags.
- A4: Preview includes planned/unassigned, blocked, reviewing, completed and active examples; list buttons are keyboard-focusable and the narrow list was exercised.
