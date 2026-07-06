# Tusker Serve UI — Foundation & authoring contract

The shell, routing, theming, data layer, and shared component library are built.
Screen authors compose **against this contract** — do not re-invent primitives or
edit shared files.

## Stack

- **Bun** (no npm) · **Vite 8** · **React 19** · **TypeScript 6** (strict, `verbatimModuleSyntax`)
- **TanStack Router** (code-based) · **TanStack Query** (data) · **TanStack Table** / **Virtual** (grids, log tails)
- **Tailwind v4** (CSS-first `@theme`, tokens in `src/styles/app.css`)
- Self-hosted fonts (Fontsource), `lucide-react` icons, `react-markdown` + `remark-gfm`

## Directory map

```
src/
  main.tsx                  entry (providers) — DO NOT EDIT
  router.tsx                route tree — DO NOT EDIT
  routes/__root.tsx         shell — DO NOT EDIT
  components/Sidebar.tsx    nav — DO NOT EDIT
  components/ui/*           SHARED primitives (read-only for screen authors)
  lib/{cn,api,queries,theme,time}.ts   SHARED (read-only)
  types/domain.ts           SHARED domain types (read-only)
  mock/fixtures.ts          SHARED mock data (read-only)
  features/<screen>/**      ← YOU OWN your screen's folder only
```

## HARD RULES for screen authors

1. **Own only your feature folder.** Replace the given component file; add any
   sub-components / screen-local mock inside the same `features/<screen>/` folder.
   **Never edit** `src/mock/fixtures.ts`, `src/lib/*`, `src/types/*`,
   `src/components/ui/*`, `src/components/Sidebar.tsx`, `router.tsx`, `main.tsx`,
   `routes/__root.tsx`, `package.json`, or another screen's folder.
2. **Colors come only from tokens** (utilities below). Never hardcode a hex.
   Tokens auto-flip light↔dark, so using them means dark mode "just works".
3. **Use the shared component library.** If you truly need a primitive it lacks,
   build it *locally* in your folder — don't touch `components/ui/`.
4. **Mock-local.** Read existing data via the hooks. If your screen needs data
   the fixtures don't have, create `features/<screen>/mock.ts` with typed data
   (import types from `@/types/domain`). Mark anything the real API must supply
   with `// TODO(api): …`.
5. **Compile-only gate.** Make *your* files typecheck (`import type` for types).
   Do **not** run the full build or dev server — the main thread runs the
   central gate. Keep imports used (strict `noUnusedLocals`).
6. **Cover the states** the design shows: loading (`<SkeletonRows/>` / `<Skeleton/>`),
   empty (`<EmptyState/>`), error (`<QueryBoundary/>` handles it), and live data.
7. **Fidelity is the goal.** Match the handed design's layout, spacing, type
   scale, and interaction. The design slice is the source of truth for *look*;
   this contract is the source of truth for *how to build it*.

## Design tokens → Tailwind utilities

Surfaces/ink: `bg-surface` `bg-panel` `bg-raised` · `text-ink` `text-ink-soft`
`text-muted` `text-faint` `text-fainter`
Lines/fills: `border-line` `border-line-soft` · `bg-hover` `bg-active`
Semantic (each also has `-soft` bg): `text-fail`/`bg-fail-soft`, `text-pass`/`bg-pass-soft`,
`text-warn`/`bg-warn-soft`, `text-info`/`bg-info-soft`, `text-accent`/`bg-accent-soft`
Type: `font-sans` (body) · `font-serif` (headings/brand) · `font-mono` (ids, commands, counts, logs)
Motion: `animate-rise` (enter), `animate-pulse-soft` (liveness). Scrollbars: add class `tk-scroll` to scroll containers.
Numbers in mono: add `tabular`.

## Shared component API (import from these paths)

`@/components/ui/primitives`
- `Mono({className})` — inline mono span
- `Dot({tone?, pulse?, size?})` — status dot
- `Chip({tone?, variant?: 'soft'|'outline'|'solid', mono?, children})`
- `CountBadge({count, tone?})` · `Kbd({children})` · `Card({interactive?, ...div})`

`@/components/ui/chips`
- `StatusChip({status})` `RiskChip({risk})` `PriorityChip({priority})`
  `ReadinessChip({readiness})` `GateKindChip({kind})` `OutcomeChip({outcome})`
  `ProofChip({proof})` `RunnerBadge({runner})`

`@/components/ui/capsule`
- `CapsuleChips({capsule, show?})` — status·priority·risk·readiness set
- `TaskRef({id, projectId})` — mono id link · `BlockingBadge({count})`

`@/components/ui/liveness`
- `LivenessIndicator({liveness, sinceSec, showLabel?})` · `LivenessDot({liveness})`

`@/components/ui/states`
- `Skeleton({className})` `SkeletonRows({rows?})`
- `EmptyState({icon?, title, hint?, action?})`
- `ErrorState({error, onRetry?})`
- `QueryBoundary({q, loading?, children:(data)=>node})` — pass a query result

`@/components/ui/page`
- `PageHeader({title, subtitle?, actions?, eyebrow?})`
- `SectionLabel({children})` — uppercase mono caps
- `PageScroll({children})` — scrolling body, max-w 1180, tk-scroll
- `Toolbar({children})`

`@/components/ui/controls`
- `Button({variant?: 'default'|'primary'|'ghost'|'danger'|'subtle', size?})`
- `IconButton({active?})` · `SegmentedControl({options, value, onChange, size?})`
- `Toggle({checked, onChange, label?})` · `TextInput` · `Select`

`@/components/ui/tone` — `tone`, and semantic→tone/label maps
(`gateKindTone/Label`, `statusTone/Label`, `outcomeTone/Label`, `readinessTone/Label`,
`riskTone`, `priorityTone`, `livenessTone`, `proofTone`) if you need raw classes.

## Data hooks (`@/lib/queries`)

`useDaemon()` `useProjects()` `useNeeds(projectId?)` `useRuns(projectId?)`
`useRun(taskId)` `useEpics(projectId?)` `useTasks(projectId?)` `useTask(id)`
`useDocList(projectId?)` `useDoc(path)` — each returns a TanStack Query result
(`{data, isLoading, isError, error, refetch}`). Wrap with `<QueryBoundary>`.

Types: `@/types/domain` (`NeedItem`, `RunSummary`, `RunDetail`, `TaskCapsule`,
`TaskDetail`, `EpicSummary`, `DocContent`, `ProjectSummary`, etc.).

## Route params / search

Use `getRouteApi` (no import cycle):
```ts
import { getRouteApi } from "@tanstack/react-router";
const route = getRouteApi("/p/$projectId/needs");
const { projectId } = route.useParams();
// docs route also: const { path } = route.useSearch();  // '/p/$projectId/docs'
```
Route ids: `/`, `/settings`, `/p/$projectId/` (overview), `/p/$projectId/needs`,
`/p/$projectId/runs`, `/p/$projectId/runs/$taskId`, `/p/$projectId/work`,
`/p/$projectId/docs` (search `{path?}`), `/p/$projectId/settings`.
Navigate with `<Link to="…" params={{…}}>` or `useNavigate()`.

## Utils

`cn(...classes)` from `@/lib/cn`. Time: `relativeTime(iso)`, `duration(sec)`,
`sinceLabel(sec)`, `compactNumber(n)` from `@/lib/time`.

## Design DSL → React translation

The design slices are a template DSL prototype:
- `{{ expr }}` → a value binding → real data / state
- `sc-if value="{{ x }}"` → `{cond && <…/>}`
- `sc-for list="{{ xs }}" as="p"` → `{xs.map((p) => <…/>)}`
- `style-hover="background:#F0F0F0"` → `hover:bg-hover`
- inline `style="…"` hexes → the nearest **token utility** (never copy the hex)
