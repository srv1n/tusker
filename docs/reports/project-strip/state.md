# Compact project navigation: state proof

## Result

PASS. The navigation state helpers cover migration, pin order, mount-time
recency, same-project route stability, checkout aliases, malformed storage and
unsafe route fallback.

## Command

```text
cd internal/serve/ui
bun test test/project-strip-state.test.ts test/project-strip-shell.test.ts test/wux-navigation.test.ts
```

Observed result: **9 tests passed, 81 expect calls**.

The separate TypeScript check also passed:

```text
bun run typecheck
```

## Covered behavior

| Area | Evidence |
|---|---|
| Migration | Old records retain last routes and opaque per-project view state. |
| Ordering | Pins lead; unpinned recency applies on the next mount; mounted order does not reshuffle on ordinary clicks or same-project routes. |
| Restoration | A valid deep link wins; project routes restore per-project locations; checkout aliases remain owned by their project. |
| Safety | Malformed storage, external paths, cross-project paths and missing projects use a safe Work/first-project fallback. |
| Shell contract | The source guardrails require one strip, icon-only global controls, scoped local links, native overflow CSS and preserved secondary actions. |

This is focused source/state proof, not installed-app or live-API proof.
