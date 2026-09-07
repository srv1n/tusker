# Work integration report

Date: 2026-09-07
Host: local macOS, Chrome channel through Playwright and Codex in-app browser

## Result

Production routes now compose the six WUX leaf components over the existing
project-scoped query hooks. Waves and Board share one Work surface; wave detail
uses Flow/Results and contextual task inspection. The sidebar no longer exposes
Epics or factory Operations.

## Checks

| Check | Result |
|---|---|
| `bun test test/wux-navigation.test.ts test/wux-overview.test.ts test/wux-flow.test.ts test/wux-inspector.test.ts test/wux-results.test.ts test/wux-board.test.ts test/wux-integration.test.ts` | PASS: 35 tests, 0 failures |
| `bun run typecheck` | PASS |
| `bun run build` in `/tmp/tusker-wux-build.YFupIg` | PASS; working-tree `dist/` untouched |
| Preview at `127.0.0.1:5187/previews/wux/integration/index.html` | PASS: desktop, tablet and narrow screenshots captured |
| Browser search `editor` | PASS: only `Ship the editor refresh` remained |
| Browser Waves to Board | PASS: Board rendered 15 tasks and durable tags stayed unavailable |
| Live read-only Serve API | PARTIAL: `/api/projects` returned 200 with no registered projects; project-scoped waves/tasks/runs returned 500 `registered project not found: tusker` |
| Fresh screenshot-only critic | NOT RUN: no separately authorized critic context was available |

First actionable gap: register a real project in the running local Serve instance,
then repeat the live project/document/wave/task/result traversal. This report does
not promote the fixture preview to live integration proof.

## Backend handoff

- Authoritative wave startability with reason and revision.
- Durable task tag read/write and validation.
- Stage-specific provider, harness, model, effort, transport, freshness and provenance.
- Accepted completion/evidence binding.
- Authored wave intent distinct from an execution summary.
- Epic-free task creation.

Until those contracts exist, readiness is `unknown`, tags are unavailable, and
the client does not offer a local-only editor or infer execution identity.

## Artifacts

- `desktop.png` — 1440x1000
- `tablet.png` — 1024x768
- `narrow.png` — 390x844
