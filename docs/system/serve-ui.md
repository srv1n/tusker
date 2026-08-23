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

- Today: `/` and `/p/<project>/`
- Tasks: `/p/<project>/tasks`
- Epics and waves: `/p/<project>/epics` and `/p/<project>/waves`
- Operations: `/p/<project>/diagnostics`
- Execution Operations: `/p/<project>/diagnostics/executions`
- Task and tracker documents: `/p/<project>/docs`
- System knowledge: `/p/<project>/knowledge`
- Project settings: `/p/<project>/settings`

The TypeScript source is the web source authority. `internal/serve/ui/dist/` is
a generated embed. Rebuild it after a web source change.

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
