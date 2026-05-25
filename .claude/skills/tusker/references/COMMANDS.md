# Commands

Most commands accept `[--vault <path>]`. Omit it unless discovery fails.

## Core V7 surface

```bash
tusker init [--vault <path>] [--yes] [--fresh]
tusker new epic --acronym <ACR> --title <title> [--summary "..."]
tusker new task --epic <ACR> --title <title> \
  [--kind feature|bug|refactor|migration|security|docs|chore|research|incident] \
  [--priority p0|p1|p2|p3] [--size s|m|l|xl] [--risk low|medium|high|critical] \
  [--domains "cli,runtime"] [--proof-mode inline|card|artifact|audit] [--proof-required focused_test,broad_test]
tusker new gate --blocks <TASK-ID> --kind <gate-kind> --owner <owner> --action "<needed action>" --verification "<proof>" [--covers A1,A2]
tusker new decision --epic <ACR> --title <title>

tusker list [--type epic|task|gate] [--epic <ACR>] [--status <status>] [--open|--closed] [--limit <n>]
tusker search <text> [--type epic|task|gate|evidence] [--epic <ACR>] [--status <status>] [--limit <n>] [--json]
tusker show <ID> [--capsule|--acceptance|--evidence|--verification|--full]
tusker compact <ID|--all> [--write] [--json] [--verbose]
tusker next [--epic <ACR>] [--owner <name>] [--json]

tusker claim <ID> --as <agent-or-person> [--reason "..."]
tusker status <ID> <ready|review|rework|cancelled|superseded> [--by <actor>] [--reason "..."]
tusker propose status <TASK-ID> --status review [--reason "..."]
tusker propose create_gate <TASK-ID> --kind <gate-kind> --owner <owner> --action "<needed action>" --verification "<proof>"

tusker verify add <TASK-ID> --covers A1,A2 --check "<command-or-manual-check>" --result pass [--note "..."]
tusker proof status <TASK-ID> [--json]
tusker proof set-mode <TASK-ID> inline|card|artifact|audit [--required focused_test,broad_test]
tusker evidence add <TASK-ID> --kind <evidence-kind> --covers A1 [--summary "..."] [--command "..."]
tusker evidence promote <TASK-ID> --from .tusker/scratch/<TASK-ID>/<artifact> --kind video --covers A1-A3
tusker evidence prune <TASK-ID> --dry-run

tusker finish <TASK-ID> [--summary <file-or-text>] [--request-review]
tusker attempt handoff <TASK-ID> [--summary "..."]
tusker close <ID> --by <reviewer> [--reason "..."]
tusker validate [--vault <path>] [--json]
tusker reindex [--vault <path>] [--json]
```

## Closeout and loop prevention

Use these when available. Older CLIs may not have them; if unsupported, use the fallback below once.

```bash
tusker closeout status <TASK-ID> --json
tusker closeout <TASK-ID> --emit-packet --validate "<command>" [--json]
tusker validate --reuse-clean --json
```

Fallback:

```bash
tusker proof status <TASK-ID> --json
tusker show <TASK-ID> --capsule
tusker search <TASK-ID> --type gate --status open --json
```

Do not repeatedly call unsupported commands. Do not revalidate unchanged human-wait states.

## Runtime/operator surface

Use these when running the local runner pickup loop. Runtime commands are the control plane, not the source of task truth.

```bash
tusker projects add --repo . --vault ./tusker
tusker projects list [--json]
tusker daemon status [--json]
tusker daemon run [--once]
tusker refresh [--quiet] [--json]
tusker runs inspect <TASK-ID> [--json]
tusker runs events <TASK-ID> [--lines <n>] [--json]
tusker runs logs <TASK-ID> [--lines <n>] [--json]
tusker runs interrupt <TASK-ID> [--json]
```

The daemon should dispatch only agent-owned work:

```text
status in ready|rework
readiness == ready
next_owner == agent or agent:<name>
no valid human-wait closeout checkpoint
```

## Docs and project knowledge

Docs commands may live under `legacy docs` in older repositories.

```bash
tusker legacy docs model [--json]
tusker legacy docs map [<DOC-NODE>] [--json]
tusker legacy docs catalog [--json]
tusker legacy docs freshness [--stale] [--json]
tusker legacy docs check <TASK-ID>
tusker legacy docs apply <TASK-ID> --node <DOC-NODE> [--reason "<what changed>"]
tusker legacy docs noop <TASK-ID> --node <DOC-NODE> [--reason "<why already current>"]
tusker legacy docs waive <TASK-ID> <DOC-NODE> --reason "<why no doc change>"
tusker legacy docs export [--vault <path>] [--site <path>] [--clean] [--public-only] [--json]
tusker legacy docs build [--vault <path>] [--site <path>] [--public-only] [--quiet] [--json]
```

Prefer `--quiet` or `--json` for agent runs.

## Vault/install/update

```bash
tusker vault status [--json]
tusker vault mount [--repo <path>] [--vault <path>] [--name <folder>] [--force] [--json]
tusker vault repair [--force] [--json]
tusker install [--bin-dir <path>] [--no-bin] [--codex-user] [--claude-user] [--repo <path>] [--refresh-existing-user-skills] [--json]
tusker update [--bin-dir <path>] [--no-bin] [--repo <path>] [--repo-only] [--json]
tusker context audit --file <session.jsonl> [--top <n>] [--json]
tusker improve scan [--days 30|--since <YYYY-MM-DD>|--all] [--write]
tusker improve scan --apply [--runner <name>] [--model <name>] [--reasoning low|medium|high]
```

## Improvement scans

Use `tusker improve scan` only when the user asks to mine repeated work or when
you are deliberately maintaining the repo's reusable agent assets. It is not
part of every task closeout.

- Default mode is dry-run over the last 30 days.
- `--write` stores the report under `tusker/feedback/improvements/`.
- `--apply` is the opt-in mutation path and creates only high-confidence missing
  agent runbook drafts under `tusker/docs/agents/`.
- Provider history is disabled unless explicitly enabled with flags such as
  `--include-codex`, `--include-claude`, `--include-memories`, or
  `--include-chronicle`.
- Runner/model/reasoning options describe the chosen runtime profile in the
  report; they are not durable task truth.

## Common flows

### Find or log work

```bash
tusker list --type epic
tusker search "<duplicate clue>" --type task
tusker list --epic <ACR> --type task --open
tusker new task --epic <ACR> --title "<what>" --kind chore --size s --risk low --priority p2 --domains cli
```

### Finish implementation work

```bash
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID> --json
# make the smallest change and proof update
tusker finish <TASK-ID> --request-review --summary "<what changed and where proof lives>"
tusker validate --json
```

### Stop for human gate

```bash
tusker proof status <TASK-ID> --json
tusker search <TASK-ID> --type gate --status open --json
# if only human/external gaps remain, emit/update packet once and stop
```

## Exit codes

- `0` success
- `1` user error
- `2` validation failure
- `3` filesystem or I/O error
