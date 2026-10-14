---
title: "CLI reference"
subject: cli
part_of: overview
status: canonical
read_when: "Looking up an exact command, document route, or machine-readable output."
skip_when: "You need the rationale or lifecycle rule behind a command."
describes:
  - cmd/tusker/commands_table.go
  - cmd/tusker/cli.go
  - cmd/tusker/docs_cmd.go
  - cmd/tusker/docs_touch_cmd.go
  - cmd/tusker/lifecycle_cmd.go
  - cmd/tusker/author_cmd.go
  - cmd/tusker/show_list_cmd.go
  - cmd/tusker/check_cmd.go
  - cmd/tusker/operator_commands.go
---
<!-- docs-check: ignore-path docs/system/decisions/ -->

# CLI reference

The installed program help is the command authority. It shows the commands
that this build can run. A machine-readable command list is also available.

One command table (`cmd/tusker/commands_table.go`) drives dispatch, help,
`version --json`, and daemon notification. The surface is deliberately small:
a new capability becomes a flag or a routed case on an existing verb, not a
new command. Each row carries a summary, a "Use when" line, and a "Not for"
line naming the neighbouring command.

| Need | Command |
| --- | --- |
| Find a command by word | `tusker help \| grep <word>` |
| Search use/avoid guidance | `tusker help --full \| grep -B3 <word>` |
| Read one command | `tusker help <command>` or `tusker <command> --help` |
| Include hidden plumbing | `tusker help --all` |

A failing command prints `help: tusker <command> --help`. A removed command
name fails with the replacement command in its hint.

## Working surface

| Intent | Command |
| --- | --- |
| Inspect one record (task, wave, batch, execution, message, escalation, doc) | `tusker show <ID>` |
| Diagnose why work is not moving (exit 0 healthy, 1 fault, 2 unavailable) | `tusker show <ID> --why` |
| Worker or reviewer packet | `tusker show <TASK-ID> --packet [agent\|worker\|reviewer]` |
| Proof coverage, progress, lease, route, closeout | `tusker show <TASK-ID> --proof\|--progress\|--lease\|--route [lane]\|--closeout` |
| A run's attempts, logs, events, traces | `tusker show <TASK-ID> --run\|--logs [--follow]\|--events [--follow]\|--traces` |
| Whether a wave can start | `tusker show <WAVE-ID> --check` |
| List or search records | `tusker list [tasks\|epics\|waves\|docs\|runs\|executions\|messages\|approvals\|forks\|flaky\|projects\|queue\|escalations] [--grep <text>]` |
| Next pickable task | `tusker list --next` |
| Create a record | `tusker new epic\|task\|gate\|decision\|wave\|batch\|fork\|flaky\|doc ...` |
| Change a record | `tusker edit <ID> ...` |
| Begin work | `tusker start <TASK-ID\|WAVE-ID\|batch> --by <actor> [--background]` |
| Record finished work | `tusker done <TASK-ID>... \| --wave <WAVE-ID> \| --since <git-ref> --by <actor>` |
| Review | `tusker review <TASK-ID> [--verdict ...]` |
| Stop without finishing | `tusker stop <TASK-ID\|WAVE-ID\|batch> [--failed\|--cancel] --reason <text>` |
| Health and repair | `tusker check [--only setup,validate,skills,docs,acp] [--fix] [--strict]` |

Direct authoring is canonical: `tusker new task ... --body-file` writes one
task whose body is the implementation contract, and `tusker new wave
--file <request.yaml> --request-key <key>` atomically authors a complete
`tusker.wave-authoring/v1` request. `--epic` and `--spec-refs` are optional;
every supplied spec ref must resolve. Creation is inert; execution begins only
at `tusker start`.

`tusker edit` routes on the record. A task takes contract flags (`--title`,
`--body-file`, `--owned-paths`, `--dependencies`, `--work-level`, ...), planned
checks (`--verify "A1|command: <cmd>|pending|note"` or `--covers A1 --check
"command: <cmd>"`), `--verify-remove <row>`, `--proof-mode <mode>`, and
`--status <state>`; content changes apply before the status change. A wave
takes `--add`, `--remove`, and `--outcome`. A gate takes `--status
satisfied --evidence <summary>` or `--status waived|obsolete --reason <text>`.

`tusker start` on a task claims it for your own session; rerunning renews the
lease, and a task whose last attempt ended gets a new attempt. `--background`
leaves a durable run directive for the configured runtime; it does not itself
launch a runner. `tusker start <WAVE-ID>` durably authorizes the exact current
wave material; daemon polling then releases each dependency frontier
automatically. `tusker stop <WAVE-ID>` blocks new wave-owned admissions while
admitted attempts finish, and starting it again restores the same
authorization, refusing when the material drifted. A task start inside a
paused wave stays task-scoped and leaves the wave paused. Any other ID is a
coordinator batch: start runs it in the foreground, stop halts it (`--drain`
lets workers finish).

`tusker done` submits the caller's live work session. Without one, it reruns
each selected task's command proof in the checkout (see
[Proof and closeout](proof-and-closeout.md)); an agent lands tasks in review
and a person closes them. A person running `done` on a task already in review
closes it.

Actors supplied with `--by human:<name>` may be used from an agent session by default. Set `agents.act_as_owner: false` in the project's `.tusker/config.yaml` (or `config.local.yaml`) to require an owner session for human attribution; agent sessions can still use `--by agent:<name>`.

## Operator commands

| Need | Command |
| --- | --- |
| Add or move a project | `tusker projects add --repo . --vault ./.tusker` / `tusker projects add --rebind <PROJECT-ID> ...` |
| Turn background work on or off, cap runs | `tusker projects set <PROJECT-ID> --on\|--off\|--limit <n> [--dry-run]` |
| Remove a project | `tusker projects remove <PROJECT-ID>\|--missing` |
| Global automation switch | `tusker automation [on\|off]` |
| Daemon status, cap, circuit reset | `tusker daemon [--limit <n>] [--resume]` |
| One poll without dispatch | `tusker daemon run --once` |
| launchd service | `tusker daemon install [--remove]` |
| Steer a daemon run | `tusker runs say\|restart [--fresh\|--requeue]\|stop [--soft]\|retire [--release] <TASK-ID>` |
| Decide one access request | `tusker approvals respond <id> --allow-once\|--block [--by human:<actor>]` |
| Messages | `tusker message ask [--notify\|--severity P0-P2]`, `tusker message reply`, `tusker message mark <ID> --consumed\|--applied` |
| Start the local service | `tusker serve` |

`tusker list waves` prints one line per wave with ID, title, status, effective
authorization, total task count, and done count. `tusker list runs` reads the
runtime database, newest start first (default limit 100); `--active` selects
claimed and running leases.

## Executions

`tusker execution register` records a direct execution.
`tusker list executions --unbound` lists unbound executions. Use `tusker show <EXECUTION-ID>`,
`tusker execution bind <EXECUTION-ID> <TASK-ID>|--none`, and `tusker execution cancel` for one execution.
These commands do not grant a task claim.

Register an interactive architect after obtaining the session's native ID.
For Claude Code, its hook input's `session_id` is the documented ID; a
temporary local `SessionStart` hook can print it, or the session's shell can
read `CLAUDE_CODE_SESSION_ID` where available. For Codex, the documented
hook input also contains `session_id`; reading it with a temporary local hook
is the lowest effort published method, but this path is **unverified in the
installed Codex**. The connection ID is the operator's stable host label.

```bash
tusker execution register --project <project-id> --wave <wave-id> --contact-role architect --harness claude-code --source direct_claude --provider anthropic --conversation-id <session-id> --connection-id <host-label> --if-generation 0 --by operator:<name> --json
```

Use `--task <task-id>` in place of `--wave` for a task contact. For Codex,
use `--harness codex --source direct_codex --provider openai`. To replace a
registration, pass its current generation via `--if-generation` and the new
session ID. The returned execution ID is the architect's sender address.
Install the operator owned inbox hooks in [Orchestration](orchestration.md).
After receiving a message with its ID, answer from that session:

```bash
tusker message reply --project <project-id> --reply-to <message-id> --sender execution:<returned-execution-id> --key <unique-key> --body-file -
```

## Repeatable demo

`tusker demo` seeds and drives a disposable deterministic project (one
standalone smoke task plus three waves, thirteen tasks) entirely through the
native CLI. No daemon, GUI, or provider is required for the offline lane.

| Need | Command |
| --- | --- |
| Seed the fixture (inert, idempotent) | `tusker demo seed --repo <empty-dir> --scenario parallel-waves --json` |
| Inspect waves, tasks, blockers | `tusker demo status --repo <dir> --json` |
| Run named waves with the timer executor | `tusker demo run --repo <dir> --waves alpha,beta [--fast] --json` |
| Run with configured profiles (real lane) | `tusker demo run --repo <dir> --waves standalone --mode real --require-harness <name> [--profile <name>] [--timeout 15m] --json` |
| Wait for terminal state | `tusker demo wait --repo <dir> --until terminal --timeout 120s --json` |
| Assert named scenario invariants | `tusker demo check --repo <dir> --json` |
| Preview or apply a guarded reset | `tusker demo reset --repo <dir> [--dry-run] [--yes] --json` |

Seed authors tasks and waves directly and leaves every wave unstarted. The
offline lane authorizes only the named waves, executes fixed delays with
exact fixture bytes, promotes evidence while each run is active, checks
results deterministically, and closes under reviewer authority. The real
lane (`--mode real`) instead resolves each task through `tusker runner
`tusker show <TASK-ID> --route`, requires the named harness/profile with no silent substitution, and
waits for the configured runtime to perform the real file, test, submit,
review, and close work; attempt IDs and profiles are recorded from runtime
inspection. Variants:
`--fail-once <task>` (branch fails once, join stays blocked, retry succeeds),
`--reject-once <task>` (reviewer rejects, correction resubmits),
`--with-human-gate` at seed (a human-owned signoff gate blocks completion
until a native human confirmation releases it; the CLI never bypasses it),
and `--require-harness <name>` (unmet precondition, no silent substitution).
Exit codes: 0 success, 2 invalid request, 3 unmet precondition, 4 failed
assertion or partial wave, 5 wait timeout, 1 internal error.

## Models

| Need | Command |
| --- | --- |
| Discover installed models and reasoning choices | `tusker runner catalog --json` |
| Read effective mappings with only referenced profiles | `tusker models --json --compact` |
| Read every profile for administration | `tusker models --json` |
| Create or update a profile (global config only) | `tusker models profile <stable-id> --display-name <name> --eligible-tiers light,standard --harness <harness> --model <id> [--effort <effort>] --preset <preset> [--if-revision <sha256>]` |
| Disable or enable future use | `tusker models profile <name> --disable\|--enable [--if-revision <sha256>]` |
| Remove an unreferenced profile | `tusker models profile <name> --remove [--if-revision <sha256>]` |
| Set an ordered primary/fallback list | `tusker models set <level> execute\|review primary,fallback [--scope global\|project] [--if-revision <sha256>]` |
| Reset a project field to inheritance | `tusker models set <level> execute\|review --clear [--if-revision <sha256>]` |
| Author task-level choices | `tusker new task ... --work-level standard --review-level demanding --review-reason "Security-sensitive review"` |
| Explain one task's effective route | `tusker show <TASK-ID> --route execute\|review --json` |

Runner profiles are defined only in the global config
(`~/.config/tusker/config.yaml`, or `$TUSKER_CONFIG`) plus built-ins. A project's
`.tusker/config.yaml` and `config.local.yaml` may only select them: tier
mappings (`automation.model_levels`), `automation.default_profile`, routing, and
task `runner_profile`/`execute_profile`/`review_profile`. A project layer that
contains `automation.profiles` or `automation.removed_profiles` fails validation
with an error naming the file, the profiles, and the global path; a project
reference to an unknown profile fails naming the missing profile.
`tusker models profile --defaults --write` adds generated `codex_exec` profiles to the
global config (never Terra, never the project) and proposes nothing once enabled
global profiles already cover every tier.

Writes are atomic. Omitting `--if-revision` uses the current revision; passing it
enforces compare-and-swap. Writes return the new revision in text or JSON. Catalog
entries carry their installed-harness provenance and supported reasoning values.
Manual profile values remain configured even when discovery is unsupported; the
first live preflight decides whether the route is actually available.

Use `tusker runner test <profile> --json` for a no-spend setup check and append
`--live` only to authorize one disposable, two-minute model turn. The report records
the exact selected profile/model/effort/transport when known; a blocked setup reports
an actionable next step without exposing credentials.

Disable preserves configuration and history but blocks new selection; an explicit
ordered fallback may still be selected after a disabled primary. Remove first
reports/refuses live level, lane, routing, default, or unstarted task references.
Historical run snapshots do not block removal. All lifecycle writes require the
current revision and serialize the reference check with the config write.

`eligible_tiers` on the profile is the authoritative membership list. A profile
may belong to multiple tiers or none. Tier worker/reviewer arrays are assignments:
their first entry is primary and later entries are explicit fallbacks. Assignments
accept only enabled eligible profiles; changing membership never rewrites them.
Profiles saved before this field existed derive membership from their current tier
references until their next guarded save. The profile map key is its stable ID;
`display_name` is editable, and omitted reasoning remains distinct from a named
effort.

For a direct native Muse profile, declare the route and access contract explicitly
in the global config's `automation.profiles`, for example:

```yaml
profiles:
  muse-direct:
    display_name: Muse direct
    harness: muse
    model: <installed-model-id>
    access:
      schema: tusker.agent-access/v1
      mode: work_in_projects
      network: true
      destructive_actions: ask
      folders: []
      private_folders: []
```

The runner resolves this contract before `muse exec --json`, records the resolved
fingerprint and execution identity, and blocks required native controls that the
installed route cannot represent. A no-spend setup check or provider-free fixture
can establish route and schema behavior; installed credentials and paid/live model
turns require a separate explicit check with `tusker runner test <profile> --live`.

Agent callers should read once, retain `revision`, make one guarded write, then
use the returned document as the new state. A stale write fails with an
actionable refresh error. Reuse `tusker show <TASK-ID> --route`; there is no duplicate
model-resolution command or client library to learn. Prefer `--compact` for
routine routing so unused profiles do not consume context.

## Documentation and skills

- `tusker list docs --grep <query>` searches the managed corpus: current chapters
  in `docs/system/`, change specifications in `docs/system/proposals/`, and
  product decisions in `docs/system/decisions/` (legacy `.tusker/specs/`
  paths still resolve during transition).
- Search returns a bounded shortlist with `read_when` and `skip_when` guidance.
  JSON also reports `total_matches` and `truncated`; open the returned subject
  or stable path to read the full document.
- `tusker new doc <subject> --read-when <line> --skip-when <line> --kind
  doc|proposal|decision` creates a document in the portable tree. Both triage
  lines are required: a `doc` without `--domain` defaults to the system
  root, `--domain <name>` targets `docs/system/domains/<name>/`, proposals go
  to `docs/system/proposals/`, and decisions to `docs/system/decisions/`.
  `--kind spec` stays accepted as the spelling for proposal. `tusker check
  --only docs` reports any managed document other than a forwarding stub that lacks
  `read_when` or `skip_when` as `DOC_REQUIRED_FIELD_MISSING`.
  `--supersedes <old-subject>` also sets the old document (same kind family, no uncommitted changes) to `status: superseded` with `superseded_by`, in one transaction.
- `.tusker/` holds tracker state and thin pointers only; product decisions
  move to documentation while work and lifecycle decisions stay with the
  tracker.
- `tusker list docs --dir [<managed-directory>] [--limit <n>] [--json]` returns one
  bounded directory level. It defaults to `docs/system`, accepts `--limit` up
  to 200, and includes folder summaries plus file discovery metadata.
- `tusker show <subject-or-path> [--section <heading>] [--current]
  [--json]` reads exactly one managed document. Use `--section <heading>` for
  one Markdown section and `--current` to follow a supersession link. Reads
  are side-effect free.
- `tusker show <subject-or-path> --backlinks [--limit <n>] [--json]` shows
  incoming metadata and body relationships with a bounded `--limit`; dangling
  managed routes are listed separately. `tusker check --only docs
  [--json]` reports named defects with repair guidance and
  exits nonzero when the selected corpus is invalid.
- `tusker new doc <subject> --print` prints a validated scaffold without
  writing it. It is the non-destructive template path; ordinary `tusker new doc`
  remains the only scaffold writer.
- `tusker check --fix` rebuilds the index and graph from the same resolver. It
  includes metadata relationships, Markdown/Obsidian links, backlinks, and
  supersession edges; broken managed routes fail validation.
- `tusker list docs --stale [--strict] [--json]` reports freshness. A doc
  is `stale` when commits touched its `describes` paths after its
  `last_verified` commit; `--stale` lists only those, and `--strict`
  exits nonzero with `DOCS_STALE` when any exist.
- `tusker check --only docs --since <ref> [--waive <subject>]... [--json]` runs the
  close-time doc-touch check without a task. It diffs `<ref>...HEAD` plus
  staged, unstaged, and untracked changes, and lists every document whose
  `describes` paths changed while the document did not. `docs.touch_rule`
  decides the outcome: `warn` (default) prints drift and exits 0, `block`
  exits nonzero with `DOC_TOUCH_DRIFT`, `off` skips the check. Repeat
  `--waive <subject>` to accept drift for a document. An unresolvable ref
  or a non-git directory is an error. Pre-merge hook example:

  ```sh
  #!/bin/sh
  # .git/hooks/pre-merge-commit
  exec tusker check --only docs --since "$(git merge-base HEAD MERGE_HEAD)"
  ```
- `tusker check --only skills --strict --json` checks the skill routes.

## Output contract

Use `--json` for scripts. A command error returns a non-zero exit code and a
typed error. Normal output is for a person and can change its layout.

## Code sources

- `cmd/tusker/cli.go`
- `cmd/tusker/capabilities_cmd.go`
- `cmd/tusker/lifecycle_cmd.go`
- `cmd/tusker/show_list_cmd.go`
- `cmd/tusker/commands_*.go`
- `cmd/tusker/install.go`
