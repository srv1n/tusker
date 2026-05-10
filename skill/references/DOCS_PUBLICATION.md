# Docs publication

Use this when the user wants project docs, a public docs site, a user guide, release notes, support docs, runbooks, or agent-readable canon.

## Read order

1. `README*`, `AGENTS.md`, `CLAUDE.md`, and obvious architecture files.
2. `tusker/README.md` and the epic roster from `tusker list --type epic`.
3. V5 docs under `tusker/docs/**`.
4. Repo docs registered through `docs/publication.yaml`.
5. Generated manifests if present:
   - `site/public/canon-manifest.json`
   - `site/public/llms.txt`
   - `site/public/llms-full.txt`
   - `site/src/generated/content-manifest.json`

If a source file disagrees with `canon-manifest.json`, trust the manifest first and call out the conflict. Do not quietly cite stale archaeology.

## V5 docs model

Docs are durable knowledge pages under `tusker/docs/**`.

Tasks carry:

- `domains`: broad areas the work touches
- `doc_nodes`: exact docs targets from `_config/docs-map.yaml`
- `## Knowledge delta`: what changed in the reader's mental model

If `doc_nodes` is non-empty, close must prove one of three things:

1. docs were already correct,
2. docs were updated,
3. docs update was explicitly waived with a reason.

## Pick the source

| Need | Source |
|---|---|
| implementation canon | doc under `tusker/docs/**` with `kind: canon` |
| user guide | doc with `audience: user` |
| support/runbook | doc with `audience: support` or `kind: runbook` |
| release notes | doc with `audience: release` or `kind: release` |
| repo-native spec or README | explicit `docs/publication.yaml` entry |
| one-task explanation | companion doc linked to the task |

Tasks are execution records, not public docs. Their evidence proves work happened, but does not automatically become publication content.

## LLM authoring contract

This is the rule agents must apply before drafting docs, not after review.

Documentation tickets are not "copy the task into a page" tickets. For every page, choose:

1. one audience: `user`, `developer`, `agent`, or `internal`
2. one primary Diátaxis mode: `tutorial`, `how-to`, `reference`, or `explanation`
3. one source authority set: Tusker D-notes, registered repo docs, or code paths

Human docs are synthesized outputs. Source material is input.

Do not publish these directly as user/developer prose:

- task records
- evidence logs
- D-note bodies
- implementation scratchpads
- generated manifests
- agent-only instructions
- stale or unregistered markdown

Use this split:

| Audience | Write for | Keep out by default |
|---|---|---|
| `user` | outcome, steps, expected result, common fixes | task IDs, stale metadata, internal paths |
| `developer` | contracts, architecture, extension points, validation | raw work logs unless they prove a claim |
| `agent` | exact canon, constraints, source paths, stale triggers | polished onboarding prose |
| `internal` | maintainer decisions, operational risk, migration context | basic tutorial walkthroughs |

Mode contract:

| Mode | Page shape | Reject if |
|---|---|---|
| `tutorial` | guided learning path with a safe result | starts with reference tables |
| `how-to` | direct steps for completing a task | wanders into background essay |
| `reference` | exact facts, schemas, commands, contracts, edge cases | hides edge cases in prose |
| `explanation` | concepts, tradeoffs, why the system works this way | becomes a procedure |

Quality gate before returning docs:

- The first screen makes the reader intent obvious.
- One primary audience and one primary mode are declared by the page shape.
- Raw canon has been transformed for the selected reader.
- User pages avoid internal metadata.
- Developer pages include contracts, source paths, edge cases, and verification.
- Agent/internal pages preserve exact IDs, constraints, source paths, and stale triggers.
- Claims about behavior include a verification path or are marked unverified.

## Diátaxis access model

`_config/docs-map.yaml` is the access layer. Every node must declare:

| Field | Purpose |
|---|---|
| `mode` | Dominant reader intent: `tutorial`, `how-to`, `reference`, or `explanation` |
| `audience` | Primary reader: `developer`, `user`, `operator`, `support`, `release`, `agent`, or `internal` |
| `agent_layer` | Agent treatment: `none`, `capsule`, or `standalone` |
| `source_of_truth` | Files that define the page's facts |
| `stale_when.paths` | Files or globs that should trigger docs freshness review |

Do not force folders to mirror Diátaxis. Tusker uses reader-facing navigation:

| Mode | Default nav |
|---|---|
| `tutorial` | Start here |
| `how-to` | Guides |
| `reference` | Reference |
| `explanation` | Concepts |

Agent docs with `audience: agent` or `agent_layer: standalone` appear under For agents. Capsule docs stay in their human-facing section with a small agent note.

## Docs close gate

```bash
tusker docs check <TASK-ID>
tusker docs apply <TASK-ID> --node <DOC-NODE> --reason "<what changed>"
tusker docs noop <TASK-ID> --node <DOC-NODE> --reason "<why already current>"
tusker docs waive <TASK-ID> <DOC-NODE> --reason "<why no doc change>"
```

Run this before `tusker close` when `doc_nodes` exists or when the task's knowledge delta says docs changed.

High-risk tasks that affect durable understanding need a useful knowledge delta:

| Change type | Topic | Before | After | Audience | Target doc nodes | Mode | Status |
|---|---|---|---|---|---|---|---|

`tusker docs check` reads the task `doc_nodes` and any target nodes in this table. Use existing node IDs from `_config/docs-map.yaml`; unknown nodes are validation failures.

Inspection commands:

| Command | Use |
|---|---|
| `tusker docs model` | Explain the docs philosophy, Diátaxis modes, agent layers, and close gate. |
| `tusker docs map [DOC-NODE]` | Inspect controlled doc nodes and source-of-truth metadata. |
| `tusker docs catalog` | Show reader-facing IA generated from docs-map. |
| `tusker docs freshness [--stale]` | Show stale/verified docs, linked tasks, waivers, and stale triggers. |

## Publish vault docs

```bash
tusker new doc --title "<Guide>" \
  --node user/guides/<slug> \
  --audience user \
  --kind guide \
  --domains docs \
  --publish true

tusker docs export --site ./site
tusker docs build --site ./site
```

Route rules:

- no leading or trailing slash
- stable `node` maps to publication route
- renamed routes need `redirect_from`
- published canon needs `canonical_status`

## Publish repo docs

Use `docs/publication.yaml` for repo-native docs that should publish:

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
    verified_at: "2026-04-29"
    tags: [specs]
```

Do not make Astro crawl random markdown. If it should publish, register it.

## Build and preview

```bash
tusker docs export --site ./site
tusker docs dev --site ./site --watch
tusker docs build --site ./site
```

Generated output includes:

- `site/src/content/docs/**`
- `site/src/generated/content-manifest.json`
- `site/src/generated/canon-manifest.json`
- `site/public/canon-manifest.json`
- `site/src/generated/routes-removed.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

Do not author in `site/src/content/docs/**`. It is generated output.

## Route lifecycle

When a published route is renamed, add the old route to replacement metadata:

```yaml
redirect_from:
  - developer/architecture/old-runtime-topology
```

`tusker validate` should fail while removed routes lack redirects. Fix by restoring the source or adding `redirect_from`, then export again.

## Evidence

Attach execution proof to tasks:

```bash
tusker evidence <TASK-ID> screenshot <file> --note "<caption>"
```

Published docs may reference selected local assets, and the exporter can copy/rewrite them. Do not stuff durable explanation into `## Evidence`; create or update a doc.

## Agent rules

- Read `canon-manifest.json` before broad repo-doc archaeology.
- Use `canonical_status`: `approved` is safe, `draft` needs verification, `deprecated`/`historical` is archaeology.
- Treat `site/src/content/docs/**` as generated output.
- Treat `_system/generated/**` as generated indexes.
- Use `tusker/docs/**` for V5 vault docs.
- Use `docs/publication.yaml` for repo docs.
- If docs impact is real, set `doc_nodes` and fill knowledge delta.
