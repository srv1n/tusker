---
capsule:
  what: "MVP cutover readiness: truthful operations, parity proof, then one human-armed wave-of-one shadow/staging run."
  use_when:
    - "Deciding whether Tusker's factory control loop may run its first bounded dogfood wave."
  skip_when:
    - "Planning Plan 17/18 strict authority or provider-backend expansion."
---

# Factory MVP cutover

## Decision

**Current verdict: NO-GO.** This is deliberately a small cutover, not a
back-door authorization for the full factory program. It may proceed only when
the operations projection is truthful, its parity E2E is green, and one
low-risk task is in an already-created wave containing exactly that task. The
human must explicitly arm that exact wave for a shadow/staging run.

The MVP scope is frozen to:

1. the truthful operations surface (CLI/API/Serve/Desktop consume the same
   projection);
2. the narrow operations-parity E2E slice; and
3. one explicitly armed, wave-of-one, shadow/staging run.

It does **not** authorize broad `all_eligible`, a second project, automatic
rollout, replacement of the existing authoritative conductor, default-ref
promotion, release, or paid work.

## Current blockers

Evidence on 2026-07-26:

- `tusker wave preflight W-0003 --json` is red: W-0003 is disarmed; the
  project is unregistered/disabled; and no managed daemon is alive. Its ten
  members are still unimplemented, including the planned operations surface
  (`ORC-T-0046`) and broad control-loop E2E (`ORC-T-0047`).
- `tusker wave brief W-0003 --json` reports 0/10 delivered and 10 parked for
  machine rework. W-0003 is therefore not the MVP wave to arm.
- No existing wave-of-one or reviewed MVP operations-parity artifact is named.
  Do not relabel the full multi-project `ORC-T-0047` contract as an MVP proof;
  its extra scheduler, review, integration, crash, and compatibility scope is
  outside this cut.
- `tusker daemon service status --json` reports no installed/loaded service;
  `tusker daemon status --json` reports no live daemon. Do not repair either
  during MVP implementation without a separate human approval.
- `tusker delivery rollout doctor --json` reports service drift and unrelated
  registered-project drift. Those findings are not permission to widen this
  MVP; select one isolated target only after its own preflight is green.

## Read-only preflight

Run these from the intended target repository. They do not install, register,
enable, arm, start, promote, push, or spend.

```bash
tusker setup doctor --repo . --json
tusker skill doctor --strict --json
tusker validate --json
tusker projects list --json
tusker daemon status --json
tusker daemon service status --json
tusker automation status --json
tusker automation queue --repo . --json
tusker wave preflight <MVP-WAVE-ID> --json
tusker wave brief <MVP-WAVE-ID> --json
```

`<MVP-WAVE-ID>` is an existing, named wave whose membership is exactly one
low-risk task. `tusker wave preflight W-0003 --json` is a valid read-only
command but is evidence of the broader blocked program, not a candidate to
arm.

The preflight passes only if the target's projection is internally consistent,
the target is the only project proposed for enablement, the selected wave has
one member and a current fingerprint, its task is low-risk and has green
operations/parity proof, the runner is non-interactive with an explicit cap,
the workspace is isolated, and the integration/default refs are clean. A
missing feature, red parity test, stale fingerprint, unrelated ready task, or
ambiguous owner is a stop—not an invitation to improvise.

## Approval boundaries

| Boundary | Human must explicitly approve | Never implied by |
| --- | --- | --- |
| Binary/skill install | Exact binary and install/sync target | A green test, doctor, or this runbook |
| Project enable | One repository, `armed_waves` scope, runner/cap/workspace | Registration, import, or daemon availability |
| Wave arm | One wave ID, current fingerprint, one task, shadow/staging intent | Project enable or preflight success |
| Daemon start | Managed service/process and selected target | Wave arm or an agent session |
| Default-ref promotion | Exact ref and promotion policy after reviewed evidence | Integration success or task closure |
| Spending | Budget, model/provider, and ceiling | A runner profile or retry |
| Release | Artifact/environment and release authority | A passing staging run or default-ref promotion |

The approval should name the target, wave, task, current fingerprint, cap, and
expiry. Agents must not execute the corresponding install/enable/arm/start/
promote/release/spend action merely because it appears in this document.

## One-task, zero-ceremony path

For ordinary interactive work, do not manufacture a delivery plan, import,
wave, daemon, or approval ceremony. Use the normal one-task ownership path:

```bash
tusker show <TASK-ID> --capsule
tusker packet <TASK-ID> --for agent
tusker work start <TASK-ID> --by agent:<name> --source codex --json
# implement and run the task's mapped proof
tusker work submit <TASK-ID> --by agent:<name> --deliverable "<summary>" --verification "<exact proof>" --gate-verdicts <A1=pass> --json
```

`work start` only records interactive ownership; it does not enable automation,
arm a wave, start a daemon, or launch a worker. Use this path until the MVP
preconditions below are objectively met.

## The single MVP run

1. Land and independently review only the operations surface and its parity
   E2E. Keep the existing conductor authoritative.
2. Select one low-risk task with no human gate, no provider credential need,
   no default-ref/release consequence, and a bounded model budget. Create its
   wave-of-one through normal planning outside this runbook; keep it disarmed.
3. Run the read-only preflight. Capture the current wave fingerprint and prove
   that the operations projection shows the same frontier, ownership, blocker,
   and next action as the E2E fixture.
4. After the separate approvals above, a human arms exactly that wave. The
   managed daemon may start only after its separate approval. Observe the run
   through `tusker wave brief <MVP-WAVE-ID> --json`, `tusker automation status
   --json`, and `tusker daemon status --json`.
5. The run is shadow/staging only. The legacy conductor remains authoritative;
   there must be one owner and one integration transaction. Any disagreement
   stops new reactor action before it can become authoritative.

## Rollback

On a projection mismatch, unexpected claim, budget/cap breach, stale
fingerprint, daemon-health failure, or integration ambiguity: stop fresh
claims by having the human disarm the exact MVP wave (or pause it for a short
diagnostic hold). Do not delete run history, rewrite task state, kill a live
lease blindly, move refs, or disable the legacy conductor. Preserve the wave
brief, operations projection, task/review history, and integration revision;
then return the task to the normal interactive path or resolve the named
machine/human blocker before another explicitly approved attempt.

## Success criteria

The MVP is complete only when all are true:

- the operations surface and its E2E parity proof are green and reviewed;
- read-only preflight is green for one current wave-of-one;
- the human approvals are recorded separately and match the observed target;
- exactly one task is claimed once, with no unrelated task, project, ref,
  release, or spend action;
- the surface tells the same truthful outcome as the E2E evidence; and
- the wave can be disarmed/paused without losing live-lease, task, review, or
  integration facts, while legacy execution remains available.

## Explicitly deferred: phase two

Plan 18's strict K1–K5 bootstrap and `exact-verification-authority-e2e`, the
Plan 17 strict convergence transaction, and provider-backend/full-gate
expansion are phase two. They are neither implemented nor accepted by this
MVP, and must not be smuggled in as a prerequisite repair or a post-run
expansion.
