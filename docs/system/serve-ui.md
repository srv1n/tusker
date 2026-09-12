---
title: "Serve UI"
subject: serve-ui
part_of: overview
status: canonical
---

# Serve UI

Serve is a local HTTP service and embedded web app. It reads repository tracker
state and the shared runtime store.

## Service

The default project policy binds Serve to `127.0.0.1:7420`. The service returns
the embedded web files and an unversioned `/api` surface. Read endpoints cover
projects, tasks, epics, waves, gates, evidence, documents, runs, executions,
delivery review, and diagnostics.

Mutations use guarded handlers. The service checks method, origin, content
type, mutation capability, project identity, and operator identity where the
action needs them. The UI must show a typed refusal.

`/api/stream` sends change events. The browser uses them to invalidate cached
queries.

## Current web routes

Wave views show the authored expected outcome separately from the derived completion result. Task detail labels evidence as available, missing, kept, or expired; only available safe targets expose an Open action. Evidence presence never creates or satisfies a human gate.

- Today: `/` and `/p/<project>/`
- Work: `/p/<project>/waves` (grouped Waves) and `/p/<project>/tasks` (Board)
- Wave flow/results: `/p/<project>/waves/<wave>`
- Full task detail: `/p/<project>/tasks/<task>`
- Operations: `/p/<project>/diagnostics`
- Execution Operations: `/p/<project>/diagnostics/executions`
- Task and tracker documents: `/p/<project>/docs`
- System knowledge: `/p/<project>/knowledge`
- Project settings: `/p/<project>/settings`

The TypeScript source is the web source authority. `internal/serve/ui/dist/` is
a generated embed. Rebuild it after a web source change.

The Work surface uses live project, wave, task and run reads. Start readiness,
durable tags and stage-specific transport identity remain unavailable until the
Serve API exposes authoritative contracts; the UI does not infer or persist them.

## Compact project navigation

The full-window shell uses one horizontally scrolling project strip below the
top bar. Every project checked under Settings → All Projects appears once;
hidden and unhealthy registrations remain listed there. Visibility does not
change automation or registration. Pinned projects lead in saved pin
order, while unpinned projects use most-recent project switches on the next
shell mount and remain stable during that mount. Switching a project restores
its last valid internal route, including known checkout aliases. Invalid,
external, cross-project or removed routes fall back to that project's Work
surface without changing project identity. Older navigation records migrate in
place, preserving routes and opaque view state; unavailable local storage only
disables persistence, not navigation.

The strip keeps native horizontal overflow, keyboard focus reveal and compact
previous/next controls at overflow edges. Add project and project refresh stay
available as secondary controls, and the selected project exposes Work,
Documents, project Settings and the existing Plan, Board, Trains and
Diagnostics destinations. Global Search and App Settings plus project Settings
are icon-only controls with accessible names and tooltips. Global routes clear
project-current styling and hide project-local navigation. The embedded
`/panel?shell=1` route keeps its compact triage shell and does not receive the
desktop strip.

The source authority is `internal/serve/ui/src/features/workbench/navigation/`
and `internal/serve/ui/src/routes/__root.tsx`; the generated embed under
`internal/serve/ui/dist/` is rebuilt after source changes. The focused evidence
for this surface is recorded in
[`docs/reports/project-strip/report.md`](../reports/project-strip/report.md).

## Model settings

`GET /api/models` returns the same versioned effective configuration as
`tusker models show`: profile definitions, three level/lane mappings, field
provenance, override state and a revision. `POST /api/models` accepts `set`,
`reset`, `profile-set`, `profile-disable`, `profile-enable`, or `profile-remove`;
every write uses the shared validator and rejects a stale revision. The response
includes profile states, reference summaries, and reference-check completeness.
`GET /api/models/catalog` exposes the installed-harness catalog. Append
`?refresh=1` only for an explicit metadata refresh; it refreshes discovery and
never starts a model turn.
Run responses expose only recorded profile, harness, model and effort values;
missing execution identity stays empty.
`POST /api/tasks/<task-id>/route` writes optional work/review tiers and
worker/reviewer profile overrides with the task's current revision. It affects
future dispatch only; active attempts keep their recorded identity.

Global Settings exposes this contract as **Agents**, with separate **Profiles**
and **Tiers** views. Profiles show only a readable name, model/effort/access,
eligibility, and test state; editing exposes the full configuration. A saved
profile can be checked or explicitly live-tested by stable profile ID, so the
conformance report binds its actual model, effort, access and transport.
Tier rows select only enabled eligible profiles; the first is primary and later
entries are explicit fallbacks. Project Settings labels inherited values and can
override or reset a lane without changing another project's defaults. The former
global Permissions placeholder is not an enforcement surface: access belongs to
the profile and independent project/system authorization still applies.

The profile access contract is resolved before execution and is retained with the
run identity. Native routes compile the supported workspace, network, write,
shell, and approval controls into their CLI invocation; a required control that
the selected route cannot express blocks the run rather than silently falling
back. `muse_cli` is the direct `muse exec --json` route and is distinct from the
legacy Muse-compatible profile route. The no-spend setup check can prove route,
installed executable metadata, and provider-free fixtures; it does not qualify a
paid or live model turn.

## TuskerBar

TuskerBar probes the default local endpoint. It reuses a healthy daemon or
starts the bundled daemon. It does not store task state.

## Code sources

- `cmd/tusker/serve_command.go`
- `cmd/tusker/serve_actions.go`
- `cmd/tusker/serve_*.go`
- `internal/serve/ui/src/router.tsx`
- `internal/serve/ui/src/lib/api.ts`
- `internal/serve/ui/src/lib/queries.ts`
- `apps/mac/TuskerBar/Sources/TuskerBar/RuntimeSupervisor.swift`
