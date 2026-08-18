# Serve UI capability and backend contract

This SPA is a client for Tusker's privileged local control plane. `USE_MOCK` is
`false` in the shipped build; fixture data is only an explicit development aid.
The UI must never imply that a local state change persisted unless the daemon
accepted it and a follow-up read confirms it.

## Capability classes

`GET /api/capabilities` publishes the machine-readable
`tusker.serve-capabilities/v1` registry. Every surface is one of:

| Class | Meaning | UI rule |
| --- | --- | --- |
| `authoritative_mutable` | Daemon accepts a durable action and owns readback. | Show controls only with the capability header; invalidate after acceptance. |
| `authoritative_read_only` | Daemon projection is authoritative but has no write path. | Render facts; do not offer edit controls. |
| `cached_projection` | Stream or cached data is advisory and may be stale. | Show freshness/connection state; never use as proof. |
| `local_preference` | Browser/native preference, not project or daemon state. | Label local and persist only through its owning shell. |
| `unavailable` | No backend contract exists. | Hide or disable controls and explain why. |

Mutations require the per-process `X-Tusker-Capability` returned by
`GET /api/capability`, JSON content type, and same-origin/loopback admission.
This is a threat limit for local cross-site requests, not user authentication:
any process able to read the local endpoint can act as the operator.

## Mutation semantics

The wire contract distinguishes accepted, refused/validation, conflict, and
transport failure. A 2xx response containing `ok:false` or `refused:true` is a
refusal, not success. `requireAccepted()` converts that in-band refusal into a
typed `ActionRefusalError`; consumers must surface its reason, avoid optimistic
state, and invalidate/read back on settled mutations. HTTP 409/422 retain their
typed conflict/validation payloads. Network/5xx errors remain transport errors.

## Current authoritative surfaces

Tasks, runs, gates, evidence, feedback, daemon actions, project registration /
automation / allowlisted project settings, setup doctor/repair, project removal,
execution binding, delivery start, and docgraph CAS saves are mutable only
through their Serve endpoints. Reads for projects,
tasks, runs, docs, graph, operations, and stream events are daemon-owned; stream
events are invalidation hints, not durable history. Reconnect/polling must be
treated as a freshness fallback until replay cursors are available.

The project-settings allowlist is `tier`, `automation.enabled_runners`,
`workspace.strategy`, and `runtime.max_active_runs_per_project`; automation's
`enabled` flag remains owned by the project automation endpoint.

## Deliberately unavailable or local-only

Legacy document editor state (`features/docs/editor.ts`) and frontmatter updates
must not claim persistence until their real CAS/write endpoints are wired.
Runner profiles, global permissions, notification delivery, density, and other
app-level settings marked `TODO(api)` are reference/local controls, not daemon
config. Project config is authoritative only for the allowlisted keys exposed
by `/api/projects/:id/settings`; setup doctor/repair and daemon registration
removal are also authoritative actions.
The UI now consumes the capability registry and disables unavailable profile
actions; do not add a success toast to a local-only control.

## Remaining contract work

- Add replay/cursor semantics to `/api/stream` and expose event sequence gaps.
- Add authoritative per-surface freshness/revision fields to read responses.
- Replace legacy docs editor mock save with the docgraph CAS contract or a
  clearly read-only source view.
- Add backend persistence for app-level settings (profiles, permissions,
  notifications, and density) before enabling those controls.
- Keep registry IDs, endpoint routes, and UI controls in a drift test.
