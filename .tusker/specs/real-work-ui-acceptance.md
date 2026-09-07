---
subject: real-work-ui-acceptance
title: "Verify the live task wave and Documents experience against the seeded project"
keywords: [repeatable testing, CLI parity, real execution, handoff]
part_of: real-work-test-packets
status: canonical
created: 2026-09-07
read_when: "Implementing or assigning verify the live task wave and documents experience against the seeded project."
skip_when: "Looking up already verified installed behavior; this is an implementation contract."
sources: [repeatable-work-testing.md, work-knowledge-and-retention.md]
decisions_locked: false
capsule:
  what: "Self-contained work packet with scope, ownership, acceptance and verification."
  use_when: "Assigning this bounded work to an implementation agent."
  skip_when: "Orchestrating unrelated tasks or redesigning the product."
---

# Verify the live task, wave and Documents experience against the seeded project

## Assignment

You own the real UI integration and its repeatable browser acceptance. Preserve the agreed design; fix concrete integration defects. Suggested work level: Standard. Do not take over task lifecycle or the fixture implementation.

## User context

The operator wants a resettable test project populated from the CLI. An agent first tests its real execution path without computer access. The operator then resets it and clicks normal Run controls to watch a task, then waves, execute. The UI must show the same work the CLI reports, with no page reload required as dependencies complete.

A sibling fixture worker supplies fictional documents, one standalone task and three four-task waves. Alpha and Beta are independently startable, each has a branch/join graph, and Follow-up depends on both. Jobs do actual small file/test changes and emit bounded progress for roughly sixty seconds using a supplied helper. Real Codex Luna is the first run; mixed Codex/Muse follows when configured.

The agreed Work UI is already implemented: persistent projects, Waves/Board, full DAG, temporary inspector and completed results. Documents keeps its real folder/file tree, readable content, compact metadata and optional Graph. This packet proves those surfaces together and fixes defects; it is not another redesign.

## Current evidence and gaps

Existing integration report `docs/reports/wux/integration/report.md` records focused tests/build and sample previews, but its live traversal was blocked by project registration. Those fixture previews are not final live proof.

Documents changes and reports live in `docs/reports/documents/ui/`. A runnable `internal/serve/ui/test/documents-polish.browser.mjs` covers real UI against isolated API responses, including Mermaid and metadata/conflict interaction. That establishes UI response behavior, not a live filesystem CAS collision. Preserve these tests and their honest scope.

Known reviewed fixes include focus containment/restoration, stopping restoration after the user chooses another control, and settling normalized metadata without losing newer edits. Do not regress those while integrating.

## Scope: individual task first

Against the real seeded project, verify the normal task Run action uses the same backend operation and authority as its CLI counterpart. Before start show truthful readiness and actual blockers. During execution show current stage, progress freshness, actual profile/model/transport when available and failure/cancellation state. Do not display a previous worker as the current reviewer.

Open the task inspector, read intent/acceptance, follow its source spec and inspect produced evidence. After accepted review/completion, the UI and CLI must agree on the final state. A successful process exit alone must not paint the task complete. Evidence attachments alone must not create a mandatory human approval step.

## Scope: waves and SSE

Use normal wave Run controls, not a hidden demo route. Alpha and Beta can each be started deliberately. The DAG shows nodes/edges from real dependencies at readable size, including the parallel branches and joins. Actual event-driven transitions must update without reload. Preserve selected task, pan and zoom when node state changes; do not yank an open flow view into results mid-interaction. Opening an already completed wave leads with its result.

Reuse the existing SSE/event subscription and query cache. Establish one shared subscription pattern, not a connection for every node. Relevant task/run/wave events must invalidate or update the correct project-scoped data. On reconnect, obtain fresh authoritative state or replay from the supported cursor; do not assume no events were missed. Out-of-order or duplicated events must not regress newer state or duplicate nodes. Show disconnected/stale state honestly.

Keep the existing accessible fallback/list representation, focus visibility and narrow-screen inspector. Handle missing/unavailable dependency details without inventing graph facts.

## Scope: Documents journey

Preserve the file/folder browser. Verify the fictional folder introductions and direct-file listings help a human navigate to the exact note. Preserve expansion, filtering, selected/deep-linked note, long-name access and keyboard operation. The document's title and content should lead; routine front matter is available in Details. Render Mermaid safely with source/error fallback. Internal links, backlinks and superseded-to-current navigation work.

Check the corresponding CLI discovery contract supplied by existing Documents helpers:

```sh
tusker docs browse <managed-directory> --limit 20 --json
tusker docs read <subject-or-path> --json
tusker docs read <subject-or-path> --section '<exact heading>' --json
tusker docs backlinks <subject-or-path> --limit 20 --json
tusker docs check --json
```

Use the seeded repo's scope/candidate according to actual help, not the current project's defaults. Folder entries must expose a useful introduction or explicitly report it absent; notes expose parsed read/skip guidance and identity without forcing full-body reads. Invalid metadata/broken links must not return a successful validation exit. Report backend/CLI gaps to their owner; do not fork the documentation resolver in frontend code.

A human-readable folder table of contents should derive from the actual child entries; do not require another manually maintained duplicate index. Respect existing authored folder introductions and link to them where appropriate.

## Interaction acceptance matrix

| ID | Scenario | Observable result |
|---|---|---|
| A1 | Fresh project → standalone task → Run | Correct task starts through normal action, progress is visible, accepted completion matches CLI |
| A2 | Start Alpha, then Beta | Both show actual running work; joins remain waiting until prerequisites meet native rules |
| A3 | Stay on DAG during events | Nodes/stages refresh without reload; selection/viewport remain stable; model labels remain truthful |
| A4 | Disconnect/reconnect SSE | Stale state is visible; reconnect catches up; no duplicate nodes or regression to older state |
| A5 | Cancel/fail/retry | Correct terminal/intermediate state; unrelated wave continues; retry is a distinct truthful attempt |
| A6 | Completed wave entry | Results appear first, evidence is usable, Flow remains one action away |
| A7 | Documents path/metadata/rationale | Folder introduction → note → exact content → linked decision works through UI and corresponding CLI |
| A8 | Keyboard/narrow screens | Run/inspector/tree controls remain operable, mobile focus does not escape or get stolen, long names remain accessible |
| A9 | Cross-surface consistency | Matched CLI snapshots and UI state agree on project/task/wave/attempt identities and completion |
| A10 | Reset/repeat | No stale prior-project query/selection contaminates fresh scenario; human runbook reproduces the journey |

## Ownership and implementation route

Start in `internal/serve/ui/src/features/workbench/integration/WorkExperience.tsx`, relevant Work components and existing event subscription/query hooks. Find the existing SSE consumer before adding code. `internal/serve/ui/src/lib/queries.ts` is a shared entry point; coordinate narrowly with model/settings changes. `features/knowledge/` owns Documents UI; reuse shared editor/Mermaid primitives without rewriting them.

Own bounded Work/Documents UI corrections, event consumption, focused browser tests and `docs/reports/real-work/ui/`. Do not edit `cmd/tusker/demo_*.go`, runtime lifecycle, CLI command dispatch or docgraph parsing. If an event/projection/backend capability is missing, provide a precise request/expected response to the lifecycle/CLI owner. Do not fake it client-side. Coordinate type/API additions before touching shared frontend types.

No global style overhaul, new graph library, mock production dataset, theme framework, new editor or full application router rewrite. Preserve sibling dirty changes and current user project data.

## Runnable verification

Add `internal/serve/ui/test/real-work-events.test.ts` for deterministic event/revision/reconnect behavior, using existing test conventions. Tests must exercise behavior, not search source strings. Command:

```sh
cd internal/serve/ui
bun test test/real-work-events.test.ts
bun run typecheck
```

Add a runnable headless journey at `internal/serve/ui/test/real-work.browser.mjs` (reuse current Playwright setup). Accept explicit base URL/project ID, fail on unmet fixture readiness, and print named assertions. It must run against the seeded ordinary app for final acceptance. Deterministic mocked-event tests are supporting proof and must be labeled separately.

Capture the actual rendered UI at 1440×1000, 1024×768 and 390×844: standalone running/completed, concurrent waves, live DAG, result reader and Documents. For each visual iteration use a fresh critic receiving only the screenshot and agreed rubric; record specific gaps and score, not invented human approval. Do not use desktop input automation while the operator uses the machine. A headless browser is the default.

A local test server may be used on an explicitly free port without replacing the resident app/daemon. Final proof must identify which candidate/backend supplied data. Never point a screenshot test at an old server and claim it verifies new code.

## Deliverables and completion boundary

`docs/reports/real-work/ui/report.md` records candidate, backend identity, project/work IDs, commands, browser/viewport, acceptance matrix, screenshots, critique and first remaining failure. Include `manual-walkthrough.md`: exact reset/seed command from fixture owner, URL/project, individual task to click, expected running/review/completion states, wave steps and expected DAG transitions. The operator should not author any tickets to follow it.

No backend screenshots are substitutes for actual CLI proof; no passing mocked UI test substitutes for the real seeded journey. Report installed-runtime/provider prerequisites explicitly. Human aesthetic acceptance remains for the operator after agent checks, without making every normal task require human review.

## Dependencies

You may inspect/fix existing UI behavior and build tests immediately. Final live acceptance requires the lifecycle owner's consistent start/review/close path and the fixture owner's registered real-harness repository. Use the same candidate and scenario IDs as those reports. Do not repeatedly coordinate implementation work on behalf of the design agent; deliver a concrete report the operator can review.
