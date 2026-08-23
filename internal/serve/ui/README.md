# Serve web app

This directory contains the web app that `tusker serve` returns. The Go binary
embeds `dist/` with `go:embed`.

## Runtime

The app reads the local Serve API. It uses mock data only when
`VITE_USE_MOCK=1` is set. The API authority is
`cmd/tusker/serve_command.go` and the `cmd/tusker/serve_*.go` handlers.

The production service uses the local address `127.0.0.1:7420` by default.
The browser client listens to `/api/stream` and invalidates its query cache when
the service reports a change.

## Commands

Run these commands in this directory:

```sh
bun install --frozen-lockfile
bun run typecheck
bun test
bun run build
```

Use `bun run dev` for the Vite development server. Set `VITE_USE_MOCK=1` only
when you need the fixture data.

## Source layout

| Path | Purpose |
| --- | --- |
| `src/router.tsx` | Current route tree. |
| `src/features/product/` | Today, task, delivery, and operations screens. |
| `src/features/docs/` | Tracker document reader and task contract view. |
| `src/features/knowledge/` | System document list, graph, and reader. |
| `src/features/executions/` | Execution graph and actions. |
| `src/lib/api.ts` | HTTP client and response types. |
| `src/lib/queries.ts` | Query keys and mutations. |
| `src/mock/` | Opt-in development fixtures. |
| `dist/` | Generated embedded files. Do not edit by hand. |

## Main routes

- `/` and `/p/<project>/` show current work.
- `/p/<project>/tasks` shows tasks.
- `/p/<project>/epics` and `/p/<project>/waves` show delivery groups.
- `/p/<project>/diagnostics` shows runtime health.
- `/p/<project>/diagnostics/executions` shows the execution graph.
- `/p/<project>/docs` reads tracker documents.
- `/p/<project>/knowledge` reads the system document graph.
- `/p/<project>/settings` changes project settings through guarded API calls.

See `docs/system/serve-ui.md` for the system contract.
