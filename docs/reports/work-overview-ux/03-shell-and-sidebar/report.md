# Work shell and sidebar — rendered check

## After

- Desktop: `after/work-desktop-settled.png`
- Narrow: `after/work-narrow-settled.png`
- Source check: both views confirmed the Work header did not contain project `01KZR7B0W24SSJ2N0EWB05DNZA`.
- Desktop shows the long, mixed-state backend wave list, Waves/Board navigation, quiet live status, and a single collapsed `1 project need attention` sidebar indicator. The affected-project detail and Settings troubleshooting link are reachable from that disclosure.

## Checks

- `bun test test/real-work-events.test.ts test/navigation-simplification.test.ts tests/project-switching.test.ts` — PASS (21 tests).
- `bun run typecheck` — PASS.
- `git diff --check -- internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx internal/serve/ui/src/features/workbench/integration/StreamStatus.tsx internal/serve/ui/src/components/Sidebar.tsx` — PASS.
- Generic read-only browser journey against the already-running local Serve UI — WIN1/WIN2/A1 PASS, then blocked at the existing Wave dependency graph visibility wait for `W-0017`; no mutation was attempted.

## Gap

No before screenshot was available: this task began in an already-dirty shared checkout and the active Vite surface hot-reloaded the changed source. A synthetic reconstruction would not be evidence for the actual routed surface.
