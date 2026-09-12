# 01. Preserve checkout-scoped workspace views and pane preferences

Source key: `state`. Canonical executable contract: `.tusker/specs/full-height-workspace.plan.yaml`. This packet is rendered from that plan; update the plan and regenerate the packet together. Tracker ID: `WUX-T-0020`; wave: `W-0021` (held/disarmed).

People return to the same document position, wave view or board mode after switching projects. Currently the navigation record retains last routes and opaque view state, while WorkExperience keeps important view state only in local component state. Deliver tolerant typed workspace-state helpers and focused migration tests before the new layouts consume them.

## Implementation contract
Read .tusker/specs/full-height-workspace.md sections 3–5 and 10–12.

1. Trace every read/write/recordViewState caller in navigationState.ts and ProjectStrip.tsx. Preserve the existing storage key, pinned order, mounted-order policy and route validation. Add the spec's workspace namespace inside existing opaque project view state, keyed by actual checkout ID. Do not replace the record with a separate store.
2. Export getWorkspaceViewState(state, routeProjectId) and updateWorkspaceViewState(state, routeProjectId, patch), with the exact WorkspaceViewState shape in section 5. Setters return an updated NavigationState and merge section fields; document how consumers persist through the root authority. Expose any needed visit/prune helper here so feature workers do not implement separate sanitizers. Section patch behavior must preserve unrelated project and section entries.
3. Validate enums, coordinates, pane bounds and same-checkout Docs paths. Keep last-20 visit ordering explicit for document/wave maps. Preserve unrelated opaque properties and legacy pin/order/last-screen data. Add tests for duplicate/unknown IDs, wrong-project saved URLs, blocked storage, malformed JSON, NaN/Infinity, stale selected task IDs and independent checkouts. Selection membership remains validated by feature data, not guessed in this helper.
4. Keep folder expansion/filter in treeStore; the Docs worker performs old railOpen migration using these new helpers. Define the migration signal clearly in the report: absent workspace context preference means default/migrate; false means intentionally closed. No implementation of component scrolling belongs here.

## Start and boundaries
Read AGENTS.md, the exact spec sections below, and all callers of the owned components. Obtain the supported interactive Tusker claim only after assignment; do not dispatch workers or start a daemon. Inventory the dirty baseline and current active claims before editing. Preserve other owners' changes. No backend/native app/package/global-theme edits, resets, installs, commits or deployment are part of this ticket. Use configured work/review levels; model suggestions are manual assignment advice only. Run the focused checks after the implementation settles; the final ticket owns the integrated build and dist. A runnable check with zero cases is a failure. No source-string assertion or screenshot alone proves an interaction.

## Contacts and surrounding work
Architect/origin is Sarav in the assigning conversation. No routable Tusker execution contact was verified; the installed CLI has no agent/contact command. Return a conflict with acceptance ID, facts, decision needed and recommendation to the assigning session; if unavailable, leave that dependent work blocked for the operator while continuing independent work. Never fabricate an execution address or message another session without authorization. See spec section 10 for WUX-T-0013/0015/0016/0017/0019 and graph/inspector/board overlap. Older horizontal-strip acceptance is superseded. Resolve active owned-file claims before editing; historical ticket status does not prove code absent or complete.

## Assignment and integration
Suggested manual worker: Terra. Required upstream outputs: none; this establishes the shared state boundary. Dependencies are hard because consumers need those exports and the same owned source baseline. Own only state helpers, export barrel and named tests. Shell consumes this API; Docs, Waves and Board consume it after Shell. Shell later edits index.ts, so this task must finish first. New proposed workspace-state.test.ts is a real executable test, not proof already present.

## Readiness review
This packet fixes the starting seams, invariant behavior, upstream boundary and scenario checks. Read it without relying on the design chat. No unresolved design question blocks the specified work; active ownership and prerequisite completion are checked at assignment. The report must distinguish expected behavior from observed PASS/FAIL, link each acceptance ID to evidence, and name any limitation.

## Owned paths

- `internal/serve/ui/src/features/workbench/navigation/navigationState.ts`
- `internal/serve/ui/src/features/workbench/navigation/index.ts`
- `internal/serve/ui/test/workspace-state.test.ts`
- `internal/serve/ui/test/project-strip-state.test.ts`
- `internal/serve/ui/test/wux-navigation.test.ts`
- `docs/reports/full-height-workspace/state`

## Acceptance

- **A1** — Old and malformed records load safely; pins, routes and opaque state survive adding the workspace namespace; blocked storage does not crash navigation.
- **A2** — Updates are isolated by checkout and section, merge partial patches, reject invalid paths/values and cap document/wave histories at the last 20 actual visits.
- **A3** — Exported helpers distinguish absent versus false pane preferences and preserve sibling data, making Docs rail migration and later feature restoration deterministic.

## Verification recipes

Covers **A1,A2,A3**. Create workspace tests with explicit migration, merge, identity, invalid-storage and bounded-history cases. Report actual named case counts. This proves model behavior only, not browser restoration.

```sh
cd internal/serve/ui && bun test test/workspace-state.test.ts test/project-strip-state.test.ts test/wux-navigation.test.ts
```

## Required result

Write `docs/reports/full-height-workspace/state/report.md` with acceptance ID, setup, expected/observed result, PASS/FAIL, exact command/case count and evidence links. Record unavailable live/installed surfaces separately. Submit through normal task lifecycle; implementation submission does not itself mean independently reviewed completion.
