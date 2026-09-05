---
title: "Graph and UI experience evidence"
task: FLW-T-0025
source_key: graph-ui
status: partial
revision: 03201019308fbc533e6aeace9f8c612e8b2237aa
host: "Darwin arm64, Saravanans-MacBook-Pro.local"
---

# FLW-T-0025: graph and UI experience

The UI lane is source complete for the graph and document relationship flow.
The report remains partial until the native Mac walkthrough and a saved before /
after screenshot pair are available, and until the shared Go contract regression
is run by the parent lane. A current UI-only CUA walkthrough is recorded below.

## Implemented flow

- `KnowledgeGraph` keeps the existing interactive map and adds a Mermaid view
  derived directly from `DocgraphResponse.graph.nodes` and
  `DocgraphResponse.graph.edges`. All six semantic edge kinds (`part_of`,
  `updates`, `source`, `decides_for`, `superseded_by`, and `link`) are labeled
  in Mermaid and the existing map legend.
- Mermaid titles are bounded and stripped of control characters and diagram
  delimiters before source generation. The existing strict Mermaid renderer
  and SVG sanitizer remain the only injection boundary; render failure shows
  readable source in a visible fallback and in a `Show Mermaid source` details
  block.
- Map nodes are keyboard links (`Enter` and `Space`), with an explicit focus
  ring. Graph issues are visible in a bounded `Needs attention` panel and link
  back to known source documents.
- The knowledge reader now lists resolved outgoing links, unresolved
  references, and backlinks in the same project-scoped routes. Unresolved
  references remain visible as review items instead of silently disappearing.
- `HumanActionCard` satisfy uses the native-only
  `window.tuskerShell.requestHumanReceipt({ projectId, gateId, action })`
  seam. Native returns accepted, cancelled, or error; the browser never
  receives or submits a signature and the card resolves only after accepted.
  The Swift producer and installed-app bridge were not built or exercised in
  this UI lane.

## Verification

Commands were run from `/Users/sarav/Downloads/side/tusker/internal/serve/ui`
on 2026-09-05. Results are actual local results, not tracker claims.

| Command | Result | Evidence |
| --- | --- | --- |
| `rtk bun test tests/graph-ui.test.ts tests/task-search.test.ts tests/wave-brief.test.ts` | PASS | 9 tests, 59 assertions; this is the current A2 UI check in `trust-5-experience.yaml` |
| `rtk bun test tests/graph-ui.test.ts tests/human-action.test.ts` | PASS | 5 tests, 52 assertions covering all six graph edge kinds and the security assertions |
| `rtk bun test` | PASS | 129 tests, 786 assertions |
| `rtk bun run typecheck` | PASS | TypeScript completed with no diagnostics |
| `rtk bun run build` | PASS | Vite built the graph, reader, native-card, and Mermaid chunks after the wide-graph fit adjustment; Vite emitted its existing large-chunk advisory |
| `rtk git diff --check -- internal/serve/ui/src/features/knowledge/KnowledgeGraph.tsx internal/serve/ui/src/features/knowledge/KnowledgeReader.tsx internal/serve/ui/src/features/human-action/HumanActionCard.tsx internal/serve/ui/src/lib/humanReceipt.ts internal/serve/ui/tests/graph-ui.test.ts internal/serve/ui/tests/human-action.test.ts` | PASS | No whitespace errors in the UI-owned source/test paths |
| `curl -fsS 'http://127.0.0.1:7420/api/docgraph?project=01M0Q4C79K5R8NY8H57AJC2GB5' \| jq ...` | OBSERVED | Current resident API returned 16 docs and 15 `part_of` edges; the source/link backend payload was not yet present in this installed runtime |
| `shasum -a 256 internal/serve/ui/dist/index.html internal/serve/ui/dist/assets/KnowledgeGraph-CwiBexXb.js internal/serve/ui/dist/assets/KnowledgeReader-BkrfeDMB.js internal/serve/ui/dist/assets/HumanActionCard-BzewP9x_.js` | PASS | `8a29f12c7f6546f50638fc6557c953d3e4ef0bb6e06e06895675f54e2ca5f2a5`, `d44b191638750c40284a7318a8ce7e13e3cc858385c9d23ee62375cf7a7a586f`, `4f79189158f654288756e044acbc74f76c0ff4cd7f335eb609130dcdae94142a`, `9ce25c69eb3edc556b0818f610bb45ac07c163fbfbc76a99877b8d8532fdc248` |
| `go test ./cmd/tusker -run "^(TestV7SpecRefsSurfaceInCapsulePacketAndAutomationPlan|TestServeHumanActionContractAndReviewProjection)$" -count=1 -v` | DEFERRED | Go checks were intentionally left to the parent lane; no result is claimed here |

## Walkthrough and visual evidence

The Mac was unlocked for a current UI-only walkthrough through the temporary
preview at `http://127.0.0.1:4173`, using the already-running Serve API. The
following states were inspected in Chrome:

- Graph Map rendered all 16 live nodes. The legend visibly contained all six
  semantic labels: `part of`, `updates`, `source`, `decides for`, `superseded
  by`, and `link`. The fit adjustment framed the complete wide graph at 37%.
  The live API still supplied only `part_of` edges, so source/link line paths
  remain fixture-covered until the backend worker's runtime is rebuilt.
- Mermaid rendered an SVG relationship map and exposed the same generated
  source through `Show Mermaid source`.
- Tab navigation focused a real `g.kg-node` with `role="link"` and the
  expected `Open CLI reference, System doc` label. Its computed focus outline
  was `rgb(0, 95, 204)`.
- The reader rendered the overview document and 15 project-scoped backlink
  cards with `part of` chips. The current corpus had no outgoing links to
  exercise in this runtime.
- Light and dark themes were both inspected. The rendered root tokens were
  light `surface #f9fafb` / `ink #09090b` and dark `surface #09090b` / `ink
  #fafafa`; the graph legend and Mermaid surfaces remained readable.
- The FLW-T-0050 task route rendered the native-only `Your action` card. In the
  browser-only preview, clicking `Mark complete` produced the explicit
  `Open this task in the Tusker Mac app to confirm the action.` error and left
  the gate pending. No native sheet, signature, or accepted result was faked.

These are current CUA captures, not a before/after pair. The browser CUA
screenshots were inspected inline during this run. Attempts to persist them
with macOS `screencapture` failed for both the Chrome window and display with
`could not create image from window/display`, so this report contains no
durable screenshot-file links and does not claim screenshot-pair acceptance.
The source checks cover the keyboard handlers, focus style, strict renderer,
sanitizer, fallback, and error-state markup. A final native walkthrough still
needs to exercise:

1. open the project knowledge graph, switch Map → Mermaid, open Mermaid
   source, and follow a node to the reader;
2. open a document with one resolved and one unresolved reference, follow its
   backlink, and confirm the project context remains in the route;
3. trigger Mermaid render failure and inspect the source fallback;
4. tab through graph nodes and controls in light and dark themes, checking
   focus visibility and contrast; and
5. open a task gate and click Mark complete once, confirming the native sheet
   shows the server challenge and that cancellation leaves the gate open.

## Dirty-baseline boundary

Before editing, `/tmp/tusker-orchestration-baseline.txt` contained 160 lines of
existing changes spanning Go, docs, internal/docgraph, Mac native sources, and
the UI presentation pass. Those changes were preserved. The UI changes in this
lane are limited to the graph Mermaid model/view, graph and reader relationship
surfaces, the human-card bridge seam, the shared shell type declaration, their
focused tests, and this report. `internal/docgraph`, `cmd/tusker/docs_cmd.go`,
`cmd/tusker/v7_traceability.go`, and native Swift producer sources were not
edited by this lane.
