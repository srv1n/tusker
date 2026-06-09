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
tusker list
tusker list ORC
tusker list --epic ORC --type task --open
tusker list --epic ORC --type task --open --limit 10
tusker list --ready --format ids
tusker list --running
```

The first command prints a compact epic table with summaries and counts. `tusker list ORC` is the short drill-down for that epic's open tasks; the explicit `--epic ORC --type task --open` form is equivalent. `--ready` is the human-friendly alias for agent-runnable V7 tasks; `--running` reports tasks with active local leases. Human table output uses the detected terminal width and drops low-value columns before wrapping; pass `--width <cols>` when an embedded terminal reports bad dimensions.

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

Use `print` and `open` for human-facing note access:

```bash
tusker print ORC-T-0019
tusker print ORC-T-0019 --acceptance --plain
tusker open ORC-T-0019 --path
tusker open ORC-T-0019 --editor
tusker open ORC-T-0019 --obsidian
```

`print` renders Markdown for terminal reading. `open` resolves the record from
the current vault first, then registered projects from `tusker projects add`
when run outside a repo. `--path` and `--json` report the resolved target
without launching an external app.

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

## Feedback reducer

Use `tusker feedback signals` and `tusker feedback review` when product friction
should come from behavior, not vibes:

```bash
tusker feedback signals --since 2026-05-01 --write
tusker feedback review --since 2026-05-01 --write
tusker feedback promote <signal-id> --write
```

Events are timestamped history. Feedback notes are subjective observations.
Signals are derived product facts stored under
`tusker/feedback/signals/YYYY-MM-DD/*.json`. Daily reviews render facts, likely
causes, proposed actions, and human decisions under `tusker/feedback/reviews/`.
Promotion is deliberately bounded: one signal or review action creates one task,
decision, gate/runbook/skill draft, CLI proposal, or skip record.

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

Use this after pulling or rebuilding Tusker when the repository should carry the current agent skill bundle under `.agents/skills/tusker`. The command also regenerates the Claude Code compatibility install under `.claude/skills/tusker` when used against a repo.

`tusker install` without `--repo`, `make install`, and `tusker update` also refresh already-installed user skill bundles under `~/.agents/skills/tusker`, `~/.codex/skills/tusker`, and `~/.claude/skills/tusker`. Refresh replaces the installed payload directory from the embedded bundle, so stale files disappear instead of hanging around beside the current `SKILL.md`. `tusker install --repo <path>` installs the repo-local `.agents` bundle and generated `.claude` compatibility copy without ambient user-skill refresh; pass `--refresh-existing-user-skills`, `--codex-user`, or `--claude-user` to request user-level writes too.

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

## Automation Control Plane

`tusker.yaml` is the durable automation control plane. `WORKFLOW.md` keeps methodology and prompt text; runtime SQLite keeps ephemeral run state. New automation installs should put trigger states, workspace strategy, concurrency caps, default runner, runner definitions, and fanout policy under `automation`.

```yaml
automation:
  trigger_states: [ready, rework]
  default_runner: codex_app_server
  enabled_runners: [codex_app_server, codex_exec]
  workspace:
    strategy: worktree
  concurrency:
    max_active_runs: 3
    max_active_runs_per_project: 1
    max_concurrent_by_state:
      rework: 1
  runners:
    codex_app_server:
      kind: codex_app_server
      command: codex app-server
    codex_exec:
      kind: codex_exec
      command: codex exec --skip-git-repo-check -
    codex_cloud:
      kind: codex_cloud
      environment_id: env-prod
      apply_mode: manual
      pr_mode: none
  fanout:
    enabled: false
    max_children: 0
    allowed_child_types: []
    merge_rule: manual_review
```

Legacy `active` trigger states are rejected unless `automation.legacy_profile` is explicit. Runner kinds are distinct: `codex_app_server` owns local JSON-RPC app-server semantics, `codex_exec` is one-shot local CLI execution, and `codex_cloud` is remote start/poll/apply/PR orchestration.

The local Obsidian-to-review loop is testable through the operator commands: register the project, move a task to a configured trigger state, run `refresh` or `daemon run`, and inspect the run. The selected runner must still move the task to `review` when it is ready; a clean exit that leaves the task runnable queues continuation rather than pretending the work is reviewable.

## Reviewer close lane

`WORKFLOW.md` can also enable a reviewer lane:

```yaml
reviewer:
  enabled: true
  runner: codex_app_server
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
