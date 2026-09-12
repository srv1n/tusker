---
title: "Project registration and visibility"
subject: project-registration-and-visibility
keywords: [project discovery, registration, visibility, all projects, tusker init]
part_of: overview
describes: [cmd/tusker/runtime_store.go, cmd/tusker/serve_actions.go, cmd/tusker/install.go, internal/serve/ui/src/features/settings/AppSettings.tsx]
status: canonical
created: 2026-09-11
last_verified: 2026-09-11
read_when: "Changing project discovery, main-screen visibility, or the All Projects settings surface."
skip_when: "Changing automation policy, checkout grouping, or project-local settings."
sources: ["[[decisions/2026-09-11-project-registration-and-visibility-grill]]", "[[work-area-redesign]]"]
decisions_locked: true
---

# Project registration and visibility

## Why

Running `tusker init` means the operator intentionally brought that folder into Tusker. Tusker should remember it automatically, but registration must not force every remembered project onto the main screen forever.

## Product contract

- `tusker init` registers the initialized project in the machine registry by default.
- A newly initialized project is visible on the main screen by default.
- Global Settings has an **All Projects** tab listing every registered project, including hidden or unhealthy projects.
- Each row has a check box controlling whether that project appears in the main project strip.
- Visibility changes navigation only. They never enable or disable automation, start work, remove history, or unregister a project.
- A missing or unreadable project remains listed with its error, is skipped by background reconciliation, and cannot terminate the daemon.
- Related checkouts grouped as one logical project share one visibility choice.
- Repository grouping identity is captured when a checkout is registered and survives a missing or deleted worktree; a path failure must not turn that checkout into another project.
- The project strip shows one item per logical repository. When that repository has multiple registered checkouts, the project context exposes a checkout switcher instead of adding more project items.
- Existing registrations migrate as visible so an upgrade does not silently erase the operator's current navigation.
- `tusker init --no-register` is the explicit opt-out for vault-only use.

## Program contract

The shared `projects` table owns a `visible` boolean independent from its existing automation `enabled` boolean. `GET /api/projects` exposes logical-project visibility. `POST /api/projects/<id>/visibility` accepts a required boolean and updates every registered checkout in that logical group.

The web shell applies visibility at its existing navigation filter. Settings reads the complete project response without applying that filter, so hidden projects remain recoverable.

## Acceptance

| ID | Observable result | Verification |
| --- | --- | --- |
| PV1 | A registered project can be hidden and remains registered with automation unchanged. | `go test ./cmd/tusker -run TestProjectVisibilityPersistsWithoutChangingAutomation -count=1` |
| PV2 | Hidden projects are absent from the project strip but present in All Projects; linked worktrees remain one project with an in-context checkout switcher even after one disappears. | `go test ./cmd/tusker -run 'TestProjectGroupingSurvivesDeletedWorktree|TestProjectRegistrationKeepsLinkedWorktreesAsDistinctCheckouts' -count=1 && cd internal/serve/ui && bun test test/project-strip-state.test.ts test/project-strip-shell.test.ts && bun run typecheck && bun run build` |
| PV3 | An unreadable registered project cannot terminate targeted daemon reconciliation. | `go test ./cmd/tusker -run TestArchitectWaveReportsSkipBrokenProject -count=1` |
| PV4 | Runtime recovery preserves a committed Mac window or panel. | `swift test --package-path apps/mac/TuskerBar --filter TuskerBarTests.testRuntimeShellLoadsStoredUIOptimisticallyAndNeverCoversCommittedContent` |

## Deferred (not now)

- Filesystem-wide scanning outside folders initialized or explicitly added by the operator.
- Cloud synchronization of project visibility.
- Search, bulk selection, or tags in All Projects; add them only when the list size makes the plain native list measurably inadequate.

<!-- tusker:delivery-import:0888ca4bbb0c516c:begin -->

## Work streams

- `[[WUX-T-0019]]` implements delivery source `task-WUX-T-0019`.

- `[[W-0019]]` is the imported delivery wave.

<!-- tusker:delivery-import:0888ca4bbb0c516c:end -->
