# tusker serve — control-room UI

The embedded single-page control room served by the `tusker` binary on
`localhost:7420`. Answers one question: **what needs me, and what is the machine
doing?** Attention-routed (needs-me first), multi-project.

## Stack

Bun (no npm) · Vite 8 · React 19 · TypeScript 6 (strict) · TanStack Router /
Query / Table / Virtual · Tailwind v4 (CSS-first tokens) · self-hosted fonts
(Source Serif 4 + Source Code Pro via Fontsource) · light + dark themes.

## Develop

```bash
bun install
bun run dev        # http://localhost:5173 (runs against the in-browser mock)
bun run typecheck
bun run build      # → dist/ (embedded by the Go serve package via go:embed)
```

There is **no npm**; use Bun for everything.

## Layout

- `src/routes`, `src/router.tsx` — app shell + TanStack (code-based) routes
- `src/features/<screen>` — one folder per screen
- `src/components/ui` — shared primitives (chips, states, controls, liveness…)
- `src/lib` — `api` (backend seam), `queries` (TanStack Query), `theme`, `time`, `cn`
- `src/types/domain.ts` — the domain model (the API contract)
- `src/mock/fixtures.ts` — in-browser mock dataset

## Status

Front-end-first. Data comes from a typed mock; the daemon JSON API is being built
in parallel. See **`BACKEND-GAPS.md`** for the wiring checklist and
`FOUNDATION.md` for the architecture + component contract.

Design source of truth: the handed "Tusker Serve" design
(claude.ai/design project `77b1bb97-2b9a-4919-89ce-eb3c10a10466`).
