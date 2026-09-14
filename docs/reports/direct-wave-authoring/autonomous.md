# Autonomous direct-wave execution (D8)

One `wave start` durably authorizes exact wave material. The resident daemon's
existing poll reconstructs and releases each dependency frontier
automatically — no second scheduler, no second Play. Pause and resume are
durable, visible, and preserve the exact authorization identity.

## Commands and controls

```bash
tusker wave start <WAVE-ID> --mode background --by human:<name>|operator:<name> [--json]
tusker wave pause <WAVE-ID> --by human:<name>|operator:<name> [--json]
tusker wave resume <WAVE-ID> --by human:<name>|operator:<name> [--json]
tusker task start <TASK-ID> --mode background --by <actor> [--json]
```

Serve exposes the same controls:

```text
POST /api/actions/projects/{project}/waves/{wave}/start
POST /api/actions/projects/{project}/waves/{wave}/pause
POST /api/actions/projects/{project}/waves/{wave}/resume
POST /api/actions/projects/{project}/tasks/{task}/start
```

## Behavior and state matrix

| Wave state | Authorization | What happens |
| --- | --- | --- |
| Planned / stale | `inert` / `stale` | `wave start` is the enabled control; start arms `authorization: armed` with the current `materialFingerprint`, `authorized_by`, `authorized_at`, then queues the current frontier through the shared helper. |
| Waiting / Running | `authorized` | `wave pause` is the enabled control; daemon polling keeps releasing eligible frontiers up to `concurrency`. |
| Paused | `paused` | `wave resume` is the enabled control. Queued wave directives no longer admit new work; admitted attempts continue to completion. Task-scoped `task start` stays enabled for eligible members with an explicit "wave remains paused" reason. |
| Completed | — | No enabled wave mutation. |

`wave start` refuses a paused wave with `wave is paused; use wave resume`.
`wave resume` refuses drifted material with an actionable stale-material
reason; it never silently reauthorizes changed material. Both pause and
resume commit the wave record and its `updated` event in one
preimage-guarded transaction under the material epoch + wave document lock,
preserving `authorization_fingerprint`, `authorized_by`, and
`authorized_at`.

## Frontier queueing and capacity

`queueAuthorizedWaveFrontier` is the single queueing authority, invoked by
`directWaveStart` after authorization, `directWaveResume` after the state
update, and the daemon's `advanceAuthorizedWaveFrontiers` on every project
poll. Under the material epoch + wave document lock it:

- proceeds only when `authorization` is exactly `armed`, the stored
  `authorization_fingerprint` equals the current material fingerprint, and
  `authorized_at`/`authorized_by` exist;
- reuses the direct review projection so only members whose required
  dependencies are complete and that pass route, gate, and strict-proof
  checks are candidates;
- enforces wave `concurrency` (default `max(1, members)`), counting both
  live member attempts and any active queued directive for member tasks —
  including task-scoped directives — toward capacity;
- queues the stable frontier order up to remaining capacity via
  `QueueWaveRunDirectives(..., enableProjectPolling=false)`;
- treats dependency/gate/route waiting as a visible no-queue condition,
  never a false `Running`; hard store or I/O errors return;
- never queues disarmed, paused, stale, or unrelated waves.

Duplicate calls are idempotent: identical directives reconcile to `already`
rather than duplicating authority.

## Pause continuity and task-scoped start

`runDirectiveMatchesTaskAuthority` and the dispatch admission fence stay
strict: a queued wave directive admits new work only while the wave is
exactly armed and non-stale, and the admission path reacquires the material
and wave locks and rereads the directive before claiming. A separate
continuity matcher — `runDirectiveAdmittedContinuityMatchesTaskAuthority`
and `consumedRunDirectiveContinuityMatchesTaskAuthority` — is used only by
daemon reconciliation of an already admitted lease/authorization; it accepts
armed or paused exact authorization identity so Pause never interrupts a
running attempt. The continuity matchers are not used by `dispatchRun*`, so
no new worker or reviewer starts after pause.

An explicit `task start --mode background` for a member of a currently
paused, non-stale wave atomically replaces that task's queued wave
directive with a task-scoped directive
(`ReplacePausedWaveDirectiveWithTaskDirective`, one fenced SQLite
transaction matching project/record/wave/fingerprint/authorized_at). The
wave stays paused; other members are untouched. Any different active
directive remains a precise conflict.

## Recovery and race invariants

- A crash after authorization but before queueing leaves the durable armed
  fingerprint; the next daemon poll (or a fresh `Daemon` on the same store)
  reconstructs the frontier and queues it exactly once.
- Competing frontier calls produce at most one directive per member and
  never oversubscribe `concurrency`.
- Daemon restart recovery needs no second Play and never auto-authorizes
  future waves; a wave with no stored armed authorization is skipped.
- Product blockers on one wave do not abort unrelated waves or the rest of
  project polling; only hard I/O/runtime failures propagate.

## Focused verification

```bash
go test ./cmd/tusker -run '^TestDirectWaveAutonomous' -count=1 -v
```

## Proof labels

All coverage is local executed fixture proof: canonical records and a real
SQLite runtime store inside temporary vaults. One test exercises an
in-process `Daemon.PollOnce` end to end (queued frontier asserted before the
deterministic no-spawn dispatch refusal); the rest drive
`advanceAuthorizedWaveFrontiers`/`queueAuthorizedWaveFrontier` on a `Daemon`
value directly. No resident daemon process was started and no provider-live
runner dispatch occurred — both are **NOT RUN** in this environment.
