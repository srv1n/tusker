# S46 qualification (TSK-T-0062) — 2026-09-23

Scope: fresh, migrated, and tool-independent documentation journeys across
docs, CLI, packet consumer, docgraph API, and Documents UI. Upstream
`TSK-T-0061` (adoption) is `backlog`/`held` and its receipt
(`docs/reports/s46/adoption.md`) records a blocked apply with no migration
applied; this qualification therefore proves the journeys on isolated fixture
corpora plus a real-repo map regeneration, and does not claim an
adopted-Tusker-corpus proof. No tracker file was hand-edited; no unrelated
dirty file was reset, migrated, or incorporated.

## Verdicts

| ID | Outcome | Result |
| --- | --- | --- |
| A1 | Fresh and legacy journeys reach CLI, packet and API consumers with correct identities and preserved authority. | PASS |
| A2 | Docs remain navigable/renderable after removing .tusker from a disposable copy. | PASS |
| A3 | Integrated UI journey and build accept the new contract. | PARTIAL (contract journey + build PASS; live-browser run NOT RUN, sandbox-blocked) |
| A4 | Actual desktop/narrow and keyboard observations, versions, screenshots and boundary-specific results are retained. | PARTIAL (contract-level observations + versions retained below; no screenshots captured — live browser NOT RUN) |

A blocked or NOT RUN boundary is not a PASS and is not counted as one.

## Verification

| Covers | Check | Result | Notes |
| --- | --- | --- | --- |
| A1 | `go test ./cmd/tusker -run TestS46PortableDocsJourney -count=1` | PASS (2/2 subtests) | New `cmd/tusker/s46_portable_docs_journey_test.go`: fresh init/create/link/task/packet/CLI/API-CAS; migrated preview/apply/forward/packet/CAS plus stale-input refusal and interrupted-apply resume. |
| A2 | `go test ./cmd/tusker -run TestS46PortableDocsWithoutTusker -count=1` | PASS | Same file: disposable copy without `.tusker`, 8+ relative link/asset/anchor resolutions, no runtime-path references, 5 pages rendered with the pinned goldmark renderer. |
| A3 | `bun test --cwd internal/serve/ui test/s46-portable-documents-journey.test.ts` | PASS (10/10, 40 expects) | New journey test: folder tree, ancestors, filter, Mermaid graph (no invented edges), lifecycle/conformance independence, editor CAS/409 contract, Escape keyboard, desktop/narrow drawer contracts. |
| A3 | `bun run --cwd internal/serve/ui build` (single delayed typecheck+build) | PASS | `tsc --noEmit` clean; vite build in ~1.36s. Rebuilt `internal/serve/ui/dist` is byte-identical to the committed dist (no tracked diff), so no concurrent dist owner was clobbered. |
| A4 | ledger (this file) | PARTIAL | See observations and boundaries below. |

Focused regression: `go test ./internal/docgraph` PASS; S46 + doc-save CLI
selection (`TestS46*`, `TestDocSave*`, `TestDocsList*`, migration/recovery/reset)
PASS; S46 UI selection (portable-documents, journey, docs-save,
knowledge-wikilink) 27/27 PASS; `gofmt` clean on the new Go test.

## Map regeneration (once)

- Fixture `/tmp/s46-qual` (project `s46-qual`, 5 docs, `docs check` valid):
  `tusker docs map` → `Docs map, index, and graph regenerated.`
- Real repo: `tusker docs map --vault ./.tusker --json` → `{"ok":true}`;
  wrote owned, gitignored `.tusker/_generated/docs/INDEX.md`
  (sha256 `c672218e…`) and `graph.json` (sha256 `f6357763…`).
- Side effect reverted: the same command also rewrote the legacy tracked
  `docs/system/INDEX.md` + `graph.json` (compatibility dual-write owned by
  the migration story); restored with `git checkout --` so the working tree
  keeps only pre-existing unrelated modifications.

## A4 observations (contract-level; live browser NOT RUN)

- Desktop/narrow/keyboard contracts verified against the actual components:
  explorer drawer closes on Escape (`KnowledgeShell.tsx`), drawer is
  focusable (`tabIndex={-1}`) with `max-w-[calc(100%-32px)]` at narrow width,
  desktop sidebar is `hidden ... lg:block` at 280px, tree exposes
  `Documents explorer` label with `aria-expanded`, Files/Graph tabs carry
  `aria-current`. Mermaid graph omits unknown edge endpoints instead of
  inventing relationships; the editor pins `base_rev` so a stale save
  surfaces a 409 conflict banner instead of overwriting.
- API behavior behind the UI verified through the real handlers in-process:
  detail read, body edit advancing the sha256 rev, stale `base_rev` → 409
  with the file untouched (fresh and migrated corpora).
- No screenshots captured: the live-browser run needs a localhost listener
  (`tusker serve` + browser), and this session's sandbox denies listening
  sockets (`bind: operation not permitted` on `127.0.0.1:7420`); unsandboxed
  execution approval is disabled, so no server could be started. The same
  block covers the repo's live-Chromium UI tests (they time out here).

## Versions

- go 1.26.5 darwin/arm64; bun 1.3.14; node v25.6.1; typescript 7.0.2;
  playwright module 1.47.1 (present, unused — no server to drive);
  chromium 140.0.7339.16 (present, unused); goldmark v1.7.17 (pinned module
  renderer used by the A2 test); fixture CLI built from this checkout.

## Unrelated failures and unrun boundaries (precise attribution)

- Full UI suite: 356 pass / 18 fail across 79 files. All 18 are outside S46:
  walkthrough-inspector/status/task-authoring, execution-settings, stream
  keys, settings toggles, agent-access approvals/profiles, agent
  coordination, and live-Chromium tests that need a listening server. No UI
  source file was changed by this task (one test file added, passing), so
  these failures are pre-existing on this checkout with its unrelated dirty
  files; they belong to their suite owners, not S46.
- Real-repo `tusker docs check` still reports the 2 known legacy lifecycle
  issues and `tusker validate` its pre-existing errors (see adoption
  receipt); they belong to the adoption/migration stories.
- NOT RUN: live-browser desktop/narrow/keyboard observation with screenshots
  (sandbox, above); adopted-Tusker-corpus end-to-end proof (blocked on
  `TSK-T-0061` apply); installed/native (mac app) boundaries.

## Defects returned

None: the fresh, migrated, and tool-independent journeys found no S46
implementation defect at the exercised boundaries. One compatibility note for
the migration owner (not a defect): `docs map` dual-writes the legacy
`docs/system/INDEX.md` + `graph.json` alongside the portable
`.tusker/_generated/docs/` outputs.
