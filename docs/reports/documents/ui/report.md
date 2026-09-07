# Documents polish proof

Current status: corrected UI passes the parent-run browser regression suite.
Tracker closeout remains pending the workspace/session reconciliation described below.

Task: WUX-T-0013  
Preview: `http://127.0.0.1:5191/previews/documents/ui/index.html`  
Host: local macOS Chrome, headless Playwright  
Revision: working tree shared with the Documents implementation task

## Checks

| Check | Result | Evidence |
| --- | --- | --- |
| Focused behavior | PASS | `cd internal/serve/ui && bun test test/documents-polish.test.ts` — 8 tests, 31 assertions |
| UI typecheck | PASS | `cd internal/serve/ui && bun run typecheck` |
| Preview requests | PASS | Desktop, tablet, and narrow preview loads had no failed or 4xx/5xx requests |
| Mermaid read mode | PASS | One valid Mermaid diagram rendered through the shared renderer; one invalid diagram kept its source and displayed the readable error |
| Details disclosure | PASS | Front matter controls were hidden by default and became available after activating Details |
| Tree filter | PASS | Long filename matched through the filter and remained an accessible file action |
| Tree keyboard | PASS | Folder `aria-expanded` changed from `true` to `false` with Enter |
| Selected document | PASS | Selecting the long-name document marked it with `aria-current="page"` and replaced the editor content |
| Narrow explorer | PASS | Mobile rail opened as a named dialog, closed on Escape, and closed after selecting a file |
| Live route | PASS | `/p/01M0Q4C79K5R8NY8H57AJC2GB5/knowledge/spec-to-proof` rendered the real corpus with Mermaid; no page errors |
| Live persistence | PASS | Collapsing `.tusker/specs` survived reload; selected document deep link survived reload |

## Rendered evidence

- `desktop.png` — 1440×1000 preview capture.
- `tablet.png` — 1024×768 preview capture.
- `narrow.png` — 390×844 preview capture.
- `live-desktop.png` — real daemon-backed Documents screen.
- `live-mermaid.png` — real daemon-backed Mermaid document.

The preview is explicitly labeled sample data and uses the real `HeaderCard`,
`DocBodyEditor`, Markdown extensions, Mermaid renderer, and filesystem tree
builder. It is not a claim that sample data is live.

## Independent visual critique

Fresh clean-context critic reviewed only `desktop.png`, with no source, prior
critique or implementation context. Score: **6/10** against a studio-quality bar.

- Clear restrained editorial direction; no decorative dashboard excess.
- Raw Mermaid failure output dominates and looks broken.
- Reading column/dead space could have a more confident composition.
- Repeated pale rounded containers flatten hierarchy.
- Diagram labels are too small to carry meaning comfortably.
- Metadata is repetitive and low contrast; truncated filename feels unfinished.

Reviewer's interpretation: the invalid Mermaid input is deliberately included
in this acceptance fixture, so its presence is not evidence of a failed valid
diagram. The criticism of its raw error presentation remains valid. The score
is a visual review result, not a machine-test failure or user acceptance.
No prior critique or that fixture explanation was supplied to the critic.

## Known scope boundary

This slice intentionally does not change global CSS, route wiring, docgraph
parsing, CLI discovery, model settings, Work, runner execution, or retention.


## Independent implementation review — follow-up required

Read-only review found no P0/P1 issue, but requested two P2 corrections before
closeout: mobile drawer Tab containment/focus restoration, and preventing
repeated autosave when returned metadata is normalized. The latter must not
discard edits made while a save is in flight.

Four focused tests assert source strings rather than reader interaction;
those are static wiring checks, not proof of editing/CAS or keyboard behavior.
A runnable headless interaction check and regression assertions for the two
findings have been requested from the implementation owner. Prior reported
Bun/typecheck success remains valid at its original scope. Final acceptance
is pending these corrections and their actual rerun.


## Correction verification and tracker limitation

Parent executed `node internal/serve/ui/test/documents-polish.browser.mjs`
after the two corrections: exit 0. Assertions passed for Details/keyboard/
Mermaid, mobile focus containment and restoration, tree persistence and wiki
links, normalized metadata settling with no repeated save, and 409 conflict
draft retention/autosave suspension. These use isolated API fixtures against
the real UI, not a claim of filesystem-level concurrent-write proof.

Screenshot evidence was copied/fingerprinted by Tusker as
WUX-T-0013-E-0001. A pending inline verification row now names the runnable
browser command. No agent-authored pass receipt was inserted.

Tracker closeout is unresolved: task moved backlog to ready; work review
requires review status; finish requires an attempt; attempt start created a
separate worktree claim while material lives in the shared checkout, and
finish still reported no attempt. The unused claim was released through
`tusker work release`, with the workspace mismatch recorded. No daemon or
wave dispatch occurred, no mismatched workspace was submitted, and task
completion is not claimed. Runtime/closeout ownership needs reconciliation
against the actual shared-checkout material.

Latest regression rerun: after adding immediate reverse-Tab coverage, the
browser script exited 1 on file-selection focus restoration (expected the
explorer toggle, observed no matching aria-label). Independent source review
accepted the reverse-Tab fix, but this intermittent navigation/focus case is
under correction. The earlier successful run does not close this new failure.


## Final parent regression run

`node internal/serve/ui/test/documents-polish.browser.mjs` exited 0 after
route-replacement focus repair, initial Shift+Tab containment and a guard
that stops restoring focus when the user chooses another control. The browser
check waits for the destination document and targets its visible Details
control, rather than an unrelated hidden summary. All five scenario groups
passed, including normalization settling and 409 draft preservation.
`git diff --check` passed for the corrected UI files. Independent source
review accepted the normalization, reverse-Tab and focus-intent guards.
The earlier regression failure is resolved; the tracker/session mismatch is
still open. CLI implementation acceptance is a separate ticket/review.
