---
title: "Tasks and waves"
subject: delivery-and-waves
part_of: overview
status: canonical
---

# Tasks and waves

A task is a durable record whose body is its implementation contract. A wave
is the durable record that groups a batch of tasks, their dependency edges,
shared context, and landing data. Both are authored directly; there is no
intermediate plan document in the canonical path.

## Authoring

- `tusker new task --title <title> --work-level light|standard|demanding
  --body-file <path|->` creates one task. `work_level` is required; review
  inherits it unless `review_level` plus a task-specific `review_reason`
  override. `--epic`, `--spec-refs`, `--dependencies`, `--owned-paths`,
  `--generated-outputs`, and contact fields are optional; every supplied
  spec ref must resolve.
- `tusker wave create --file <request.yaml> --request-key <stable-key>`
  atomically authors a complete `tusker.wave-authoring/v1` request —
  temporary keys, bodies, dependencies, and human gates — into durable task
  records, gates, and one disarmed wave. An identical request key and
  fingerprint replay to the original durable IDs recorded in the wave's
  immutable authoring receipt; a changed request under the same key
  conflicts.
- `tusker task update <TASK-ID> --if-revision <state_rev> ... --by <actor>`
  mutates mutable authoring fields under the material lock and preserves
  identity, history, and proof lineage; a material change on a done or
  review task reopens it as rework.
Creation is inert. Nothing marks work ready, arms anything, or dispatches.

## Wave shared context

A wave request's `shared_context` is stored once on the wave under
`## Shared context`. Task packets project it under `## Wave context` with
the wave ID and outcome so wave members read shared intent first, while the
task body stays the task-specific contract. Receipt and administrative
fields are never projected into packets.

## Review and start authority

`tusker wave review <WAVE-ID> --json` projects state, authorization, the
material fingerprint, member eligibility, dependency frontiers, blockers
with repair actions, and the state-appropriate controls.

`tusker wave start <WAVE-ID> --mode background --by
human:<name>|operator:<name>` validates durable material, routes, gates, and
ownership, arms `authorization: armed` bound to the exact current
fingerprint, and queues eligible roots as durable `run_directives`. One
Start is the whole authorization: daemon polling reconstructs and releases
each dependency frontier automatically as prerequisites complete, up to the
wave's `concurrency`. An offline daemon leaves the wave authorized and
Waiting — never falsely Running — and recovers the frontier on its next
poll.

`tusker wave pause <WAVE-ID> --by human:<name>|operator:<name>` blocks new
wave-owned admissions while admitted attempts
finish, preserving the authorization fingerprint, actor, and timestamp.
`tusker wave resume <WAVE-ID> --by human:<name>|operator:<name>` restores `armed` only while the current material still
matches the stored fingerprint; drifted material is refused rather than
silently reauthorized. An explicit `tusker task start` inside a paused wave
converts that task's queued directive to task scope and leaves the wave
paused. `tusker task start <TASK-ID> --mode interactive --by <agent>
--current-workspace` claims one task in the current workspace.

## Landing

Each landing binds the reviewed task, source revision, integration base, and
proof facts. One wave landing must not silently overwrite another landing or
a user change.

## Code sources

- `cmd/tusker/direct_authoring_cmd.go` — task create/update and atomic wave authoring
- `cmd/tusker/direct_wave_authority.go` — wave review projection, start/pause/resume, frontier queueing
- `cmd/tusker/daemon.go` — per-poll frontier advancement
- `cmd/tusker/runtime_store.go` — run directives and wave execution scope
- `cmd/tusker/v7_land_cmd.go` — serialized merge lane
- `internal/v7schema/schema.go`
