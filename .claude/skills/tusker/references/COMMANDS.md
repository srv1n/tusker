# Commands

The public V5 CLI is intentionally small. Use these commands.

## Discovery

Most commands accept `[--vault <path>]`. When omitted, `tusker` walks up from the current directory looking for `tusker/` or a vault-shaped directory.

## Core Surface

```bash
tusker init [--vault <path>] [--yes] [--fresh]
tusker init --migrate-v5 [--vault <path>] [--yes] [--vault-only] [--dry-run]
tusker new epic --acronym <ACR> --title <title> [--summary "..."]
tusker new task --epic <ACR> --title <title> \
  [--kind feature|bug|refactor|migration|security|docs|chore|research|incident] \
  [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] \
  [--domains "cli,docs"] [--doc-nodes "reference/commands"]
tusker new bug --epic <ACR> --title <title> [--priority p0|p1|p2|p3]
tusker new doc --title <title> --node <domain/slug> \
  [--kind reference|canon|guide|runbook|release|support] \
  [--audience developer|user|operator|support|release|agent|internal] \
  [--mode tutorial|how-to|reference|explanation] \
  [--agent-layer none|capsule|standalone]
tusker list [--type epic|task|doc] [--epic <ACR>] [--status <status>] [--open|--closed] [--limit <n>]
tusker search <text> [--type epic|task|doc] [--epic <ACR>] [--status <status>] [--limit <n>] [--json]
tusker show <ID> [--capsule|--acceptance|--evidence|--verification|--full]
tusker compact <ID|--all> [--write] [--json] [--verbose]
tusker next [--epic <ACR>] [--owner <name>] [--json]
tusker claim <ID> --as <agent-or-person> [--reason "..."]
tusker status <ID> <draft|backlog|ready|blocked|active|review|rework|done|cancelled> [--by <actor>] [--reason "..."]
tusker evidence <ID> <screenshot|video|log|bench|pr|packet> <file-or-url> [--note "..."]
tusker docs model [--json]
tusker docs map [<DOC-NODE>] [--json]
tusker docs catalog [--json]
tusker docs freshness [--stale] [--json]
tusker docs check <TASK-ID>
tusker docs apply <TASK-ID> --node <DOC-NODE> [--reason "<what changed>"]
tusker docs noop <TASK-ID> --node <DOC-NODE> [--reason "<why already current>"]
tusker docs waive <TASK-ID> <DOC-NODE> --reason "<why no doc change>"
tusker docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
tusker docs dev [--vault <path>] [--site <path>] [--watch] [--port <n>] [--host <host>]
tusker docs build [--vault <path>] [--site <path>] [--public-only] [--quiet] [--json]
tusker vault set --path <obsidian-vault>
tusker vault status [--json]
tusker vault mount [--repo <path>] [--vault <path>] [--name <folder>] [--force] [--json]
tusker vault repair [--force] [--json]
tusker verify <ID> --by <verifier> [--summary "..."]
tusker close <ID> --by <reviewer> [--reason "..."]
tusker validate [--vault <path>] [--json]
tusker reindex [--vault <path>] [--json]
tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--json]
tusker context audit --file <codex-session.jsonl> [--top <n>] [--json]
```

## Operator Runtime Surface

The daemon/runtime commands are shipped as operator/internal controls. Keep the public workflow task-centric, but use these when running the local runner pickup loop. Codex is the default live runner today; future runner adapters should use the same task lifecycle.

`WORKFLOW.md` also carries a runner-neutral reviewer policy. When `reviewer.enabled` is true, `review` tasks may be dispatched to `reviewer.runner`; low/medium risks can be closed by `reviewer.actor`, while high/critical risks stay human-gated.

| Command | Meaning |
|---|---|---|
| `tusker projects add --repo . --vault ./tusker` | Register this repo/vault for daemon polling. |
| `tusker projects list [--json]` | List registered projects and health. |
| `tusker daemon status [--json]` | Show state root, registered project count, and active run count. |
| `tusker daemon run [--once]` | Run the polling loop, or one poll tick with `--once`. |
| `tusker refresh [--quiet] [--json]` | Run one daemon poll tick; best local smoke command. |
| `tusker runs inspect <TASK-ID> [--json]` | Inspect run, attempts, turns, sessions, latest event, decisions, token totals, and artifact paths. |
| `tusker runs events <TASK-ID> [--lines <n>] [--json]` | Tail normalized event JSONL. |
| `tusker runs logs <TASK-ID> [--lines <n>] [--json]` | Tail raw runner logs. |
| `tusker runs interrupt <TASK-ID> [--json]` | Interrupt a live run and record the stop. |

The normal V5 workflow remains task-centric: create/claim/status/evidence/docs/verify/close. Runtime commands are the control plane, not the source of task truth.

## Common Flows

### Log One Task

```bash
tusker list --type epic
tusker search "<duplicate clue>" --type task
tusker list --epic <ACR> --type task --open
tusker new task --epic <ACR> --title "<what>" \
  --kind chore --size s --risk low --priority p2 \
  --domains cli
```

New tasks should start as `draft` until shaped. Move shaped future work to `backlog`; move shaped current work to `ready`. Only pull `ready` tasks that are unblocked.

Use `tusker search` for tracker lookup before shelling out to `rg`. It searches
first-party task/epic/doc notes and skips `Attachments/**`, `_system/**`,
runtime state, and generated indexes.

Use `tusker list --type epic` as the top-level index. It prints epic summaries
and open/done counts without note bodies. Use
`tusker list --epic <ACR> --type task --open` to drill into one epic's open
tasks. Open a task file only after choosing the task.
Add `--limit <n>` when an epic has a large queue.

Use `tusker show <ID>` for the selected item before opening the markdown file.
It defaults to `--capsule`; use `--acceptance`, `--evidence`, or
`--verification` for a bounded summary. `--verification` shows verification
frontmatter plus a small log tail; use `--section "Verification log"` only when
the full log is needed. `--full` is an explicit escape hatch.

Use `tusker compact <ID>` when a note looks noisy. It dry-runs by default and
reports removable empty optional frontmatter plus disposable placeholder
sections such as empty `Execution plan` or creation-only `Work log`. Add
`--write` only after checking that the reported removals are scaffolding, not
durable decisions.

Use `tusker context audit --file <jsonl>` for Codex transcript analysis. Do not
inspect large JSONL files with `head`, `tail`, broad `rg -n`, or ad hoc scripts
unless the bounded audit output is insufficient.

### Pick Work

```bash
tusker next
tusker claim <ID> --as codex
tusker next --claim --as codex
```

`next` only returns `ready` or `rework` tasks with no unresolved `blocked_by` dependencies. `claim` assigns the task and moves it to `active`.

### Work And Close

```bash
tusker claim <ID> --as <name>
# work happens
tusker evidence <ID> pr <url>
tusker docs check <ID>
tusker status <ID> review
tusker verify <ID> --by <verifier>
tusker close <ID> --by <reviewer>
tusker validate
```

For the current orchestration milestone, `active`/`rework` are the runnable tracker states in `WORKFLOW.md`. `ready` is still only shaped work. Flipping a task to `active` is picked up only when the repo/vault is registered and `daemon run`, `daemon run --once`, or `refresh` is running locally.

When the reviewer lane is enabled, moving a task to `review` can trigger an independent review attempt. Passing low/medium work may be verified and closed by `reviewer.actor`; passing high/critical work stays in `review` for human close.

### Publish Docs

Authoring sources:

- task execution records: `tusker/epics/<ACR>/<ACR>-T-NNNN.md`
- durable docs: `tusker/docs/**`
- repo-native docs registered through `docs/publication.yaml`

Inspection commands:

| Command | Meaning |
|---|---|
| `tusker docs model` | Explain the docs philosophy, Diátaxis modes, agent layers, and close gate. |
| `tusker docs map` | List controlled domains and doc nodes from `_config/docs-map.yaml`. |
| `tusker docs map <DOC-NODE>` | Inspect the page, domain, mode, audience, source-of-truth, and stale triggers for one node. |
| `tusker docs catalog` | Show the generated reader-facing catalog from `Docs.md` / `docs.index.json`. |
| `tusker docs freshness` | Show freshness, linked tasks, last verified event, waivers, and stale triggers. |
| `tusker docs freshness --stale` | Show only docs needing attention. |
| `tusker docs noop <TASK-ID> --node <DOC-NODE>` | Record that a target doc was checked and already current. |

Generated output:

- `site/src/content/docs/**`
- `site/src/generated/content-manifest.json`
- `site/src/generated/canon-manifest.json`
- `site/public/canon-manifest.json`
- `site/public/llms.txt`
- `site/public/llms-full.txt`

Do not author in `site/src/content/docs/**`. `tusker docs export` is the compiler.

### Shared Obsidian Vault

```bash
tusker vault set --path /path/to/shared-obsidian-vault
tusker vault mount --repo /path/to/repo --vault /path/to/repo/tusker --name repo-name
tusker vault status
```

`vault mount` creates a symlink at `<shared-vault>/<name>` that points to the repo-local Tusker tracker. Use this when one Obsidian workspace should monitor multiple project trackers.

### Repair Existing Repo

Use this when a repo already has an older Tusker vault:

```bash
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
tusker validate --vault ./tusker
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site --quiet
tusker update --repo . --repo-only --no-bin
```

Prefer `tusker docs build --quiet` or `--json` in agent runs. Full Astro output
is useful for humans watching a terminal, but it is wasted model context on
successful builds.

`--migrate-v5` converts legacy stories and bugs into tasks, renames epic `index.md` files to `<ACR>.md`, rewrites wikilinks, refreshes V5 templates/views, and fills docs-map nodes for published docs. `--repo-only` refreshes `.agents/skills/tusker` and `.claude/skills/tusker` without touching user-level installs or PATH.

## Exit Codes

- `0` success
- `1` user error
- `2` validation failure
- `3` filesystem or I/O error
