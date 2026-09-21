# W-0031 cross-wave dependency qualification

Date: 2026-09-20. Scope: the shared source checkout only. This report does
not change tracker state, start a daemon, dispatch automation, install a
build, call a provider, or touch `/private/tmp/tusker-walkthrough-20260920`.

## Verdict

**PASS at the deterministic current-checkout boundary.** The standard
four-wave demo fixture seeds valid c1 → a4/b4 hard dependency contracts; a
human can authorize W-0004 while its Beta prerequisite is unfinished; when
Beta completes, the production frontier continuation queues c1 exactly once
under the stored authorization and is idempotent; the follow-up then
completes through the deterministic offline runner with exactly one accepted
completion per member; and the integrated UI renders the same wire DTO
truthfully at each step. This is not a browser, installed-product, or
provider qualification.

## Build identity and working material

| Field | Observed |
| --- | --- |
| Source HEAD | `c849629b920e620f535c64c32cf5377b8daaacd1` |
| Checkout state | dirty/shared (128 porcelain entries; unrelated concurrent work preserved) |
| UI runtime | Bun `1.3.14` |
| Installed build identity | NOT RUN / unknown |
| Browser URL and registered project | NOT supplied |

## Executed evidence

| Coverage | Command | Result |
| --- | --- | --- |
| A1–A5 (Go) | `go test ./cmd/tusker -run '^TestDemoCrossWaveAuthorization$' -count=2` | PASS: `ok tusker/cmd/tusker 67.896s`. Two independent deterministic repetitions of the single test in one invocation; each run allocates a fresh `t.TempDir()` repo, state root, and binary-built fixture, seeds once, and never reseeds, resets, or cleans up between boundaries. |
| A1–A4 (UI) | `bun test --cwd internal/serve/ui test/walkthrough-cross-wave-integrated.test.ts` | PASS run 1: 4 tests, 24 assertions. |
| A1–A4 (UI) | `bun test --cwd internal/serve/ui test/walkthrough-cross-wave-integrated.test.ts` | PASS run 2: 4 tests, 24 assertions. |
| UI integration | `bun run --cwd internal/serve/ui typecheck` | PASS: `tsc --noEmit` clean. |
| Source hygiene | `git diff --check` | PASS. |

## What the Go test proves per boundary

`TestDemoCrossWaveAuthorization` (`cmd/tusker/demo_cross_wave_test.go`)
retains one seeded fixture end to end — same repo, manifest, task documents,
runtime DB, attempts, and receipts across every observation.

1. **Seed.** `demo seed` of the standard scenario maps 4 waves / 13 tasks;
   c1 carries exactly two hard `dependency_contracts` rows pinned to a4's and
   b4's current `directWaveTaskContract` fingerprints; the canonical W-0004
   review reports neither `DEPENDENCY_CONTRACT_INVALID` nor
   `CONTRACT_FINGERPRINT_STALE`; authorization is `inert`.
2. **Alpha only.** `demo run --waves alpha --fast` completes Alpha while b4
   stays unfinished. Direct review and the Serve
   `GET /api/projects/<id>/waves/W-0004/review` DTO agree: a4 is
   `external`/`done` with its Alpha title and wave; b4 is `external`,
   unfinished, with its Beta title and wave; c1's `waitingReason` is exactly
   `waiting for dependency <b4>`; the `wave start` control is enabled.
3. **Authorization.** One `directWaveStart` by `human:test-operator` returns
   `authorized` with zero queued task IDs. Direct and Serve re-reads show
   `authorized`, c1 still waiting only on b4, and no enabled `wave start`
   control.
4. **Beta + continuation.** `demo run --waves beta --fast`; a4/b4 docs are
   `done`. One `queueAuthorizedWaveFrontier` call — the same seam the daemon
   drives — queues exactly `[c1]`, bound to W-0004's stored
   `authorization_fingerprint`/`authorized_at`. A second call queues nothing
   and the active c1 directive count stays one.
5. **Follow-up.** `demo run --waves follow-up --fast` completes c1–c4 without
   a second Start; the queued directive presented no conflict to the offline
   claim path and remains in the store as history. Interval evidence shows
   c2/c3 start only after c1 finishes and c4 only after c2/c3; each follow-up
   task records exactly one `done` interval. `demo check` reports PASS on
   every applicable assertion.

## Acceptance map

| ID | Evidence | Status |
| --- | --- | --- |
| A1 | A fresh `demo seed` persists valid hard c1 → a4/b4 dependency contracts pinned to current producer fingerprints; the canonical W-0004 review reports neither `DEPENDENCY_CONTRACT_INVALID` nor `CONTRACT_FINGERPRINT_STALE`. | PASS |
| A2 | With Alpha done and Beta unfinished, the direct review, the Serve review DTO, and the integrated UI fixture agree that Beta is the only remaining external prerequisite (a4 external/done, b4 external/unfinished, c1 `waitingReason` names b4 only, exact `Waiting for Beta` copy). | PASS |
| A3 | One native `directWaveStart` authorizes W-0004 while waiting and queues nothing; after Beta completes, `queueAuthorizedWaveFrontier` queues c1 exactly once — bound to the stored authorization fingerprint/`authorized_at` — without another Start, and a repeat call is a no-op. | PASS |
| A4 | `-count=2` provides two fresh deterministic repetitions; each run shows c2/c3 starting only after c1 finishes, c4 only after c2/c3, exactly one `done` interval per follow-up member, the queued directive retained as history, and `demo check` PASS. | PASS |
| A5 | The boundary table above separates deterministic/source, built UI, installed application, browser, provider/worker, and human acceptance as PASS or NOT RUN; fixtures are fresh `t.TempDir()` allocations and the retained campaign at `/private/tmp/tusker-walkthrough-20260920` was never read or used. | PASS |

## Evidence boundaries and NOT RUN

| Boundary | Status | Reason |
| --- | --- | --- |
| Source review | PASS | Current source inspected; the test exercises only production seams (`directWaveStart`, `queueAuthorizedWaveFrontier`, `newServeServer`, `demo run`). |
| Deterministic current-checkout Go test | PASS | `TestDemoCrossWaveAuthorization` passed both `-count=2` repetitions. |
| Built UI / typecheck | PASS | Source-level `bun test` + `tsc --noEmit`; not a browser or bundle execution. |
| Installed application | NOT RUN | No build/install/restart was authorized or performed. |
| Browser walkthrough | NOT RUN | No authorized service URL/project was supplied; no browser was opened. |
| Provider / worker attempt | NOT RUN | No daemon was started and no provider attempts were spent. |
| Human acceptance | NOT RUN | No operator walkthrough or screenshots were collected. |

No first divergence: every asserted boundary agreed between canonical task
documents, the direct review, and the Serve DTO on the checks above.
