# Full-height workspace shell audit

Date: 2026-09-11

## Result

The visible desktop shell now uses one coherent left-side navigation system:

- Collapsed mode: 48px project rail and 56px section rail, icons only.
- Expanded mode: 256px project rail and 176px section rail, full project names and labels.
- Waves, Board, and Docs use identical 48px targets and the same icon/label alignment.
- Settings and secondary destinations use the same target geometry in the section rail footer.
- The mode is persisted under `tusker.sidebar.layout.v1` and restored on reload.
- The former horizontal project strip and project-scoped top tab row are gone.
- The root shell fills the viewport without the previous outer top gutter or rounded shell card.

## Verification

| Check | Result | Evidence |
| --- | --- | --- |
| TypeScript | BLOCKED outside shell | `bun run typecheck` reaches unrelated dirty `src/features/settings/app/ProfilesSection.tsx`: unused `accessFromPreset` and `presetMode`, plus missing `legacyAccessLabel` |
| Vite bundle | PASS | `cd internal/serve/ui && bunx vite build` |
| Focused navigation/state tests | PASS | 13 tests, 117 assertions |
| Browser shell render | PASS | 13 fixture projects; collapsed 48px/56px; expanded 256px/176px; all section targets 48px; persisted expanded state; no page errors |
| Visual capture | PASS | [/tmp/tusker-sidebar-collapsed.png](/tmp/tusker-sidebar-collapsed.png), [/tmp/tusker-sidebar-expanded.png](/tmp/tusker-sidebar-expanded.png) |

## Scope boundary

This closes the visible desktop shell correction. The remaining full-height packets are not implicitly complete: the shared `WorkspaceSurface` adoption, Docs/Waves/Board context panes, responsive breakpoints, and final qualification still require their own implementation and proof. WUX-T-0020 remains in review and WUX-T-0021 remains tracker-blocked on that dependency.
