# Factory rollout blockers — 2026-07-28

## Scope

Read-only audit of the resident runtime before the first live Tusker pilot.
This report records facts and remedies only; it does not authorize a daemon,
project, wave, runner, redrive, ref move, release, or configuration change.

## Parked-run finding

The runtime contains 78 terminal `parked_no_progress` rows. They have no active
attempt ID, PID, process group, lease owner, or retry schedule, so starting the
daemon cannot revive them.

| Project | Count | Cause |
| --- | ---: | --- |
| cinta | 36 | Authorized Codex executable preflight failed before an attempt. |
| CarelessWhisper | 22 | Authorized Codex executable preflight failed before an attempt. |
| backend | 17 | Authorized Codex executable preflight failed before an attempt. |
| backend | 3 | Attempt cap reached after prior exit-1 failures. |

No parked row belongs to the Tusker project. The affected projects are disabled.

## Required sequence before any recovery

1. Repair and prove the configured Codex executable and daemon-effective PATH.
2. Verify discovery with `tusker runner catalog --json`.
3. Redrive only the 75 preflight-blocked rows with the runner repair recorded.
4. Inspect the three attempt-cap rows' original exit-1 cause before any
   individual redrive.

Do not bulk-redrive. The Plan 21 runner pre-claim health task is the product
fix for the first 75 rows; the later typed wait/stall task owns bounded retry
and escalation behavior.
