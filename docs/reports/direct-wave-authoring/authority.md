# Direct wave/task review and start authority

Direct review/start replaces delivery-plan authority for canonical records.
`tusker wave review` is read-only: it reads durable wave/task/gate material
plus current route and runtime facts only — never plan paths, plan bytes,
source keys, factory input, or context input. `task start` and `wave start`
write authorization and runtime run directives; they never claim siblings,
unrelated waves, or persist a readiness/status edit to make a task claimable.

## Commands

```bash
tusker wave review <WAVE-ID> [--json]
tusker wave start <WAVE-ID> --mode background --by human:<name>|operator:<name> [--json]
tusker task start <TASK-ID> --mode interactive|background --by <actor> [--current-workspace] [--json]
```

- `task start --mode interactive` requires `--current-workspace` and a
  trusted Codex/Claude host session at the repository root on a named Git
  branch, exactly like `tusker work start`. It needs no daemon, automation,
  or project registration, and may claim a planned backlog/held task after
  the real dependency/gate/ownership/route checks pass. It claims only the
  selected task.
- `task start --mode background` and `wave start` require a registered
  project and valid configured execute + independent-review routes. An
  offline daemon is not a refusal: the authorized scope is persisted as a
  queued run directive and the result is `Waiting` for runtime, never
  `Running`.
- `work start` remains the lower-level ownership protocol and its
  ready/rework-only semantics are unchanged.

## Review projection

`wave review` and `GET /api/projects/{project}/waves/{wave}/review` share the
same `tusker.wave-review/v1` builder. Serve also exposes
`POST /api/actions/projects/{project}/waves/{wave}/start` and
`POST /api/actions/projects/{project}/tasks/{task}/start`; the POST body
carries `mode` and the operator actor comes from the configured Serve
authority.

### Planned wave (fixture label — no runtime rows)

```json
{
  "schema": "tusker.wave-review/v1",
  "waveId": "W-0001",
  "title": "Direct wave W-0001",
  "outcome": "Ship W-0001",
  "state": "Planned",
  "authorization": "inert",
  "materialFingerprint": "sha256:0daf...",
  "members": [
    {"taskId": "APP-T-0001", "title": "Direct APP-T-0001", "state": "ready",
     "executeRoute": "codex-exec", "reviewRoute": "codex-exec",
     "acceptance": ["A1 Works."], "verification": ["A1 command: go test ./x"],
     "instructions": "Do APP-T-0001."},
    {"taskId": "APP-T-0002", "state": "waiting",
     "waitingReason": "waiting for dependency APP-T-0001",
     "dependencies": ["APP-T-0001:hard"], "executeRoute": "codex-exec",
     "reviewRoute": "codex-exec"}
  ],
  "frontiers": [["APP-T-0001"], ["APP-T-0002"]],
  "blockers": [{"code": "DEPENDENCY_WAITING", "taskId": "APP-T-0002",
    "reason": "dependency APP-T-0001 is unfinished",
    "action": "complete APP-T-0001 first"}],
  "controls": [{"action": "wave start", "enabled": true, "scope": "W-0001"}]
}
```

### Running wave (runtime label — live owner lease)

A member claimed by a run directive or live lease reports `state: "running"`;
the wave reports `Running`. The lease is a runtime row, not durable task
material.

### Waiting wave (runtime label — authorized, daemon offline)

`wave start` with the daemon offline persists the wave authorization and
queues directives for eligible roots, then reports:

```json
{"schema": "tusker.direct-start/v1", "subject": "W-0001", "scope": "wave",
 "state": "Waiting", "authorization": "authorized",
 "materialFingerprint": "sha256:0daf...", "queuedTaskIds": ["APP-T-0001"],
 "replayed": false, "reason": "Authorized — waiting for runtime"}
```

A root human gate produces the same authorized `Waiting` state with zero
queued roots. A crash after authorization but before directive publication
leaves the durable armed fingerprint; re-running start recovers and queues
the roots — the wave is never falsely `Running`.

### Stale wave (fixture label — material drift after authorization)

Editing task contract material (instructions, acceptance, verification
check/coverage, dependencies, gates, routes) changes
`contract_fingerprint`, which changes the wave `materialFingerprint`. The
stored authorization fingerprint no longer matches:

```json
{"state": "Waiting", "authorization": "stale",
 "materialFingerprint": "sha256:new...", "controls": [
   {"action": "wave start", "enabled": true, "scope": "W-0001",
    "reason": "authorization is stale; re-authorize"}]}
```

Status/readiness-only edits do not change the fingerprint and do not stale
the authorization. An old `proof_contract` cannot certify a changed
contract: strict lineage validation fails against the new fingerprint.

A material edit to a `done` or `review` task performs a controlled rework
in the same transaction: the task drops to `status: rework` /
`readiness: ready` / `proof_status: pending` with acceptance and closure
fields cleared, so it no longer projects as `completed`, no longer counts
toward a `Completed` wave, and is not terminal-refused by start. Strict
lineage keeps certifying only the old fingerprint, so the amended task
reports `STRICT_PROOF_STALE` until reproven.

## Blockers and repair actions

| Code | Scope | Repair action |
| --- | --- | --- |
| `WAVE_MISSING` | global | choose an existing wave id |
| `MATERIAL_INVALID` | global/member | repair the wave material and rerun wave review |
| `MATERIAL_CYCLIC` | global | remove the cyclic dependency edge |
| `ROUTE_UNAVAILABLE` | global | repair WORKFLOW.md route configuration |
| `RUNTIME_UNAVAILABLE` | global | restore the runtime store and rerun wave review |
| `STRICT_PROOF_STALE` | member | re-certify strict proof for the task |
| `CONTRACT_FINGERPRINT_STALE` | member | rebind the task contract fingerprint with `tusker task update` |
| `DEPENDENCY_CONTRACT_INVALID` | member | rebind the dependency contract for the task |
| `ROUTE_INVALID` | member | repair the execute/review route configuration for the task |
| `DEPENDENCY_WAITING` | member | complete the listed dependency first |
| `HUMAN_GATE_OPEN` | member | complete the human gate for the task |
| `ACTIVE_OWNER` | member | wait for the owner to release or reclaim the lease |

Global refusals (missing subject, cyclic material, strict corruption,
route invalidity, unregistered project for background mode, ownership
collision on an eligible member) write no authorization, directive, or
status. Task-scoped waits (dependency, human gate) do not block eligible
roots or wave authorization.

## Material fingerprint

Wave material covers only durable truth: wave ID and outcome, members and
the dependency DAG, each task's `contract_fingerprint` (computed from
canonical D2 contract material when absent), gate contract/status/authority
receipts, execute/review route choices, owned paths, and the current
contents of explicitly referenced specs. Delivery plan schema, plan,
factory, context, and source-key projection fields are excluded.
`task new`/`task batch` write `contract_fingerprint` at creation and
`task update` recomputes it on every material mutation; lifecycle
status/readiness/state_rev/timestamps and proof result cells are excluded.

## Idempotency and recovery

- Duplicate same-material start replays and returns the existing
  authorization/directive; changed material under the same request returns
  stale with an explanation rather than silently re-authorizing.
- Task background start writes one task-scoped directive (empty `waveId`)
  bound to the task contract fingerprint and the exact actor.
- Wave background start writes the armed authorization then durable
  wave-scoped directives for eligible roots, atomically tied to the exact
  authorization fingerprint. A deterministic test hook injects a crash in
  the post-authorization/pre-queue window; the recoverable intent stays
  `Waiting`.
- D8 autonomous advancement, pause/resume, and frontier recovery are
  covered by docs/reports/direct-wave-authoring/autonomous.md: daemon
  polling releases each dependency frontier under the stored authorization
  fingerprint, `wave pause`/`wave resume` preserve the exact authorization
  identity, and a task-scoped `task start` inside a paused wave replaces
  only that task's queued wave directive.
