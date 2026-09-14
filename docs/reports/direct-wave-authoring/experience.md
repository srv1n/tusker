# Direct wave authority — Serve/UI experience

FLW-T-0046. The UI now reads and drives the canonical direct-wave authority
endpoints only. No surface asks for a delivery plan, plan path, fingerprint
confirmation, mark-ready, arm, or Play.

## Endpoints and DTOs

| Surface need | Endpoint | DTO |
| --- | --- | --- |
| Wave review (sole read) | `GET /api/projects/{project}/waves/{wave}/review` | `tusker.wave-review/v1` → `WaveReview` |
| Wave authority (sole controls) | `POST /api/actions/projects/{project}/waves/{wave}/start\|pause\|resume` | `tusker.direct-start/v1` → `DirectStartResult` |
| Task start (sole task control) | `POST /api/actions/projects/{project}/tasks/{task}/start` | `tusker.direct-start/v1` → `DirectStartResult` |

Wire types (`internal/serve/ui/src/types/domain.ts`): `WaveReview`,
`WaveReviewMember`, `DirectStartBlocker`, `DirectStartControl`,
`DirectStartResult`. Wave controls submit the Serve operator actor resolved
from `/api/capability`; Start and task Start submit `mode: "background"`.

Control mapping: `review.controls` is authoritative — the UI renders exactly
the enabled `wave start` / `wave pause` / `wave resume` / `task start` control
the review projects, with its backend `reason`. Disabled controls are rendered
disabled (or absent when the backend withholds them), never re-enabled by
local state. `authorization: authorized` with no live attempt stays `Waiting`
— never displayed as Running.

## Surfaces

Four surfaces consume the shared direct authority UI:

1. **WorkExperience** (`features/workbench/integration/WorkExperience.tsx`) —
   WorkWave header renders compact `WaveAuthorityControls` and the body
   renders `WaveReviewDetail` with `showControls={false}` (review/member/
   instruction content, no duplicate authority button). Flow/Results
   controls unchanged; no local wave-status guard.
2. **DeliveryScreens WaveDetail** (`features/product/DeliveryScreens.tsx`) —
   actions are compact `WaveAuthorityControls`; a nonduplicative
   direct-review section renders outcome, members, frontiers and blockers from
   the same DTO. Empty copy refers to authored waves, not reviewed plans.
3. **DeliveryReview** (`features/delivery/DeliveryReview.tsx`) — rewritten
   from plan inbox into the shared exports `WaveAuthorityControls` and
   `WaveReviewDetail`. Renders state/authorization, blockers (`code`,
   optional `taskId`, `reason`, `action`) inline with a `/settings` link for
   route blockers and canonical task links for task blockers, member state,
   waiting reasons, routes, dependencies, and expandable full
   instructions/acceptance/verification. `WaveReviewDetail` accepts
   `showControls?: boolean` (default true) so surfaces with header controls
   embed the detail without a duplicate button. Stable attributes:
   `data-wave-authority`, `data-wave-state`, `data-wave-control`,
   `data-wave-review`, `data-wave-member`, `data-wave-instructions`.
4. **Task surfaces** (`features/product/TaskScreens.tsx`,
   `features/workbench/inspector/TaskInspector.tsx`,
   `features/docs/TaskContract.tsx`) — all use `useTaskStart` with
   `Start task` / `Starting…` / `Authorized — waiting for runtime` labels,
   `aria-label="Start task <ID>"`, and task-scoped semantics (a task Start
   inside a paused wave leaves the wave paused). `taskRunBlocker` no longer
   rejects backlog or readiness-held tasks and no longer requires a started
   wave; it blocks review/in-progress/terminal, open human gates, unfinished
   dependencies, and route blockers with actionable text.

Navigation: `Plan` is removed from `Sidebar`, `ProjectStrip`, and the
`/p/$projectId/plan` route in `router.tsx`. Work/Waves is canonical.
`DeliveryReview` remains as a shared component, not a plan route.

## Accessibility and narrow viewport

- Blockers render reason and action as visible text (no hidden tooltips);
  human-gate actions stay visible.
- Controls are real buttons with stable `data-wave-control` and aria labels;
  duplicate clicks are suppressed while a mutation is pending.
- Mutation results (reason, queued count, refusals) are announced via
  `ActionResultLine` in an `aria-live` region.
- Member instructions/acceptance/verification are keyboard-operable
  `<details>/<summary>` disclosures.
- 390x844: wave and task surfaces must have no horizontal overflow and keep
  authority controls visible and focusable.

## Backend

`directTaskBackgroundStart` eligibility tightened to an exact whitelist:
only `backlog`, `ready`, `rework` are startable (planned backlog remains
eligible even when readiness is held); every other status — `review`,
`in_progress`, `blocked`, `idea`, unknown — refuses with `task <id> is
<status>; task start accepts backlog, ready, or rework`; terminal states
keep their specific phrasing. No readiness-held refusal was added.
`taskRunBlocker` mirrors the whitelist client-side.

Serve handlers exercised: `handleWaveReviewAPI`,
`handleWaveStartAction`, `handleWavePauseAction`,
`handleWaveResumeAction`, `handleTaskStartAction` — all use the configured
Serve operator actor.

## Tests

Backend — `cmd/tusker/direct_wave_serve_test.go` (all `TestDirectWaveServe*`,
fixture/local only, no process/provider):

- `TestDirectWaveServeReviewReturnsCanonicalProjection` — schema/state/
  members/controls present; no plan/path/factory fields.
- `TestDirectWaveServeStartPauseResumeUseServeOperator` — POST paths use the
  configured Serve operator and produce expected state transitions.
- `TestDirectWaveServeTaskStartQueuesPlannedBacklog` — planned backlog/held
  task Start succeeds and queues the exact task.
- `TestDirectWaveServeTaskStartRefusalsCreateNoDirective` — review,
  route-blocked, dependency-blocked and human-gated tasks refuse and create
  no directive.
- `TestDirectWaveServeChangedWaveReportsStale` — changed wave reports changed
  material and stale control truth.

UI — `internal/serve/ui/test/direct-wave-authority.test.ts` (server-rendered
with seeded TanStack Query `WaveReview` fixtures):

- Planned/Waiting/Running/Paused/Completed/blocked states; only the correct
  enabled control; Waiting never rendered as Running.
- Every blocker's reason and action text; route-settings link; task links.
- No plan/path/fingerprint-confirmation/Play/arm/preflight wording.
- Source contract: `WorkExperience` and `DeliveryScreens` use
  `WaveAuthorityControls`; `TaskScreens`/`TaskInspector`/`TaskContract` use
  `useTaskStart` with no old start hooks; `Sidebar`/`ProjectStrip`/`router.tsx`
  contain no Plan route.
- `taskRunBlocker` allows backlog/held, blocks route/human/dependency/
  review/terminal cases.

Updated legacy tests: `pilot-start-readiness.test.ts`,
`wux-integration.test.ts`, `wux-inspector.test.ts`,
`task-authoring-experience.test.ts`.

Browser — `internal/serve/ui/test/task-authoring.browser.mjs` (read-only):
fetches the selected wave's direct review and requires
`tusker.wave-review/v1`; desktop wave route asserts state/control/blockers/
member instructions, keyboard Enter on an instructions disclosure, reload
fingerprint/state comparison; 390x844 wave route overflow and control
focusability; blocked waves suppress Start with reason/action visible;
`/plan` is asserted absent from navigation; all mutation requests captured
and asserted empty. Missing endpoint/scenario exits readiness (NOT RUN).

## Proof

- Fixture/local Go tests: `go test ./cmd/tusker -run '^TestDirectWaveServe'
  -count=1 -v` — **5/5 PASS**.
- UI typecheck: `bun run typecheck` — clean.
- UI unit/source tests: `bun test test/direct-wave-authority.test.ts
  test/task-authoring-experience.test.ts test/wux-inspector.test.ts
  test/pilot-start-readiness.test.ts test/wux-integration.test.ts` —
  **32 pass, 0 fail**.
- Build: `bun run build` — succeeds; `go build ./cmd/tusker` — succeeds;
  `git diff --check` — clean.
- Browser: **NOT RUN** — `python3 scripts/test-task-authoring-journey.py
  --mode browser` returned `NOT RUN` (no supplied service URL/project).
  The lane is read-only and records readiness, never a fixture pass.
- Provider/live execution: **NOT RUN** — no provider or resident daemon was
  invoked; fixture-local proof only.
