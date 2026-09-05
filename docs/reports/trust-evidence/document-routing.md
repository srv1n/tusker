# Document routing evidence

Status: source implementation and canonical spec move are complete; the
named focused regression passes were observed in the shared checkout before the
final resolver adjustments. The required rerun is held for root's compile
window. This file records executed checks only. It does not claim FLW-T-0022
passed its tracker transition.

## Convention and ownership

The current resolver has one managed corpus:

- `docs/system/` owns current product behavior;
- `.tusker/specs/` owns governing specs; and
- `.tusker/specs/decisions/` owns durable decisions.

The repository canon pointers reported by the packet are present at
`.tusker/knowledge/domains/project/INDEX.md` and
`.tusker/knowledge/domains/project/CANON.md`; this work leaves their managed
content and tracker metadata to the steward.

`docs/specs/` and `docs/design/` are not governing routes in this current-only
implementation. The two governing specs now live under `.tusker/specs/`; no
compatibility copy or alias remains. A path is not promoted into the governing
corpus merely because it exists.

Owned source paths are `internal/docgraph/`, `cmd/tusker/docs_cmd.go`,
`cmd/tusker/v7_traceability.go`, `cmd/tusker/serve_docgraph.go`, this evidence
file, `cmd/tusker/serve_docgraph_test.go`, and
`cmd/tusker/trust_document_routing_test.go`.
The owned system routes are
`docs/system/00-overview.md`, `docs/system/cli.md`,
`docs/system/knowledge-and-feedback.md`, and
`docs/system/storage-and-runtime.md`. The skills route remains a documentation
owner, while the installed skill source stays with the contract worker. The
proof-and-closeout page stays with the proof worker.

## Changes

- Added one subject/path resolver with source-relative Markdown and Obsidian
  link normalization, supersession forwarding, semantic links, backlinks, and
  managed broken-route reporting.
- Made `docs find` bounded by default (`8` rows), retained total/truncation
  facts in JSON, and kept `read_when`/`skip_when` as compact routing metadata.
- Routed V7 spec validation and traceability scanning through the current
  resolver and current roots.
- Made generated Mermaid and graph JSON consume the same semantic relationships
  as validation.
- Added current-root, supersession, broken-route, tracker-reference,
  real-spec discoverability, and 100/1000-fixture regression scenarios.

## Executed checks

Host: `Saravanans-MacBook-Pro.local` (`Darwin arm64`). Source `HEAD` at check:
`03201019308f`.

| Command | Result |
| --- | --- |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./internal/docgraph -count=1 -v` | PASS; all existing and new resolver/search/map/freshness tests passed. |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./cmd/tusker -run '^Test(DocsListEndpoint|DocDetailRendersBodyAndHeader|DocLinksResolveAcrossCorpus|DocBacklinksListed|DocLinksPipeLabelSyntaxMatchesFrontend|DocSaveWritesFile|DocSaveRefreshesCorpus)$' -count=1 -v` | PASS; the API graph/detail/save regressions passed in 4.566s, including source/link edges, relative Markdown resolution, backlinks, and refreshed saves. |
| `git diff --check -- internal/docgraph cmd/tusker/docs_cmd.go cmd/tusker/v7_traceability.go docs/system` | PASS; no whitespace errors. |
| `scripts/with-validation-lock.sh -- env GOMAXPROCS=2 go test -p=1 -parallel=1 ./cmd/tusker -run '^TestTrust(DocumentRouting|DocsLifecycle)$' -count=1 -v` | PASS; `TestTrustDocumentRouting` and `TestTrustDocsLifecycle` executed. The 100-document fixture returned 8 of 100 matches in 1,573 bytes (138.916µs); the 1000-document fixture returned 8 of 1000 matches in 1,584 bytes (1.5625ms). |
| `/tmp/tusker-trust-current skill sync --repo . --mode symlink --source "$PWD" --json` (candidate SHA-256 `1a7695e00583fec1e05fbc9391e6ba34e888d57f3eab14d93368554c9fd7363d`) | PASS; repo-local `.agents/skills/tusker` and `.claude/skills/tusker` point to canonical `skills/tusker`; no global skill was installed. |
| `/tmp/tusker-trust-current skill doctor --package skills/tusker --strict --json`; same for `.agents/skills/tusker` and `.claude/skills/tusker` | PASS; all three returned `ok:true`, zero errors/warnings, and `provenance.status: current`; source is `canonical` for `skills/tusker` and `symlink` for both managed exports. |

The package check proves the shared docgraph implementation at the recorded
revision. The command-level 100/1000 journey, supersession, broken-route,
scaffold, and lifecycle scenarios also executed at that point. The final
source adjustments add same-directory Markdown resolution and prevent duplicate
serve dangling issues; root must rerun the focused Go commands before treating
those results as current. The repo-local skill exports passed strict provenance
checks; global CLI/skill refresh and live downstream runs remain open.

## Acceptance state

| Acceptance | Evidence | State |
| --- | --- | --- |
| A1 | Shared resolver, current-root traceability, generated graph wiring, current-root/supersession fixture, and scaffold route execute in the focused regression. | Source and prior focused-test verified; rerun pending after final source adjustments |
| A2 | Bounded search, read/skip guidance, supersession forwarding, semantic links, backlinks, and 100/1000-document journeys pass. | Prior focused-test verified; rerun pending after final source adjustments |
| A3 | Metadata search and measured 100/1000-document routing pass without a full-tree result; repo-local skill provenance is current. | Focused-test and repo-local provenance verified; global gate open |

## Remaining gates

FLW-T-0006 (`baseline`) and FLW-T-0008 (`token-baseline`) remain hard
dependencies in the delivery plan. FLW-T-0012 (`cli-guide`) and FLW-T-0013
(`enforcement-lint`) are required by the lifecycle task. The named tracker
tasks were observed as backlog/held, and packet execution was not authorized;
no tracker state, daemon, provider, install, or downstream project was
mutated. The shared checkout's generated map outputs should be regenerated after
the steward updates managed plan/task spec references; this work did not mutate
managed state or generated outputs. The explicit candidate passed repo-local skill sync
and strict package doctors. Global user-skill/CLI installation remains deferred
pending the final proof candidate; downstream fresh-workspace checks and
human/live acceptance remain external gates.
