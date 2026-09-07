# Real-work UI acceptance report

Date: 2026-09-07
Owner: UI packet (`real-work-ui-acceptance`)
Status: UI corrections + deterministic proof done. Live seeded journey NOT run — blocked on two prerequisites outside this packet (fixture repo, runnable browser/server in this environment). Nothing below promotes mocked or preview proof to live proof.

## Candidate and backend identity

- UI candidate: working tree at `f9d2b2b1` plus uncommitted UI-only changes (no sibling files touched):
  - `internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx` (canonical member keys, entry-view latch, status note)
  - `internal/serve/ui/src/features/workbench/integration/integrationModel.ts` (`nextEnteredView`)
  - `internal/serve/ui/src/features/workbench/integration/StreamStatus.tsx` (new)
  - `internal/serve/ui/src/features/workbench/integration/index.ts` (exports)
  - `internal/serve/ui/src/lib/stream.ts` (dead per-project connection removed)
  - `internal/serve/ui/src/routes/__root.tsx` (dead connection unwired)
- Serve backend probed read-only: resident `127.0.0.1:7420`, installed binary `archive/pre-convergence-main-20260727-342-gf9d2b2b1` (revision `f9d2b2b1`, `modified:false`), project `tusker` (`01M0Q4C79K5R8NY8H57AJC2GB5`, 15 waves / 81 tasks at probe time).
- The resident backend serves the pre-change embedded UI. No screenshot or browser claim is made against it. Final proof must run the UI dev server built from the candidate above.

## Bounded UI corrections (all in packet-owned files)

1. Wave member details now use the canonical task key. `WorkWave` read members through `["task", id, projectId]` while every invalidation (stream mapping, `invalidateOperatorState`) targets `qk.task(id, projectId)` = `["task", projectId, id]`. Targeted task events therefore missed open DAG members; only the generic `["task"]` sweep or reconnect refreshed them, and there is no interval polling while the stream is connected. Fixed to `qk.task(id, projectId)`.
2. Entry view is latched per wave (`nextEnteredView`). The detail recomputed `initialWaveView` every render, so a wave landing while the operator watched Flow yanked the open view into Results mid-interaction. The latch keeps the entry view; an already-completed wave still leads with Results; `?view=` still wins on entry.
3. Removed the dead per-project SSE connection. `ProjectLayout` opened `/api/stream?project=<id>` with no message handler: it never fed the query cache and doubled broker clients. The unfiltered `/api/stream` already carries every project-scoped event (verified in `cmd/tusker/serve_stream.go`: an empty filter receives all events). One shared subscription remains (`main.tsx`). The packet-mandated contract change is locked by the rewritten test in `tests/stream.test.ts`; all other assertions there are untouched.
4. Stale state is now visible. No component consumed stream status, so a dropped connection silently showed last-known data. The Work header shows `StreamStatusNote` (Live updates / Reconnecting — showing last known state, plus last-event age; connectivity text is the live region, the age is supplementary).

Not touched per ownership: `cmd/tusker/demo_*.go`, lifecycle/review/workspace code, CLI dispatch, docgraph parsing, knowledge components (sibling-dirty), shared frontend types.

## Verification (observed, this session)

| Command | Result |
|---|---|
| `bun test test/real-work-events.test.ts` | 16 pass, 0 fail, 62 expects |
| `bun run typecheck` | pass |
| `bun test` (full UI suite) | 194 pass, 1 fail — the fail is `tests/factory-operations.test.ts` "real Chromium render", which fails identically on clean HEAD in a separate worktree (environmental: no working browser/CDP in this sandbox). Pre-existing, unrelated. |
| `bun test tests/stream.test.ts` | 6 pass (includes rewritten shared-subscription test) |
| `node --check test/real-work.browser.mjs` | pass |
| Journey readiness, missing env | exit 2, `READINESS WIN0` |
| Journey readiness, unreachable base | exit 2, `READINESS WIN1` |
| Journey readiness, resident tusker project, shape=fixture | exit 2, `READINESS WIN2`: 15 waves / 81 tasks, no Alpha/Beta/Follow-up — honest refusal, "not a UI failure" |
| Journey readiness, same project, shape=generic | Tier 1+2 pass (focus wave `W-0004` via member-detail edge detection), then clean `BROWSER` exit 1: headless Chrome cannot start in this sandbox |
| Resident API probes (read-only GETs) | `/api/projects`, `/waves`, `/tasks`, `/tasks/<id>` (detail carries `deps`, `intent`, `acceptance`, `verification`, `evidence`), `/runs`, `/docgraph`, `/api/stream` (`: connected`) all OK |

An early hypothesis during inspection — that scoped stream keys miss scoped query keys — was refuted empirically: `qk.tasks/waves/epics` use plain `[name, project]` keys and the stream mapping matches them exactly. No change made there.

## Backend/CLI gaps reported to owners (not forked client-side)

- Fixture owner's 13-task real-harness repo is not published yet: no `scripts/test-real-work-project.sh`, no `docs/reports/real-work/fixture/report.md`, no registered seeded project. The journey's Tier-2 gate fails closed until it lands.
- `tusker docs browse/read/backlinks/check --json` do not exist in the installed binary (`unknown command: docs browse`, `unknown command: docs check`); the surface is `find/new/map/status/verify/adopt`, with `docs_browse/read/check_cmd.go` in flight as untracked sibling work. Documents CLI parity (A7 CLI side) is blocked on that owner.
- Run parity (server-read, no execution): `POST /api/tasks/:id/run` routes to `handleTaskRunDirective`, which records an operator one-shot directive for the resident daemon to consume (`tusker task run`); the UI `useRunTask` and the CLI share that operation and operator-actor authority. Live Run/Execute clicks stay manual (see walkthrough) because they queue real daemon work.

## Acceptance matrix

| ID | Verdict | Evidence |
|---|---|---|
| A1 | Contract proven deterministically; live click pending | Inspector intent/acceptance/identity rules unit-tested incl. rendered markup; journey A1 coded (inspector + `Execute <id> once` / readiness check) but not executed live |
| A2 | Same as A1 | DAG edge/selection assertions coded against Alpha/Beta; live pending fixture |
| A3 | Fix landed + unit proof; live pending | `topologyKey` stability across the live-update pair; entry-view latch; journey asserts zoom-% stability and selection marking |
| A4 | Fix landed + unit proof; live pending | Shared-subscription test; reconnect/replay-miss/malformed/duplicate coverage; `StreamStatusNote` both states rendered; journey asserts the Reconnecting note with the stream blocked |
| A5 | Read-only, conditional | Journey offers retry assertion only when a failed/interrupted run exists, else SKIP; never fires actions |
| A6 | Latch landed; live pending | Completed entry leads with results (unit); journey asserts Results-lead + Flow one action away, else SKIP |
| A7 | UI path coded; CLI blocked | Journey reader assertions (title leads, `.tk-prose` visible); CLI verbs missing — owner gap above |
| A8 | Preserved + coded; live pending | Narrow viewport, Escape close, zero-overflow assertion; existing focus-trap/mermaid coverage untouched |
| A9 | Probed live (API) + coded (UI) | Resident API returns stable wave/task identities; journey compares member IDs, coded |
| A10 | Latch + reset discipline; live pending | Selection/view reset on wave change; journey reload-convergence check coded |

## Screenshots and critique

No new screenshots: this sandbox cannot bind loopback ports (no test Serve/UI server) and headless Chrome starts but its target closes immediately, so no new-code pixels could be captured honestly. The prior `docs/reports/wux/integration/*.png` fixture previews are explicitly NOT promoted to live proof. The per-iteration fresh-critic step runs once screenshots exist; the journey writes them to this directory (`standalone-task.png`, `wave-dag.png`, `waves-tablet.png`, `wave-results.png`, `documents.png`, `board-narrow.png`).

## First remaining failure

Run the journey against the fixture owner's seeded project from a machine that can serve the candidate UI and launch headless Chrome:

```sh
cd internal/serve/ui
TUSKER_REALWORK_BASE_URL=http://127.0.0.1:5193 \
TUSKER_REALWORK_PROJECT=<seeded-project-id> \
node test/real-work.browser.mjs
```

Expected until then: exit 2 with `READINESS WIN2` listing the missing fixture shape. The first real failure to close after the fixture lands is a live A1/A2 pass, followed by screenshots + fresh critic review.
