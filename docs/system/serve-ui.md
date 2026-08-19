---
title: Serve — local control plane and embedded SPA
subject: serve-ui
keywords:
  - serve
  - control plane
  - localhost API
  - SSE stream
  - snapshot cache
  - capability token
  - TanStack SPA
part_of: overview
status: canonical
read_when: "You are calling, adding, or debugging a `/api/*` Serve endpoint, reasoning about snapshot freshness or the SSE invalidation stream, or building/embedding the SPA in internal/serve/ui."
skip_when: "You need daemon dispatch, run lifecycle, task contracts, or gate semantics rather than their HTTP projection ([[orchestration]], [[execution-observability-system]], [[tasks-and-proof]], [[gates]])."
sources:
  - cmd/tusker/serve_command.go
  - cmd/tusker/serve_actions.go
  - cmd/tusker/serve_types.go
  - cmd/tusker/serve_capabilities.go
  - cmd/tusker/serve_stream.go
  - cmd/tusker/serve_delivery.go
  - cmd/tusker/serve_docgraph.go
  - cmd/tusker/serve_execution_graph.go
  - cmd/tusker/daemon_serve.go
  - internal/serve/assets.go
---

# Serve — local control plane and embedded SPA

`tusker serve` runs one `http.Server` whose handler is `*serveServer`
(`cmd/tusker/serve_types.go`). It answers `/api/*` as JSON and everything else
from the embedded SPA build. Reads are projections of the vault plus the
runtime SQLite store; mutations shell into the same command functions the CLI
uses (`serveInvokeCommand`, `cmd/tusker/serve_actions.go`). Treat readback, not
the POST status, as proof that anything changed.

## Process model

- `serveCmd` (`cmd/tusker/serve_command.go`) first calls
  `serveDeferToIncumbentDaemon`: if a live daemon already has embedded serve
  enabled, it prints/emits that daemon's URL and exits without binding.
- Bind address comes from `--addr`/`--listen`, or `--port` when the address is
  still the default. `serveNormalizeAddr` defaults to `127.0.0.1:7420` and
  **refuses any non-loopback host**.
- The daemon path is `(*Daemon).startServe` (`cmd/tusker/daemon_serve.go`): it
  picks a target project, listens itself, sets `server.stream` to the daemon's
  broker and `server.reconcileStatus` to the adaptive reconciler, then writes
  the serve addr into the daemon PID file. Only the daemon-hosted server emits
  run/task stream events (`daemon_stream.go` calls `stream.Broadcast`); a
  standalone `tusker serve` only ever broadcasts `projection_refreshed`.
- Both paths call `warmRegisteredProjectSnapshots` in the background and set
  `requireCapability = true`.

## Security boundary

`ServeHTTP` sets CSP (`default-src 'self'`), `X-Content-Type-Options`,
`Referrer-Policy: no-referrer`, `X-Frame-Options: DENY` on every response, then
routes `/api*` to `handleAPI` and everything else to `handleAssets`.

Mutations (`POST`, plus `PUT /api/docgraph/doc`) pass `mutationRefusal`:
loopback `Host`, same-origin `Origin`/`Referer` when present,
`Content-Type: application/json`, and a constant-time match on the
`X-Tusker-Capability` header against a 32-byte random per-process token.
Failure is `403` with `{ok:false,refused:true,reason:...}`. `GET
/api/capability` hands that token to any same-origin reader — this is CSRF
defense, **not authentication**; any local process that can reach the port owns
the control plane. Human mutations also require an explicit qualified operator
actor. Configure standalone Serve with `--by human:<name>`, or set
`TUSKER_SERVE_OPERATOR=human:<name>` for daemon-hosted Serve; the capability
bootstrap returns that actor and the SPA includes it on run and
delivery-start requests. There is no `$USER` fallback. Interactive
Codex/Claude and dispatched Tusker sessions cannot use that human actor.

Admission control: 128 slots for normal requests, a separate 32 for
`/api/stream` so idle SSE cannot starve reads; over budget returns `503`.
Handler panics become `500` when the response is not yet committed.

## Capabilities registry

`GET /api/capabilities` (`cmd/tusker/serve_capabilities.go`) returns schema
`tusker.serve-capabilities/v1` and a static, sorted list classifying each
surface as `authoritative_mutable`, `authoritative_read_only`,
`cached_projection`, `local_preference`, or `unavailable`. Current non-obvious
entries: `docs` is read-only (legacy editor writes do not exist), `docgraph` is
mutable (CAS-protected), `stream` is `cached_projection`, `app-preferences` is
`local_preference`, `profiles` is `unavailable`. Add a registry row in the same
change that adds a surface; the SPA gates controls on this list.

## Endpoint map

Reads are `GET` (`HEAD` allowed); anything else on a read path returns `405`
with `Allow: GET, HEAD`. `/api/stream` and `/api/capabilities` are `GET`-only.
Most reads accept `?project=<id>`; `/api/capability`, `/api/capabilities`,
`/api/daemon`, `/api/digest`, and `/api/projects` ignore it. Omitted means the
launch project (`projectForSnapshot("")` matches the registered project whose
vault/repo equals the server's, else synthesizes one).

| Method + path | Handler (file) | Notes |
| --- | --- | --- |
| `GET /api/capability` | `serve_command.go` | mutation token + configured operator actor, `no-store` |
| `GET /api/capabilities` | `serve_capabilities.go` | compatibility registry |
| `GET /api/stream` | `serve_stream.go` | SSE; `?project=` filter |
| `GET /api/daemon` | `serve_command.go` | `serveDaemonStatus` projection |
| `POST /api/daemon/{start,stop,resume,limits}` | `serve_actions.go` | wraps `daemon*Cmd`; `start` replies after 250 ms |
| `GET /api/projects` | `serve_command.go` | includes disabled projects |
| `POST /api/projects` | `serve_actions.go` | register a project |
| `POST /api/projects/<id>/{automation,settings}` | `serve_actions.go` | automation toggle, project settings |
| `POST /api/projects/<id>/refresh` | `serve_refresh.go` | one-way daemon `reconcile_project`; 2 s coalesce |
| `GET /api/summary` | `serve_command.go` | attention/review/running/failed counts |
| `GET /api/digest` | `serve_command.go` | `?since=`; built from the **launch** vault |
| `GET /api/morning-brief` | `serve_command.go` | `?date=YYYY-MM-DD` |
| `GET /api/needs` | `serve_command.go` | precomputed on the snapshot (`serve_needs.go`) |
| `GET /api/factory-operations` | `serve_command.go` | read-only projection |
| `GET /api/review/batch` | `serve_command.go` | capsules for tasks in `review` |
| `GET /api/runs` | `serve_command.go` | `?limit` (≤500, default 100), `?cursor`, `?all` |
| `GET /api/runs/<task-id>` | `serve_command.go` | detail + attempts + events |
| `POST /api/runs/<id>/{redrive,acknowledge,interrupt}` | `serve_command.go`, `serve_actions.go`, `serve_runs.go` | interrupt confirms lease + process state |
| `GET /api/executions` | `serve_execution_graph.go` | store `ExecutionGraph`, many filters; backs the SPA's Execution Operations surface |
| `GET /api/executions/inbox` | `serve_execution_graph.go` | unbound direct executions |
| `GET /api/executions/timeline` | `serve_execution_timeline.go` | `tusker.execution-timeline/v1`, cursor paged |
| `GET /api/executions/<id>/binding-preview` | `serve_execution_graph.go` | conflicts + next binding generation |
| `POST /api/executions/<id>/{rename,bind,cancel}` | `serve_execution_graph.go` | bind computes the canonical wave first |
| `GET /api/epics`, `/api/waves`, `/api/waves/<id>` | `serve_command.go` | vault projections |
| `POST /api/waves/<id>/land`, `/api/tasks/<id>/land` | `serve_actions.go` | same `handleLandAction` |
| `GET /api/tasks`, `/api/tasks/<id>` | `serve_command.go` | capsule / detail |
| `POST /api/tasks/<id>/{status,run,discard,close}` | `serve_actions.go` | `run` only records a directive |
| `GET /api/gates`, `/api/gates/<id>` | `serve_actions.go` | `?task=` filter |
| `POST /api/gates/<id>/{satisfy,waive,obsolete}` | `serve_actions.go` | see [[gates]] |
| `GET /api/evidence`, `/api/evidence/<id>`, `POST /api/evidence` | `serve_actions.go` | durable evidence |
| `GET /api/decisions`, `/api/decisions/<id>` | `serve_actions.go` | read-only |
| `GET /api/feedback`, `/api/feedback/<ref>`, `POST /api/feedback` | `serve_actions.go` | see [[knowledge-and-feedback]] |
| `GET /api/attempts`, `/api/attempts/<id>` | `serve_actions.go` | runtime attempt history |
| `GET /api/docs`, `/api/docs/<repo-path>` | `serve_docs.go` | read-only; path confined by `safeRepoPath` |
| `GET /api/docgraph`, `/api/docgraph/doc` | `serve_docgraph.go` | corpus, graph, issues |
| `PUT /api/docgraph/doc?subject=` | `serve_docgraph.go` | CAS save (below) |
| `GET /api/delivery/plans`, `/api/delivery/review` | `serve_delivery.go` | read-only |
| `POST /api/delivery/start` | `serve_delivery.go` | requires reviewed plan identity |
| `GET /api/roster` | `serve_command.go` | runner/handoff projection |

Unmatched `/api/*` is `404 {"error":"not found"}`. Unmatched non-API paths fall
back to `index.html` so client routing works; `assets/<name>-<hash>.<ext>` gets
`max-age=31536000, immutable`, everything else `no-cache`.

## Snapshots and freshness

`serveSnapshot` (`cmd/tusker/serve_types.go`) is a per-project in-memory
projection: workflow, notes bucketed by kind (task/epic/gate/wave/evidence/
decision/attempt), `docs`, `needs`, `runs`, automation `queue`, and an
`openP0Escalation` flag. `buildSnapshotForProject` loads project contents, then
calls `ListRunsForProjectPage` with a hard cap of 1000 runs — exceeding it is a
`RUNTIME_RUN_SNAPSHOT_LIMIT` error, never a partial snapshot.

Cache behavior in `loadSnapshotForProjectMode`:

- Entries are keyed by project ID (falling back to cleaned vault/repo path).
- A ready, valid entry is served immediately; if it is older than 30 s it is
  marked invalid and a background rebuild starts.
- A ready-but-invalid entry is still served stale unless the caller asked for a
  fresh build; concurrent builders wait on the entry's `done` channel.
- After a rebuild, `serveSnapshotContentHash` (`serve_snapshot_hash.go`) hashes
  only client-visible fields (timestamps and transport metadata excluded). A
  changed hash broadcasts a `projection_refreshed` stream event covering every
  major key.

Mutations invalidate explicitly: `invalidateProjectSnapshot` marks entries
invalid and clears the summary cache; `refreshProjectSnapshot` also kicks a
background warm.

## Stream

`GET /api/stream` is SSE with `text/event-stream`, `no-store`,
`X-Accel-Buffering: no`, a `: connected` preamble and a `: heartbeat` comment
every 15 s. Each client buffers 16 events; a client that cannot keep up is
closed and dropped, not blocked. The broker keeps a 128-event replay ring;
`Last-Event-ID` replays newer events for the subscribed project. A cursor that
is malformed, negative, ahead of the broker, or older than the ring emits a
`stream_replay_miss` event first.

Events carry `kind`, `keys`, and optional `project`/`task_id`/`urgency`/
`deep_link_path`. **Keys are cache-invalidation hints, not an audit log** —
`serveStreamRunKeys`/`serveStreamTaskKeys` build them, and the SPA maps each key
to TanStack query keys in `internal/serve/ui/src/lib/stream.ts`. When the stream
is disconnected the SPA falls back to a 45 s refetch interval
(`LIVE_STREAM_FALLBACK_MS`); a missing event is unknown, never proof of absence.

## Mutation outcomes

`serveActionResult` is the common shape. Distinguish four outcomes:

1. **accepted** — `{ok:true}`; invalidate and read back.
2. **refused** — `200` or `403` with `refused:true` and a `reason` (often a
   typed `Issue`). Not an error; not a success either.
3. **conflict** — `409` (docgraph `DOC_SAVE_CONFLICT`, execution bind/rename),
   `422` (`serveDeliveryError`, docgraph defects).
4. **transport** — network/timeout/5xx: the durable outcome is unknown, so
   re-read before retrying.

Notable narrow contracts:

- `POST /api/tasks/<id>/run` records an operator directive only. It never
  launches a runner; the resident daemon consumes it. See [[orchestration]].
- `POST /api/runs/<id>/interrupt` reports `ok` only when the canonical lease is
  `interrupted` **and** no process group is alive.
- `POST /api/executions/<id>/cancel` returns the store's control availability
  and never claims provider acknowledgement. See [[execution-observability-system]].
- Binding responses restate the proof boundary: observations recorded before
  the new binding generation stay observable but proof-ineligible.

## Docgraph CAS editing

`PUT /api/docgraph/doc?subject=<subject>` takes `{base_rev, header?, body?}`.
The server recomputes `sha256` of the file on disk and rejects a mismatch with
`409 DOC_SAVE_CONFLICT` plus `current_rev`. An untouched header or body is
preserved byte-for-byte. The edited document is re-parsed and the whole corpus
re-validated; only defects that are *new* relative to the pre-edit corpus block
the save (`422`). The write is atomic (temp file + rename), then the corpus is
reloaded and the fresh detail returned, with a warning when the docs map is
stale. See [[knowledge-and-feedback]].

## Delivery plans

`serveDeliveryPlans` lists `docs/plans/**.{yaml,yml}` under the project repo and
keeps only `tusker.delivery-plan/v2` schema files (unparsable ones surface as
`state:"invalid"` with an issue). Reads are capped at 1 MiB.

Review and start open the plan through `serveDeliveryPlanSnapshotAt`
(`serve_delivery_snapshot_unix.go`): every path component is opened relative to
the repo root descriptor with `O_NOFOLLOW`, dev/ino/mode are recorded, and the
bytes are read once from that descriptor. `Verify()` re-checks component
identity, size, mtime, and content digest. `POST /api/delivery/start` requires
the exact `planIdentity` returned by review and refuses with `409`-class errors
if anything moved. This file is Unix-only; on other platforms delivery
review/start refuse outright (`serve_delivery_snapshot_portable.go`).

## Needs and human action

`serveNeeds` (`serve_needs.go`) is computed during snapshot build. It emits one
need per open **human-owned** gate blocking a task (`owner` prefixed `human`,
deduplicated by gate ID), a `review` need for tasks that bounced to rework twice
or more, and a `failed` need per terminally failed run (retry budget exhausted
or parked, and not acknowledged/retired). Sorting is by blocking count then
priority. Tasks in `rework` are excluded from gate needs.

`serveHumanActionForTask` (`serve_human_action.go`) projects the first open
human gate in gate-ID order into a single next action with a covered-acceptance
checklist; a blocking gate without explicit `covers` covers the task's whole
acceptance contract. There is no second persisted human-action record.

## SPA

Source lives in `internal/serve/ui`; the production build is committed to
`internal/serve/ui/dist` and compiled into the binary via `//go:embed all:ui/dist`
(`internal/serve/assets.go`). Rebuilding the UI is therefore part of any Go
build that must ship UI changes.

- Toolchain: Bun (`packageManager: bun@1.3.14`, `bun.lock`, no npm), Vite 8,
  React 19, strict TypeScript, TanStack Router/Query/Table/Virtual, Tailwind v4
  via `@tailwindcss/vite`, TipTap for the knowledge editor, mermaid for diagrams.
- `make ui-install | ui-test | ui-build | ui-check` wrap
  `bun install --frozen-lockfile`, `bun run typecheck && bun test`, and
  `bun run build`. Dev server is `bun run dev` on 5173, proxying `/api` to
  `127.0.0.1:7420`. `vite.config.ts` drops sourcemaps and splits vendors into
  `react`, `tanstack`, `editor`, and `mermaid` chunks.
- `src/lib/api.ts` is the single transport seam: it bootstraps and caches
  `/api/capability`, attaches `X-Tusker-Capability` to every mutation, and
  retries once after clearing the cache on a capability-specific `403`.
  `USE_MOCK` is a UI-only fixture switch and is `false`.
- Layout: `src/routes` + `src/router.tsx`, `src/features/<screen>`,
  `src/components`, `src/lib`, `src/types/domain.ts` (the API contract). Bun
  tests live under `test/` and `tests/`.
- `internal/serve/ui/BACKEND-GAPS.md` tracks surfaces without a durable Serve
  contract; do not describe those as persisted until an endpoint and a readback
  test exist. `GET /api/capabilities` is the authority at runtime.

The macOS menu-bar shell that hosts this same SPA lives in `apps/mac/TuskerBar`
and is out of scope here; see [[platform-support]].
