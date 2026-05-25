---
schema: "tusker.doc/v5"
id: "reference/cli"
title: "Tusker v5 CLI surface"
type: "doc"
node: "reference/cli"
audience: "developer"
mode: "reference"
agent_layer: "capsule"
kind: "reference"
canonical_status: "draft"
publish: true
publish_lane: "internal"
publish_path: "reference/cli"
publish_description: "Tusker v5 CLI surface."
created: "2026-04-29"
updated: "2026-05-14"
---

# Tusker v5 CLI surface

## Shape

The public CLI is small on purpose:

| Area | Commands |
|---|---|
| Setup | `init`, `update` |
| Work items | `new`, `list`, `search`, `show`, `status`, `evidence`, `verify`, `close` |
| Docs | `docs model`, `docs map`, `docs catalog`, `docs freshness`, `docs check`, `docs apply`, `docs noop`, `docs waive`, `docs export`, `docs dev`, `docs build` |
| Context audit | `context audit` |
| Improvement scans | `improve scan` |
| Shared vaults | `vault set`, `vault status`, `vault mount`, `vault unmount`, `vault repair`, `vault move` |
| Operator runtime | `daemon`, `projects`, `runs`, `refresh` |
| Health | `validate`, `reindex` |

## Bounded tracker lookup

Use `tusker search` before broad shell searches when the question is about existing tracker work:

```bash
tusker search "reviewer lane" --type task
tusker search "docs close gate" --type task --epic DOC --status active
tusker search "cache" --json
```

The command searches first-party task, epic, and doc notes. It skips attachments, generated indexes, runtime state, and raw logs, so it is safe as the default duplicate-check and status lookup path for agents.

Use `tusker list` as the progressive index:

```bash
tusker list --type epic
tusker list --epic ORC --type task --open
tusker list --epic ORC --type task --open --limit 10
```

The first command prints the short epic roster with summaries and counts. The second drills into one epic's open tasks without reading note bodies.

Use `tusker show` before opening a note:

```bash
tusker show ORC-T-0019
tusker show ORC-T-0019 --acceptance
tusker show ORC-T-0019 --evidence
```

`show` defaults to the agent capsule. `--verification` shows verification
frontmatter plus a small log tail; use `--section "Verification log"` only when
the full log is needed. `--full` is available, but it should be a deliberate
drill-down.

Use `compact` to trim old notes before they become model context:

```bash
tusker compact ORC-T-0019
tusker compact ORC-T-0019 --write
tusker compact --all --json
```

`compact` dry-runs by default. It removes empty optional frontmatter and
disposable placeholder sections such as empty `Execution plan` and creation-only
`Work log`; substantive decisions and evidence are preserved.

Use `context audit` to inspect Codex JSONL without dumping the transcript:

```bash
tusker context audit --file ~/.codex/sessions/2026/05/09/session.jsonl
tusker context audit --file ./thread.jsonl --top 20 --json
```

It reports top output categories, largest tool outputs, token totals, and
context-reduction recommendations. This is the default path for token-burn
forensics.

## Improvement scans

Use `tusker improve scan` when you want Tusker to look across recent work and
find repeated manual workflows worth packaging:

```bash
tusker improve scan
tusker improve scan --since 2026-05-01 --write
tusker improve scan --apply --profile cheap-discovery --runner codex --model gpt-5.3-codex-spark
```

The command is opt-in. Dry-run is the default, scoped to the last 30 days or all
available history if the vault is newer. Evidence is Tusker-first: task
summaries, attempt summaries, proof/verification text, feedback notes, and the
existing skill/doc/subagent/automation inventory.

External sources are off by default:

| Source | Opt-in |
|---|---|
| Codex sessions | `--include-codex` or `--codex-session <path>` |
| Claude Code transcripts | `--include-claude` or `--claude-transcript <path>` |
| Memories | `--include-memories` or `--memories-path <path>` |
| Chronicle | `--include-chronicle` or `--chronicle-path <path>` |

The shortlist reports repeated workflow, evidence dates, frequency/confidence,
recommended form, and rationale. `--apply` intentionally has a narrow first
slice: it creates missing agent runbook drafts under `tusker/docs/agents/` for
high-confidence skill/playbook candidates. It reports custom subagents and
automations for human/provider-specific follow-up instead of silently creating
them.

## Operator/runtime commands

The runtime commands are shipped as operator/internal controls. They support the local runner pickup loop while keeping task truth in markdown.

| Command | Behavior |
|---|---|
| `tusker projects add --repo . --vault ./tusker` | Register a repo-local vault for daemon polling. |
| `tusker projects list [--json]` | Show registered projects and health. |
| `tusker daemon status [--json]` | Show state root, project count, and active run count. |
| `tusker daemon run [--once]` | Run the daemon loop, or a single poll tick. |
| `tusker refresh [--quiet] [--json]` | Run one poll tick; best smoke command. |
| `tusker runs inspect <TASK-ID> [--json]` | Inspect run, attempts, turns, sessions, latest event, decisions, token totals, and artifact paths. |
| `tusker runs events <TASK-ID> [--lines <n>] [--json]` | Tail normalized runtime events. |
| `tusker runs logs <TASK-ID> [--lines <n>] [--json]` | Tail raw runner logs. |
| `tusker runs interrupt <TASK-ID> [--json]` | Interrupt a live run. |

These stay internal/operator commands. The public workflow still leads with task commands, not daemon mechanics.

## Existing repo migration

```bash
tusker init --migrate-v5 --dry-run --vault ./tusker
tusker init --migrate-v5 --yes --vault-only --no-mount --vault ./tusker
```

`--migrate-v5` converts old stories and bugs into tasks, updates IDs and wikilinks, renames epic index files, installs V5 templates/views, and adds missing docs-map nodes for published docs.

## Repo-local skill refresh

```bash
tusker update --repo . --repo-only --no-bin
```

Use this after pulling or rebuilding Tusker when the repository should carry the current agent skill bundle under `.agents/skills/tusker` and `.claude/skills/tusker`.

`tusker install` without `--repo`, `make install`, and `tusker update` also refresh already-installed user skill bundles under `~/.agents/skills/tusker`, `~/.codex/skills/tusker`, and `~/.claude/skills/tusker`. Refresh replaces the installed payload directory from the embedded bundle, so stale files disappear instead of hanging around beside the current `SKILL.md`. `tusker install --repo <path>` installs repo-local skill bundles without ambient user-skill refresh; pass `--refresh-existing-user-skills`, `--codex-user`, or `--claude-user` to request user-level writes too.

## Docs pipeline

```bash
tusker docs map --json
tusker docs freshness --stale
tusker docs export --vault ./tusker --site ./site
tusker docs build --vault ./tusker --site ./site --quiet
```

The site output is generated. Author source docs in `tusker/docs/**` or registered repo docs, not in `site/src/content/docs/**`.
For agent runs, prefer `docs build --quiet` or `--json`; successful Astro route
output is suppressed, while failures include the final non-empty log tail.

## Codex-first orchestration status

`WORKFLOW.md` currently names `codex` as the default runner and `codex app-server` as the Codex command. That is the first production path, not a permanent product boundary. The tracker and reviewer policies are runner-neutral: future adapters such as `claude-code` or `opencode` should plug into the same task lifecycle, runtime lanes, and close gates.

The local Obsidian-to-review loop is now testable through the operator commands: register the project, move a task to `active` or `rework`, run `refresh` or `daemon run`, and inspect the run. The selected runner must still move the task to `review` when it is ready; a clean exit that leaves the task `active`/`rework` queues continuation rather than pretending the work is reviewable.

## Reviewer close lane

`WORKFLOW.md` can also enable a reviewer lane:

```yaml
reviewer:
  enabled: true
  runner: codex # current default; use another enabled runner when its adapter is ready
  actor: agent-reviewer
  auto_close_risks: [low, medium]
  human_required_risks: [high, critical]
```

When enabled, a task in `review` can be picked up by an independent reviewer run. The reviewer prompt tells the agent to inspect the task, evidence, diff, tests, docs impact, and caveats. If the task fails review, it should send the task to `rework`:

```bash
tusker status <TASK-ID> rework --by agent-reviewer --reason "<specific unmet acceptance item>"
```

For low/medium tasks, the reviewer may close after the normal gates pass:

```bash
tusker docs check <TASK-ID>
tusker verify <TASK-ID> --by agent-reviewer --summary "<what was checked>"
tusker close <TASK-ID> --by agent-reviewer --reason "agent review accepted"
```

For high/critical tasks, the configured reviewer is blocked from `verify` and `close`; it can produce advisory evidence, but a human must verify and close. Closed task output and frontmatter summaries include both reviewer and closer attribution through `verified_by` and `closed_by`.

## Shared Obsidian vault

```bash
tusker vault set --path /path/to/shared-obsidian-vault
tusker vault mount --repo /path/to/repo --vault /path/to/repo/tusker --name repo-name
tusker vault status
```

`vault mount` creates a symlink at `<shared-vault>/<name>` that points to the repo-local Tusker tracker. Use this when one Obsidian workspace should monitor multiple project trackers.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | user input or command error |
| 2 | validation failure |
| 3 | filesystem or I/O failure |
