# Schema

Frontmatter is the machine layer. Note body is the human layer. Tusker's Go YAML parser preserves the vault schema cleanly, and Obsidian Properties round-trip without drama.

## Note types

- `epic` — a coherent workstream or subsystem. Lives at `epics/<ACR>/index.md`.
- `story` — unit of work. Feature, refactor, migration, docs, chore, research, security, incident. Lives at `epics/<ACR>/<ACR>-S-NNNN.md`.
- `bug` — defect or regression. Lives at `epics/<ACR>/<ACR>-B-NNNN.md`.
- `doc` — polished standalone doc that will outlive individual stories. Lives at `epics/<ACR>/<ACR>-D-NNNN.md`.
- `note` — free-form (dashboard, architecture, daily). No lifecycle.

No project layer, no change layer, no tier. Use `epic + story/bug/doc` everywhere.

## Status enums

### `epic.status`
`intake`, `active`, `paused`, `done`, `cancelled`

### `story.status`
`intake`, `active`, `blocked`, `in_review`, `done`, `cancelled`

### `bug.status`
Same as story.

### `review_state`
`none`, `verification_requested`, `requested`, `changes_requested`, `approved`

### `doc.status`
`draft`, `review`, `approved`, `published`, `archived`

Status transitions are recorded in the `transitions[]` frontmatter array as an append-only audit log (from Symphony borrowings). `set-status` writes the row; never hand-edit.

## Other enums

- `story.change_type`: `feature`, `bug`, `refactor`, `migration`, `security`, `docs`, `chore`, `research`, `incident`
- `risk`: `low`, `medium`, `high`, `critical` (orthogonal to size)
- `size`: `s`, `m`, `l`, `xl`
- `priority`: `p0`, `p1`, `p2`, `p3`, `icebox`
- `delegation`: `execute`, `explore`, `escalate`
- `ai_assistance`: `none`, `light`, `moderate`, `heavy`
- `ai_tools`: array of strings (e.g. `["claude-code", "codex", "cursor"]`)
- `doc.audience`: `developer`, `user`, `release`, `support`, `internal`
- `surfaces`: array including `frontend`, `desktop`, `mobile`, `api`, `backend`, `runtime`, `cli`, `docs`, others as needed

## Epic frontmatter

Required: `id`, `title`, `type`, `status`, `owner`, `created`, `updated`

Recommended: `target_release`, `spec_source` (repo-relative path or wikilink to the canonical design doc), `docs` (array of wikilinks to DOC notes), `success_metrics` (array of strings), `tags`

Status-stamp dates: `started`, `completed`, `cancelled_at`, `paused_since`.

Append-only: `transitions[]`.

## Story frontmatter

Required before `status: active`:
- `id`, `title`, `type`, `status`, `change_type`, `epic`
- `size`, `risk`, `priority`, `delegation`
- `ai_assistance`
- `requester`, `created`, `updated`

Recommended: `assignee`, `surfaces`, `ai_tools`, `ai_session_log`, `due`, `blocked_by`, `blocks`, `related`, `tags`

Claim/dispatch fields (Symphony): `dispatch_state` (`unclaimed` | `claimed` | `running` | `done` | `failed` | `stalled` | `cancelled`), `claimed_by`, `claimed_at`, `run_attempts`, `last_attempt_at`, `failure_class`.

Attestation fields: `attested_by`, `attested_at`, `attested_role`, `signoff_by`, `signoff_at`.

DoD split: `dod_code_complete` (boolean), `dod_user_verified` (boolean).

Status-stamp dates: `started`, `review_opened`, `completed`, `cancelled_at`, `blocked_since`.

Append-only: `transitions[]`.

## Bug frontmatter

Same required set as story, including `change_type: bug`.

Additional recommended: `severity`, `regression_of`.

## Doc frontmatter

Required: `id`, `title`, `type`, `status`, `epic`, `audience`, `created`, `updated`

Recommended: `doc_intent` (`canon|companion` for developer docs), `canon_for` (wikilink to epic for canonical docs), `story` (wikilink to originating story for companion docs), `canonical` (boolean), `canonical_status` (`draft|approved|deprecated|historical`), `owner_epic`, `verified_at`, `deprecated`, `superseded_by`, `publish` (boolean), `publish_path`, `publish_description`, `publish_order`, `publish_section_title`, `redirect_from`, `publish_url`, `published_at`, `related`, `tags`.

Publication contract:

- `publish: true` requires `status: approved|published`
- `publish: true` requires non-empty `publish_path` and `publish_description`
- `publish_path` is a unique route without a leading/trailing slash; top-level lane must be `developer`, `user`, `release-notes`, `support`, or `internal`
- `publish_order`, if set, must be an integer
- `publish_section_title`, if set, must be non-empty
- `redirect_from`, if set, is a list of old route paths without leading/trailing slashes
- `canonical: true` requires `canonical_status`
- `canonical_status: approved` should set `verified_at`
- deprecated docs should set `superseded_by`

## Body sections

Required sections depend on type + risk. See `SKILL.md §Required sections by risk` for the matrix.

Summary:

- Every story/bug has a `---` HR followed by `## Agent handoff`. Above is human-authored spec. Below is agent execution packet + work log.
- Validator checks required sections are present, and reports missing substance separately. Empty placeholders still do not count as a real ticket.
- `## Work log`, `## Agent handoff`, and `## Evidence` are exempted from substance checking at creation time — they get filled as work progresses.
- Evidence gate fires at `in_review`/`done` for risk ≥ medium.
- UI demo rule: if `change_type: feature` AND `surfaces` contains `frontend`/`desktop`/`mobile` AND risk ≥ medium, `## Evidence` must include a demo asset at `in_review`/`done`.

## Linking conventions

- Epic frontmatter references: `docs: ["[[<EPIC>-D-0001]]"]`, `spec_source: "docs/specs/X.md"` or `spec_source: "[[<EPIC>-D-0001]]"`
- Story-to-epic: canonical storage is `epic: "[[ABC]]"`; the CLI resolves either form, but the templates use wikilinks
- Story-to-story: `blocked_by`, `blocks`, `related` use wikilinks: `related: ["[[ABC-S-0007]]"]`
- Doc-to-story companion link: `story: "[[ABC-S-0007]]"`
- Canonical developer doc: `doc_intent: "canon"` plus `canon_for: "[[ABC]]"`; `new-doc --canon-for` also stamps `canonical: true`, `owner_epic`, and `canonical_status`

## Generated indexes

`_system/generated/` holds derived JSON, refreshed by `tusker reindex`:

- `epics.index.json` — one row per epic
- `stories.index.json` — one row per story
- `bugs.index.json` — one row per bug
- `docs.index.json` — one row per doc
- `links.index.json` — graph edges
- `attestation.index.json` — stories/bugs at `in_review` with human review requested
- `publication.index.json` — exporter-ready publication manifest; top-level `sources.vault_docs` holds publishable vault docs with route metadata (`publish_path`, `publish_description`, `publish_order`, `publish_section_title`, `redirect_from`) and canon lifecycle fields (`canonical`, `canonical_status`, `owner_epic`, `verified_at`, `deprecated`, `superseded_by`)
- `dashboard.json` — Symphony-borrowed dispatcher snapshot: queues (`unclaimed`/`claimed`/`running`), per-agent activity, failure counters
- `summary.json` — vault-wide counts

Never hand-edit `_system/generated/`.

## Published docs manifests

`tusker docs export` writes site-side manifests:

- `site/src/generated/content-manifest.json` — schema-versioned route list with title, audience, source path, tags, summary, and canon lifecycle fields.
- `site/src/generated/canon-manifest.json` — schema-versioned current canon plus all published docs for the static site build. Canon entries come from explicit `canonical: true` metadata, not from broad tags like `specs`.
- `site/public/canon-manifest.json` — same canon map, available from deployed docs.
- `site/src/generated/routes-removed.json` — unredirected published routes that disappeared since the previous export.
- `site/public/llms.txt` and `site/public/llms-full.txt` — agent-friendly route catalogs.

Agents should read `site/public/canon-manifest.json` before treating old repo docs as current truth.

When a docs site manifest exists, `tusker validate` fails if either manifest copy diverges, if a published source is newer than the manifest, if a manifest source was deleted, if a route disappeared without `redirect_from`, or if an active epic lacks a canon entry in `canon[]`.

## Hard invariants (from Symphony borrowings)

Validator fails hard if:

- `ID_COLLISION` — two notes share the same `id`
- `PATH_MISMATCH` — filename does not match the id-derived canonical path
- `PATH_ESCAPE` — a note references a path that resolves outside the vault

## Filename rules

- Epic index: `epics/<ACR>/index.md` (content path is canonical; `id` in frontmatter = the acronym)
- Story: `epics/<ACR>/<ACR>-S-NNNN.md` (zero-padded 4-digit serial)
- Bug: `epics/<ACR>/<ACR>-B-NNNN.md`
- Doc: `epics/<ACR>/<ACR>-D-NNNN.md`
- Note (free-form): anywhere except `epics/*/` and `_system/`
