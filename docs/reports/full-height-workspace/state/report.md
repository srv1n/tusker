# WUX-T-0020 state report

## Verification

- Setup: existing dirty Tusker checkout; scoped model tests only. No daemon,
  browser fixture, build, install, deployment or agent was started.
- Command: `cd internal/serve/ui && bun test test/workspace-state.test.ts test/project-strip-state.test.ts test/wux-navigation.test.ts`
- Observed: PASS — 10 tests, 79 assertions, 0 failures.
- Additional checks: `cd internal/serve/ui && bun run typecheck` PASS;
  `git diff --check -- internal/serve/ui/src/features/workbench/navigation/navigationState.ts internal/serve/ui/src/features/workbench/navigation/index.ts internal/serve/ui/test/workspace-state.test.ts` PASS.

## Acceptance

| ID | Result | Evidence |
| --- | --- | --- |
| A1 | PASS | `workspace-state.test.ts`: legacy opaque state, checkout-keyed state, malformed JSON, blocked storage and absent/false pane preference cases. |
| A2 | PASS | `workspace-state.test.ts`: checkout isolation, partial section merges, same-checkout Docs path validation, finite/bounded value validation, and last-20 document/wave visit pruning. |
| A3 | PASS | `workspace-state.test.ts`: absent versus `false` context preference and sibling/opaque state preservation; helpers are exported from the navigation barrel. |

## Limits

This is local model evidence only. Browser layout, scroll restoration in mounted
components, live service behavior and installed-app behavior remain unverified
and belong to the later integration/qualification tickets.
