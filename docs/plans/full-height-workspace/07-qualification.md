# 07. Qualify the built full-height workspace and publish the handoff result

Source key: `qualification`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0026`; wave: `W-0021` (held/disarmed).

The integrated workspace has reproducible evidence for its geometry, navigation and state behavior, and current documentation describes what actually passed. This ticket builds once, exercises the same cases against built assets and reports exact remaining limits; it does not redesign or install the app.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 1–12.

1. Confirm all six implementation outputs and read their acceptance-linked reports. Inspect the final diff for ownership and superseded horizontal navigation paths, without deleting another owner's work. Route defects back to the correct implementation ticket; do not patch source outside this ticket's owned paths.
2. Run the exact focused test command below and then bun run build, which already runs tsc --noEmit. Record exit status and first actionable error; zero matched tests fails. Preserve unrelated baseline failures separately. Build only after all source work settles, and repeat only after a new relevant change.
3. Serve dist with bun run preview -- --host 127.0.0.1 --port 5196 --strictPort. Execute --case all with TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5196. Preserve source-fixture evidence and copy/link built-run screenshots under qualification without overwriting source-run artifacts; use a runner output-directory override added by Shell. Check all screen/edge-state requirements in section 11, including the exact desktop top budgets and responsive boundaries.
4. Write final requirement-to-evidence matrix with host, viewport/text scale, source revision/dirty baseline, actual commands/case counts, API fixture provenance and screenshot/geometry paths. Every R1–R9 gets PASS/FAIL/NOT OBSERVED with reason. Required source/build/fixture failures prevent completion. Read-only live and installed-app checks are supplemental and must be explicitly unavailable if absent; do not substitute fixture evidence or restart/install anything.
5. Update docs/system/serve-ui.md to describe verified layered navigation, settings scope, visible-project/checkout behavior, restoration, context panes and mobile navigation. Retain service/backend facts. Run scoped docs check, then docs map and validate; record unrelated corpus failures without broad repair. Normal independent review remains required; no invented human approval or aesthetic score.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: responsive. Dependencies are hard because consumers need those exports and the same owned source baseline. Qualification follows Responsive and owns dist, final report and current Serve UI documentation only. It reviews all source outputs but returns fixes to their owners. New built evidence is owned beneath docs/reports/full-height-workspace/qualification. No live-service availability gate is invented; required built-browser failures stay open, and unobserved installed behavior is reported separately.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/dist`
- `docs/system/serve-ui.md`
- `docs/reports/full-height-workspace/qualification`

## Acceptance

- **A1** — All focused cases pass and the integrated UI builds; generated assets match the reviewed source and no missing/zero-case result is reported as PASS.
- **A2** — The complete browser matrix passes against built preview and records content/first-element top bounds, layout widths, independent scroll, accessible navigation and restoration for Docs/Waves/Board.
- **A3** — The final report maps R1–R9 to actual evidence and separates source tests, fixture browser, built preview, read-only live service and installed macOS observations; all required failures remain explicit.
- **A4** — The current Serve UI reference matches observed implementation and navigation semantics; scoped documentation validation results and unrelated corpus failures are recorded.

## Verification recipes

Covers **A1**. Run after integration. build includes typecheck. Capture focused test names/count, build result and exact unrelated baseline errors.

```sh
cd internal/serve/ui && bun test test/workspace-state.test.ts test/project-strip-state.test.ts test/project-strip-shell.test.ts test/documents-polish.test.ts test/knowledge-wikilink.test.ts test/wux-flow.test.ts test/wux-integration.test.ts test/wux-overview.test.ts test/wux-results.test.ts test/wux-board.test.ts && bun run build
```

Covers **A2,A3**. Start built preview first. Runner must resolve output override relative to repo root. All cases and matrix scenarios must execute; screenshots alone do not prove interactions.

```sh
TUSKER_WORKSPACE_BASE_URL=http://127.0.0.1:5196 TUSKER_WORKSPACE_OUTPUT_DIR=docs/reports/full-height-workspace/qualification/browser node internal/serve/ui/test/full-height-workspace.browser.mjs --case all
```

Covers **A4**. Inspect prose against actual browser report in addition to schema check. Then run docs map and validate; preserve unrelated failures.

```sh
tusker docs check docs/system/serve-ui.md --json
```

## Required result

Write `docs/reports/full-height-workspace/qualification/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
