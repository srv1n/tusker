---
kind: proposal
subject: workspace-modes-and-landing
title: "Workspace modes and landing: shared checkout or worktrees, merged as soon as done"
keywords: [shared, worktree, landing, merge, main, drift, re-approval, owned paths, daemon lifecycle, quit, governor]
part_of: overview
describes: [cmd/tusker/v7_land_cmd.go, cmd/tusker/direct_wave_authority.go, cmd/tusker/review_pass.go, apps/mac/TuskerBar]
status: accepted
code_conformance: unverified
created: 2026-10-19
last_verified:
read_when: "Changing how tasks get a workspace (shared checkout or worktree), how finished work reaches main, when a wave needs re-approval, or how the daemon and app start and stop."
skip_when: "Changing what a single run does once started; see process-containment."
updates: [direct-wave-authoring, event-driven-daemon, process-containment]
sources: []
decisions_locked: true
---

# Workspace modes and landing

## Why

On 2026-10-19 a four-task demo wave ran from dispatch to review to close with
no human step, but almost every manual step that day came from how finished
work reaches main: a wave branch that drifts from main, re-approval refused
while a task runs, review requests nobody receives, and a demo whose task
records were never committed. Large projects (Rust) also suffer from
worktrees: every task copies the repository and starts with a cold build
cache. The operator wants the smoothest process that does not break things.

## The two modes (a per-project setting)

**Shared mode** — for large projects where copying the repository is costly.
- Work happens in the project's own checkout on its default branch. No
  worktrees, no task branches, no wave branch.
- Each task commits its own owned files directly. Build caches stay warm.
- Two tasks may run at once only when their owned files do not overlap;
  otherwise they run one after another. The landing check (build and tests)
  runs one at a time.
- If review rejects work that is already committed, the fix is a new commit
  (rework), never a history rewrite.
- Main moving is normal here, so there is no drift check.

**Worktree mode** — for projects where tasks may touch overlapping code.
- Each task works in its own worktree and branch.
- **Each task merges into main as soon as it passes review and its landing
  check.** There is no wave branch that accumulates work. Merging early keeps
  every task close to main, so conflicts stay small and are handled at that
  task's merge, and a wave can never drift far.
- If main moved since the task started, the landing rebases or merges the
  task onto current main and reruns its checks; only a real conflict goes
  back to the task as rework.

In both modes a **wave is an approval and grouping unit**, not a branch: it
says which tasks may run and in what order.

## Decisions (locked 2026-10-19)

| # | Decision |
|---|---|
| M1 | Workspace mode is per project: `shared` or `worktree`. Shared truly means no worktrees and no branches. |
| M2 | Shared mode runs tasks in parallel only with disjoint owned files; landing checks are serialized. |
| M3 | Rejected committed work is fixed forward with a new commit. |
| M4 | Worktree mode merges each task to main as soon as it passes; no wave integration branch. |
| M5 | Re-approval is needed only when what a task promises (its acceptance, scope or checks) changes, and it applies to the next attempt instead of being refused while one runs. Commits to main never require re-approval. |
| M6 | When a reviewer finds the work correct but touching a file the task does not own, Tusker widens the task's owned files itself, unless that file is owned by another active task or is on a do-not-touch list; then it goes to the operator's attention queue. |
| M7 | Demo projects commit their task records like real projects, so every check runs the real path. A docs-only task's proof requirement accepts a command check, not only a focused test. |
| M8 | The daemon runs on its own (login item), independent of the app window. Closing the window keeps work going. |
| M9 | "Quit Tusker" (menu) and `tusker stop --all` stop the daemon and every process Tusker started, cleanly, using the process inventory. A force-killed app or daemon is cleaned up at next start by the janitor. |

## Deferred, needs its own spec session

**Governor.** A Tusker-owned agent profile that handles discrepancies on the
operator's behalf using written procedures (parked runs, review caps, owned
file requests outside M6's safe rule, stuck landings, conflicting edits) and
only escalates what the procedures do not cover. It extends the deferred run
triage layer. Rules first; the model judges only what rules cannot decide;
safety stops never depend on a model.
