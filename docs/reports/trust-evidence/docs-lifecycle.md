# Documentation lifecycle evidence

Status: source implementation and canonical spec move are complete; the
named focused regression passes were observed in the shared checkout before the
final resolver adjustments. The required rerun is held for root's compile
window. This file records executed checks only. It does not claim FLW-T-0027
passed its tracker transition.

## Ownership chain

The current route is explicit and has one owner for each meaning:

1. `skills/tusker/` owns the operator skill source;
2. `.tusker/SKILL.md` and the project canon own repository facts;
3. `docs/system/` owns current behavior and runbook explanations;
4. `.tusker/specs/` owns governing product contracts;
5. `.tusker/specs/decisions/` owns durable decisions; and
6. task records and `docs/reports/trust-evidence/` own execution/proof facts.

This work leaves `skills/tusker`, `skills/spec`, `cmd/tusker/install.go`, and
`docs/system/proof-and-closeout.md` to their assigned workers. It does not
overwrite user-owned material outside the managed roots. The two governing specs
were moved into `.tusker/specs/` with their authored content preserved, and no
compatibility copy remains. The shared serve graph/detail test remains in this workstream
because it consumes the same resolver contract as the lifecycle checks.

## Changes

- Added shared semantic relationship extraction for metadata, Markdown, and
  Obsidian links. Generated map artifacts, validation, search, and the serve
  graph/detail/backlink API use the same resolver.
- Broken managed routes now produce named validation/map defects. External
  report/source links remain navigation and are not silently blessed as specs.
- `docs new --kind spec` now scaffolds `sources` and `decisions_locked` beside
  the common identity, routing, and freshness fields.
- Updated the CLI, knowledge, storage, and overview routes with current roots,
  bounded search behavior, supersession, and generated-map rules.
- Added lifecycle regression coverage for semantic links, supersession
  backlinks, broken routes, external tracker references, generated-map freshness,
  canonical spec discoverability, and preservation of a user-owned file outside
  the managed corpus.

## Executed checks

Host: `Saravanans-MacBook-Pro.local` (`Darwin arm64`). Source `HEAD` at check:
`03201019308f`.

| Command | Result |
| --- | --- |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./internal/docgraph -count=1 -v` | PASS; parser, map, freshness, secure-write, resolver, semantic-link, and search tests passed. |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./cmd/tusker -run '^Test(DocsListEndpoint|DocDetailRendersBodyAndHeader|DocLinksResolveAcrossCorpus|DocBacklinksListed|DocLinksPipeLabelSyntaxMatchesFrontend|DocSaveWritesFile|DocSaveRefreshesCorpus)$' -count=1 -v` | PASS; the API graph/detail/save regressions passed in 4.566s, including all six semantic edge consumers and user-document refresh behavior. |
| `git diff --check -- internal/docgraph cmd/tusker/docs_cmd.go cmd/tusker/v7_traceability.go docs/system` | PASS; no whitespace errors. |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./cmd/tusker -run '^TestTrust(DocumentRouting|DocsLifecycle)$' -count=1 -v` | PASS; `TestTrustDocsLifecycle` and `TestTrustDocumentRouting` executed, including generated-map freshness, user-file preservation, broken-route, supersession, scaffold, and 100/1000-document scenarios. |
| `/tmp/tusker-trust-current skill sync --repo . --mode symlink --source "$PWD" --json` (candidate SHA-256 `1a7695e00583fec1e05fbc9391e6ba34e888d57f3eab14d93368554c9fd7363d`) | PASS; repo-local `.agents/skills/tusker` and `.claude/skills/tusker` point to canonical `skills/tusker`; no global skill was installed. |
| `/tmp/tusker-trust-current skill doctor --package skills/tusker --strict --json`; same for `.agents/skills/tusker` and `.claude/skills/tusker` | PASS; all three returned `ok:true`, zero errors/warnings, and `provenance.status: current`; source is `canonical` for `skills/tusker` and `symlink` for both managed exports. |

The package check is executable proof for the shared lifecycle seam at the
recorded revision. The command-level lifecycle and routing scenarios execute,
and the repo-local skill exports pass strict provenance checks. The final
source adjustments add same-directory Markdown resolution and prevent
duplicate serve dangling issues; root must rerun the focused Go commands before
treating those results as current. Global installed-skill refresh, downstream
customization, and human/live checks remain external.

## Acceptance state

| Acceptance | Evidence | State |
| --- | --- | --- |
| A1 | Current ownership route is documented; spec scaffold carries source/lock fields; user-owned file preservation and shared corpus tests pass. | Prior focused-test and repo-local provenance verified; rerun pending after final source adjustments |
| A2 | Semantic links, backlinks, supersession, broken-route defects, generated-map agreement, and shared detail/backlink API resolution pass in package and command regressions. | Prior focused-test verified; rerun pending after final source adjustments; UI surface remains worker-owned |
| A3 | Freshness remains commit-anchored and no mass timestamp mutation was made; behavior routes document the current convention. | Source and repo-local provenance verified; global example pending |
| A4 | Broken managed routes fail the affected validator; unrelated external links remain advisory/navigation; the focused command regression passes. | Focused-test verified; installed downstream gate open |

## Remaining gates

FLW-T-0022 must complete before the lifecycle task. FLW-T-0012
(`cli-guide`) and FLW-T-0013 (`enforcement-lint`) remain hard dependencies.
The explicit candidate passed repo-local skill sync and strict package doctors;
global user-skill/CLI installation remains deferred pending the final proof
candidate. Fresh downstream customization checks, the proof page update, UI
surface acceptance, tracker closeout, and human/live acceptance remain
external or worker-owned gates. Generated map outputs still need regeneration after the steward updates
managed plan/task spec references; no tracker state, daemon, provider, global
install, downstream project, or generated map was mutated here.
