# Commands

Full CLI reference. `SKILL.md` has the five quick commands; this file covers everything.

## Vault discovery

Every command accepts `[--vault <path>]`. When omitted, `tusker` walks up from cwd looking for a `tusker/` directory (or any folder with `_system/config.yaml`). Pass `--vault` only to override.

## Init (interactive)

```bash
tusker init [--vault <path>] [--yes]
```

Interactive setup for a new project. Runs in cwd:

1. Creates the vault at `./tusker/` (or `--vault <path>`) if not already present.
2. For `AGENTS.md` and `CLAUDE.md` in cwd, prompts to inject a pointer block that names `<vault>/README.md` (project overview + epic roster). Marker-delimited so re-running is idempotent.
3. Prompts to install the repo-contract scaffolds (`.github/` templates, `docs/agent-workflow.md`).

Pass `--yes` to accept all defaults non-interactively (CI, scripts). If stdin is not a TTY and `--yes` isn't set, init errors out.

## Bootstrap (non-interactive)

```bash
tusker bootstrap [--vault <path>] [--epic <ACR> --title <title>]
```

Creates the vault structure: `_system/`, `epics/`, `Dashboard.md`, `Attachments/`, templates, hooks, config. If `--vault` is omitted, creates `./tusker/` in cwd. Also writes `_system/config.yaml` with dispatcher defaults. Non-interactive — use `init` for the prompted onboarding flow.

## Create

```bash
tusker new-epic [--vault <path>] --acronym <ACR> --title <title>
                [--summary "<=120 char one-liner"]
                [--owner <name>] [--spec-source <path-or-wikilink>]
                [--target-release <tag>] [--docs "DOC-ID,..."]
                [--success-metrics "..."] [--tags "..."]

tusker new-story [--vault <path>] --epic <ACR> --title <title>
                 --size s|m|l|xl --risk low|medium|high|critical
                 [--change-type feature|refactor|migration|security|docs|chore|research|incident]
                 [--priority p0|p1|p2|p3|icebox]
                 [--delegation execute|explore|escalate]
                 [--surfaces "frontend,api,runtime,desktop"]
                 [--assignee <name>] [--requester <name>]
                 [--ai-assistance none|light|moderate|heavy]
                 [--ai-tools "claude-code,codex,..."]

tusker new-bug  [--vault <path>] --epic <ACR> --title <title>
                --size s|m|l|xl --risk low|medium|high|critical
                [--priority ...] [--surfaces ...] [--assignee ...] [--ai-assistance ...]

tusker new-doc  [--vault <path>] --epic <ACR> --title <title>
                [--audience developer|user|release|support|internal]
                [--canon-for <EPIC> | --companion-to <STORY-ID>]
                [--status draft|review|approved|published|archived]
                [--publish true|false] [--publish-path <route>]
                [--publish-description <text>] [--publish-order <n>]
                [--publish-section-title <text>] [--publish-url <url>]
                [--tags "architecture,user-guide,..."]

tusker handoff  [--vault <path>] --id <ID> --for worker|verifier|reviewer
```

## Lifecycle

```bash
tusker set-status  [--vault <path>] --id <ID> --status <...>
                   [--actor <name>] [--reason "..."]

tusker attest      [--vault <path>] --id <ID> --by <name> --role agent|human
tusker signoff     [--vault <path>] --id <ID> --by <name>

tusker attach-evidence [--vault <path>] --id <ID>
                       --kind screenshot|video|log|bench|pr
                       --path <file-or-url> [--note "..."]

tusker promote-decision [--vault <path>] --id <ID> --summary "<one line>"
                        [--target architecture|agents]
```

### Status enums

- `epic.status`: `intake | active | paused | done | cancelled`
- `story.status`: `intake | active | blocked | in_review | rework | merging | done | cancelled`
- `bug.status`: `intake | active | blocked | in_review | rework | merging | done | cancelled`
- `doc.status`: `draft | review | approved | published | archived`
- `review_state`: `none | verification_requested | requested | changes_requested | approved`

## Dispatcher / orchestration

```bash
tusker pickup  [--vault <path>] --id <ID> --by <agent> [--reason "..."]
               # atomic claim; enforces agents.concurrency from config.yaml

tusker release [--vault <path>] --id <ID>
               --to running|done|failed|stalled|cancelled
               [--by <name>]
               [--failure-class transient|deterministic|stuck|blocked-by-human|budget-exceeded]
               [--reason "..."]
```

See `references/DISPATCHER.md` for the broader dispatcher model.

## Inspection

```bash
tusker reindex  [--vault <path>] [--json]
                # regenerates _system/generated/*.json and dashboard.json

tusker validate [--vault <path>] [--json]
                # checks all notes against risk-matrix + schema rules

tusker list     [--vault <path>] [--json]
                [--epic <ACR>] [--status <...>] [--type story|bug|doc]

tusker epics    [--vault <path>] [--json]
                # compact roster: ACR, status, s/b/d counts, title, summary
                # run this before logging so you pick the right parent epic
```

## Update installed Tusker

```bash
tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--json]
```

Run this after pulling, rebuilding, or installing a newer Tusker release. It refreshes the installed `tusker` binary link and every existing user skill bundle in `~/.agents`, `~/.codex`, and `~/.claude` from the currently running binary.

Use `--repo <path>` when a repository has repo-local `.agents/skills/tusker` or `.claude/skills/tusker` installs that should be refreshed too.

This is the command that keeps `SKILL.md` and `references/**` in sync with CLI changes. If agents are acting confused, run it before debugging prompts.

## Routing

```bash
tusker move [--vault <path>] --id <ID> --to-epic <ACR>
            [--reason "..."]
            # reassigns a story/bug/doc to a different epic.
            # mints a new id in the target epic, renames the file,
            # appends a transition + work-log entry, and reports any
            # other notes still referencing the old id.
```

Use `move` when an agent filed the item under the wrong epic. References in
other notes aren't auto-rewritten — the command reports them so you can fix
wikilinks manually, then run `tusker reindex` + `tusker validate`.

## Repo contract

```bash
tusker sync-repo-contract --repo <path> [--force]
```

Installs AGENTS.md, `.gitignore` additions, and git hook scaffolds into a repo that uses this vault. See `references/REPO_CONTRACT.md`.

## Docs publishing

```bash
tusker docs init [--site <path>] [--force]
tusker docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
tusker docs dev [--vault <path>] [--site <path>] [--watch] [--port <n>] [--host <host>]
tusker docs build [--vault <path>] [--site <path>] [--public-only] [--json]
```

`new-doc` creates vault docs. `docs export/dev/build` publishes approved/published vault docs plus explicitly registered repo docs into the Astro/Starlight site.

The exporter writes:

- `site/src/content/docs/**` — generated Starlight content
- `site/src/generated/content-manifest.json` — schema-versioned list of all published routes
- `site/src/generated/canon-manifest.json` and `site/public/canon-manifest.json` — schema-versioned canon/published truth map for agents
- `site/src/generated/routes-removed.json` — vanished published routes not covered by `redirect_from`
- `site/public/llms.txt` and `site/public/llms-full.txt` — compact route catalogs

`docs dev --watch` starts Astro and re-exports when vault docs, attachments, or registered repo docs change.

Do not use `site/scripts/sync-docs.mjs` as the docs pipeline. It is legacy glue; `tusker docs export` is the compiler.

Published repo docs become canon only when their `docs/publication.yaml` entry or frontmatter sets `canonical: true` and `canonical_status`. Tags like `specs` or `architecture` do not make a page authoritative by themselves.

`tusker validate` also checks generated docs state when a docs site is present: stale manifests, mismatched manifest copies, deleted manifest sources, removed routes without `redirect_from`, and active epics with no canon entry are failures.

For `--publish true`, set:

- `--status approved` or `--status published`
- `--publish-path <lane/path>` where lane is `developer`, `user`, `release-notes`, `support`, or `internal`
- `--publish-description "<one sentence>"`
- `redirect_from` in frontmatter when renaming an already-published route

## Exit codes

- `0` — success
- `1` — user error (missing/invalid args, vault not found)
- `2` — validation failure
- `3` — filesystem or I/O error

## Output modes

Most inspection commands accept `--json` for machine-readable output. The dispatcher uses this; agents usually want the default human format.

## Common patterns

### "What's next?"

```bash
tusker reindex
tusker list --status active
```

### "Claim the top of ready, work it, close it"

```bash
tusker pickup --id <ID> --by <agent>
# ... do work ...
tusker handoff --id <ID> --for verifier
tusker review verify --id <ID> --by <verifier>
tusker review approve --id <ID> --by <reviewer>
tusker attach-evidence --id <ID> --kind pr --path <url>
tusker attest --id <ID> --by <reviewer> --role human
tusker set-status --id <ID> --status done
```

### "Break a spec into stories"

1. Pick the canon location first (see `CANON_LOCATIONS.md`).
2. If you need a canonical developer doc: `tusker new-doc --epic <ACR> --title "<Spec title>" --audience developer --canon-for <ACR>`
3. For each execution slice: `tusker new-story --epic <ACR> ...`
4. Wire `blocks` / `blocked_by` wikilinks in each story's frontmatter.
5. Leave stories with unmet `blocked_by` in `intake`; use `blocked` only after active work stalls on a real blocker.
6. `tusker validate` to confirm the graph.

### "Document a new project as we build"

1. Run `tusker epics`; create a new epic if nothing fits.
2. Pick canon: epic `## Design`, canonical D-note, or repo `spec_source`.
3. Create initial docs:

```bash
tusker new-doc --epic <ACR> --title "<Architecture spec>" \
  --audience developer --canon-for <ACR>
tusker new-doc --epic <ACR> --title "<User guide>" \
  --audience user
```

4. Create implementation stories that cite the canon in `## Canon`.
5. Add companion docs when a story needs explanation that should outlive the task.

### "Publish a doc"

```bash
tusker new-doc --epic <ACR> --title "<Guide>" \
  --audience user --status approved \
  --publish true \
  --publish-path user/guides/<slug> \
  --publish-description "<one sentence>"
tusker docs export --site ./site
tusker docs build --site ./site
```

### "Find current docs canon"

Read these before broad source-doc archaeology:

```bash
site/public/canon-manifest.json
site/public/llms.txt
site/src/generated/content-manifest.json
```

If the manifest says not to cite a path, do not cite it as current truth without explicitly labeling it historical.

### "Validate and regenerate everything"

```bash
tusker reindex
tusker validate
```

`reindex` writes `_system/generated/*.json` (epic/story/bug/doc/links indexes + dashboard). `validate` reads those and the notes themselves.
