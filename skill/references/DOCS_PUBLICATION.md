# Docs publication

Use this when the user wants project docs, a public docs site, a user guide, release notes, or agent-readable canon. Tusker owns the publication rules. Astro/Starlight only renders the exported tree.

## Read order

1. `README*`, `AGENTS.md`, `CLAUDE.md`, and obvious architecture files.
2. `tusker/README.md` and the epic roster from `tusker epics`.
3. Existing repo docs under `docs/` and `docs/publication.yaml`.
4. Generated manifests if present:
   - `site/public/llms.txt`
   - `site/public/llms-full.txt`
   - `site/public/canon-manifest.json`
   - `site/src/generated/content-manifest.json`

If a source file disagrees with `canon-manifest.json`, trust the manifest first and call out the conflict. Do not quietly cite stale archaeology. Tags are hints; `canonical: true` plus `canonical_status` is authority.

## Pick the source

| Need | Source |
|---|---|
| implementation canon | canonical D-note: `tusker new-doc --audience developer --canon-for <ACR>` |
| user guide | user doc: `tusker new-doc --audience user` |
| support/runbook | support doc: `tusker new-doc --audience support` |
| release notes | release doc: `tusker new-doc --audience release` |
| repo-native spec or README | explicit `docs/publication.yaml` entry |
| one-story explanation | companion D-note: `tusker new-doc --audience developer --companion-to <STORY-ID>` |

Stories and bugs are execution records, not public docs. Their `## Evidence` proves work happened, but it does not auto-publish yet.

## Publish a vault doc

```bash
tusker new-doc --epic <ACR> --title "<Guide>" \
  --audience user \
  --status approved \
  --publish true \
  --publish-path user/guides/<slug> \
  --publish-description "<one sentence>"

tusker docs export --site ./site
tusker docs build --site ./site
```

Route rules:

- no leading or trailing slash
- first segment is `developer`, `user`, `release-notes`, `support`, or `internal`
- final segment is not `index`
- route must be unique

## Publish repo docs

Add the doc or directory to `docs/publication.yaml`:

```yaml
repo_docs:
  - source: docs/specs
    include: "*.md"
    route_prefix: developer/specs
    audience: developer
    section_title: Specs
    canonical: true
    canonical_status: draft
    owner_epic: ORC
    verified_at: "2026-04-28"
    tags: [specs]
```

Do not make Astro crawl random markdown. If it should publish, register it.

Canon registry fields:

| Field | Meaning |
|---|---|
| `canonical` | opt-in; without this, a repo doc can publish but is not in `canon[]` |
| `canonical_status` | `draft`, `approved`, `deprecated`, or `historical` |
| `owner_epic` | epic acronym agents should use for "what is canon for X?" |
| `verified_at` | date the doc was last checked against implementation |
| `deprecated` + `superseded_by` | keep stale pages visible without letting agents cite them as current |

## Route lifecycle

When a published route is renamed, add the old route to the replacement page:

```yaml
redirect_from:
  - developer/architecture/old-runtime-topology
```

`tusker docs export` writes `site/src/generated/routes-removed.json` for routes that disappeared without a replacement. `tusker validate` fails while that file has removed routes. Fix it by restoring the source or adding `redirect_from` to the replacement page and exporting again.

## Build and preview

```bash
tusker docs export --site ./site
tusker docs dev --site ./site --watch
tusker docs build --site ./site
```

`--watch` re-exports when vault docs, attachments, or registered repo docs change while Astro serves the generated tree.

NPM scripts in `site/package.json` are wrappers around the Tusker compiler:

```bash
npm --prefix site run export
npm --prefix site run dev
npm --prefix site run build
```

Do not use `site/scripts/sync-docs.mjs` as the publication path. It is legacy glue and bypasses the route, asset, link, and manifest logic agents need.

`tusker validate` checks generated docs state when a site manifest exists: public/generated manifests must match, published sources must not be newer than `generatedAt`, manifest sources must still exist, removed routes must be covered by `redirect_from`, and each active epic must have a canon entry. If validation says the manifest is stale, run `tusker docs export`.

## Evidence

Attach execution proof to stories with:

```bash
tusker attach-evidence --id <ID> --kind screenshot --path <file> --note "<caption>"
```

Tusker copies local evidence into `tusker/Attachments/<ID>/` and appends a markdown link to `## Evidence`. Published docs can reference local assets and the exporter will copy/rewrite them, but story evidence is not yet promoted into release pages automatically. If a user-facing doc needs media today, reference the selected screenshot/video from the doc itself.

## Agent rules

- Read `canon-manifest.json` before broad repo-doc archaeology.
- Use `canon[].canonical_status` before trusting a page: `approved` is safe, `draft` needs verification, `deprecated`/`historical` is archaeology.
- Treat `site/src/content/docs/**` as generated output, not authoring source.
- Treat `_system/generated/**` as generated indexes, not source.
- Use `docs/publication.yaml` for repo docs; use D-note frontmatter for vault docs.
- Do not publish internal notes by moving files into the site tree.
- If a doc should outlive a story, create a D-note instead of stuffing it into `## Evidence`.
