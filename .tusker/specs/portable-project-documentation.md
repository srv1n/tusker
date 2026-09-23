---
subject: portable-project-documentation
title: "S46: Portable project documentation"
keywords: [S46, documentation, placement, domains, proposals, decisions, migration]
part_of: overview
status: canonical
read_when: "Implementing or reviewing the S46 documentation placement migration."
skip_when: "Operating unrelated tasks or changing execution scheduling."
updates:
  - docs/system/knowledge-and-feedback.md
  - docs/system/cli.md
  - docs/system/skills.md
  - docs/system/00-overview.md
decisions_locked: true
---

# S46: Portable project documentation

## Outcome and authority

Product knowledge belongs to its repository and remains readable, linkable and publishable after Tusker is removed. All managed product knowledge lives under `docs/system/`. Tusker retains work state, tool configuration, generated caches and thin routing pointers under `.tusker/`.

This specification records the user's S46 placement direction and the bounded implementation decisions below. Its current location is a bootstrap exception because the present CLI resolves governing specs under `.tusker/specs/`. The adoption story moves this file to `docs/system/proposals/portable-project-documentation.md` while retaining subject identity. Task references initially use the concrete path; subject identity remains stable across migration.

Authoring this wave is inert. The present request authorizes specification and task creation, not execution, deployment, registration or automation changes. Typed architect/origin contacts are omitted because no registered route has been verified. The origin is this user-directed Codex conversation in the Tusker repository, unbound provenance. Questions return through the operator with task ID, acceptance ID, facts, decision needed and recommendation.

## Locked placement and document contract

| Location | Owner and content |
| --- | --- |
| `docs/system/00-overview.md` | Human-maintained entry point and relative links to domains, proposals and decisions. |
| `docs/system/domains/<domain>/00-index.md` | Domain purpose, reading order and links to focused current chapters. Existing useful current pages may remain in place; do not split or rewrite them mechanically. |
| `docs/system/proposals/` | Change specifications, their lifecycle and `updates` references. |
| `docs/system/decisions/` | One durable product decision per file, with rationale and related documents. |
| `.tusker/` | Tasks, waves, work decisions, evidence, events, runtime/configuration, generated outputs, compatibility mappings and thin documentation pointers. No duplicate product prose. |

Work/lifecycle decision records stay with the tracker. Product decisions move to documentation. Do not rewrite immutable evidence or historical events to modernize their paths.

New documents declare `kind: doc | proposal | decision` in YAML. Kind is independent of location and lifecycle. Keep `--kind spec` as a CLI compatibility spelling for proposal; support `--kind proposal` and `--kind decision`. A `doc` without a domain continues to default to the current system root; a specified domain targets its folder. New domain indexes use `00-index.md`. Existing `INDEX.md` scanning exclusions must be narrowed to known generated files so authored indexes are not silently hidden.

Lifecycle values are kind-specific: docs use `current | superseded`; proposals use `proposed | accepted | implemented | superseded`; decisions use `proposed | accepted | superseded`. Superseded documents name `superseded_by`. Proposal acceptance records intent, not implementation proof. `implemented` requires recorded completion evidence and updated current documentation; neither file movement nor task admission sets it.

`code_conformance: unverified | matches | drift | not_applicable` is separate from lifecycle. Reuse `last_verified` as the checked date and commit; `matches` requires that stamp and a stated verification scope. Partial implementation is `drift` with the unmatched scope named in prose. Unknown legacy conformance remains `unverified`. A changed source path can signal a stale verification, but Tusker must not infer semantic correctness from a timestamp. Historical rationale may use `not_applicable`.

Preserve `subject`, `keywords`, `part_of`, `read_when`, `skip_when`, `describes`, `updates`, `decides_for`, `sources`, aliases and supersession. Prefer normal relative Markdown links for portable human navigation. Front-matter graph edges enrich the same documents; they must not be necessary to read them outside Tusker. Do not manufacture history, conformance or proposal approval when migrating old `status: canonical` material.

## Compatibility and storage

The shared docgraph owns parsing, classification, resolution and validation for CLI and UI. During transition, read legacy `.tusker/specs/` and domain routes; all new knowledge writes use `docs/system/`. Explicit new metadata governs classification. Legacy kind inference is a compatibility fallback and surfaces a migration diagnostic. Missing or ambiguous lifecycle maps require an explicit inventory disposition.

Reuse existing subject/alias/supersession resolution. If an old file URL needs a forwarding stub, it contains only its successor link and minimum identity metadata; it must not duplicate knowledge or shadow the current subject. Keep historical task/evidence references resolvable without changing their recorded bytes. Active material task references can change only through normal revision-aware CLI authority; report stale dependency/proof bindings and require ordinary rebinding/review, never silently bless them.

Generated `graph.json` and generated corpus indexes move to `.tusker/_generated/docs/`; authored domain indexes remain in `docs/system/`. A plain overview must remain useful with those outputs absent. Root/task scanners, freshness/map code and server graph readers must agree on the new generated location. No new publishing service or custom storage framework is required.

Extend the existing reviewed `docs adopt` mechanism with explicit source-to-target moves and reference repairs. Preview is read-only and fingerprinted. Apply checks unchanged inputs, duplicate subjects, target collisions, path escapes, symlinks, dirty owned files and active material references before writes. Preserve originals or a recoverable journal until completion; an interrupted apply is safely resumable and a repeat is idempotent. Initialization and skill refresh never migrate documents implicitly.

## Verified implementation seams

- `internal/docgraph/docgraph.go`: `ParseDocHeaders`, `kindForPath`, `scanRepository`; currently infers kind by root and skips all `INDEX.md` files.
- `internal/docgraph/resolver.go`, `discovery.go`, `docmap.go`, `freshness.go`: managed roots, relationships, browse, generated outputs and verification.
- `cmd/tusker/docs_cmd.go`, `docs_browse_cmd.go`, `docs_adopt_cmd.go`, `init_docs.go`, `cli.go`: creation, default paths, reviewed adoption and onboarding.
- `cmd/tusker/v7_domain_cmd.go`, `v7_validation.go`, `v7_skill_cmd.go`, `commands_v7.go`, `capsule.go`, `v7_traceability.go`: domain authoring, packet routing and governing refs. `v7SpecRefExists` admits specs/decisions by resolved kind. `v7_wave_authorization.go` separately hashes spec paths; the model story must route this consumer through the same resolver.
- `cmd/tusker/serve_docgraph.go` and `internal/serve/ui/src/features/knowledge/`: existing shared corpus API, literal folder tree, graph, optimistic-concurrency editor and badges.
- `skills/tusker/`: canonical shipped operating guidance and templates. Generated/copied installations are refreshed through supported tooling only.

At authoring, `.tusker/specs/` contains 32 tracked Markdown files plus a concurrent untracked `ui-reset.md`; the domain tree contains three Markdown files. Re-inventory at execution. There are existing unrelated dirty UI, build output and WUX task changes. They are not owned by this wave. Coordinate any overlapping live author before editing; do not overwrite, reset, migrate or incorporate their uncommitted work.

## Stories and dependency order

| Key | User story | Depends on |
| --- | --- | --- |
| model | As a reader, I can identify document purpose, lifecycle and code conformance without decoding a folder name. | none |
| cli | As a project author, I can create and discover every document kind in the portable tree. | model |
| routing | As a task agent, I receive the same domain knowledge and governing references that humans read. | cli |
| migration | As an existing user, I can preview and apply a recoverable migration while old references still resolve. | routing |
| ui | As a product user, I can browse, edit and understand the unified documentation tree. | model |
| guidance | As a new or returning user, I receive one consistent placement rule and usable examples. | migration, ui |
| adoption | As a Tusker maintainer, I can use this layout in Tusker's own repository without damaging concurrent work. | guidance |
| qualification | As a releasing maintainer, I have evidence that both fresh and migrated projects work across CLI, agents and UI. | adoption |

Backend command edits are serialized through cli, routing and migration. UI owns `serve_docgraph.go` and knowledge components; any exported model change belongs to model and is consumed by UI. Guidance owns canonical skill/docs wording after implementation interfaces settle. Adoption owns document moves and the migration receipt, and uses revision-aware CLI operations for any tracker changes. Qualification owns final integrated proof and regenerated assets after earlier work lands.

Authoring produced W-0040, with model TSK-T-0055, cli TSK-T-0056, routing TSK-T-0057, migration TSK-T-0058, ui TSK-T-0059, guidance TSK-T-0060, adoption TSK-T-0061 and qualification TSK-T-0062. Preflight found a current product defect: wave authoring accepts a subject spec reference but wave material hashing rejects it with MATERIAL_INVALID. The wave retains that valid authored subject reference; the model task owns the shared-resolver repair and regression test. The current CLI has no supported wave spec-reference amendment. No tracker file is edited by hand to hide the defect. Until that repair is implemented under separate execution authority, W-0040 remains inert and not wave-start-ready; its first task can be inspected and separately authorized through the normal task entry.

## Acceptance at the product boundary

1. A fresh project creates a domain index/chapter, proposal and decision with correct paths, metadata and portable relative links. Init is idempotent and does not modify user files.
2. The same subjects resolve from `docs find/read/browse/backlinks`, agent packets, task `spec_refs`, UI Files/Graph and editor saves. Wrong kinds, bad sections, duplicate identities and invalid conformance fail visibly.
3. Legacy project migration previews without writes; collision, source drift, symlink escape and partial failure cases preserve data. Retry is safe. Historical links resolve and active task material is revalidated without manufacturing proof.
4. The UI distinguishes accepted intent from code conformance, preserves keyboard/mobile navigation, and rejects stale editor revisions without overwriting the file.
5. In a disposable copy with `.tusker/` absent, the overview, indexes, chapters, proposals and decisions remain navigable through ordinary Markdown links; a renderer can consume that tree without Tusker runtime or generated graph files.
6. Tusker's own adopted corpus has one current owner per subject. Remaining legacy pointers, blocked concurrent files and unmigrated records are enumerated, not silently ignored.

Each task carries focused planned checks. Newly named S46 tests are required deliverables, not assertions that tests already exist. Final qualification must demonstrate the initiating operation and its actual consumer. Keep source inspection, fixture execution, UI observation and installed-product qualification separate. No passing claim is recorded during planning.

## Handoff for other project teams

Prepare an inventory of your product documents and their current links. The target is `docs/system/domains/<domain>/` for current chapters, `docs/system/proposals/` for change specs and `docs/system/decisions/` for product decisions, with stable subject identities and independent lifecycle/conformance fields. Preserve task evidence and product history. Do not rename documents ahead of the compatible Tusker release; first use its reviewed migration preview, resolve ambiguous lifecycle mappings and collisions, then apply the approved mapping. Validate task references, agent routing and ordinary Markdown navigation. Report the migration receipt and unresolved exceptions.
