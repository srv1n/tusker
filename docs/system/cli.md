---
title: "CLI reference"
subject: cli
part_of: overview
status: canonical
read_when: "Looking up an exact command, document route, or machine-readable output."
skip_when: "You need the rationale or lifecycle rule behind a command."
---

# CLI reference

The installed program help is the command authority. It shows the commands
that this build can run. A machine-readable command list is also available.

## Common work commands

`tusker wave create ... --summary "<expected outcome>"` records the capability promised by a wave. `tusker wave outcome <WAVE-ID> --summary "<expected outcome>"` updates it. `wave show` and `wave brief` keep that promise separate from the derived completion result.

Delivery-plan requirements may be intentionally omitted with `deferrals: [{requirement: R1, reason: "..."}]`. Delivery review reports every requirement as covered, deferred, or uncovered and names the covering task and acceptance IDs.

| Need | Command |
| --- | --- |
| Find runnable work | `tusker next` |
| Read one task | `tusker show <TASK-ID> --capsule` |
| List work | `tusker list` |
| Search tracker text | `tusker search <text>` |
| Create a task | `tusker new task --vault ./.tusker --epic APP --title "..."` |
| Change task state | `tusker status <TASK-ID> <STATE> --reason "..."` |
| Add a check result | `tusker verify add <TASK-ID> ...` |
| Submit review | `tusker review submit <TASK-ID> ...` |
| Close a task | `tusker close <TASK-ID>` |
| Check the tracker | `tusker validate --vault ./.tusker --json` |

## Project and runtime commands

| Need | Command |
| --- | --- |
| Add a project | `tusker projects add --repo . --vault ./.tusker` |
| List projects | `tusker projects list --json` |
| Enable automation | `tusker projects enable --id <PROJECT-ID>` |
| Check the daemon | `tusker daemon status --json` |
| Check one run | `tusker runs inspect <RUN-ID> --json` |
| Start the local service | `tusker serve` |

## Executions

`tusker execution register` records a direct execution.
`tusker execution inbox` lists unbound executions. Use `execution show`,
`execution bind`, `execution rename`, and `execution cancel` for one execution.
These commands do not grant a task claim.

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

Seed imports native delivery plans and leaves every wave unstarted. The
offline lane authorizes only the named waves, executes fixed delays with
exact fixture bytes, promotes evidence while each run is active, checks
results deterministically, and closes under reviewer authority. The real
lane (`--mode real`) instead resolves each task through `tusker runner
route`, requires the named harness/profile with no silent substitution, and
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
| Discover installed models and reasoning choices | `tusker models catalog --json` |
| Read effective mappings with only referenced profiles | `tusker models show --json --compact` |
| Read every profile for administration | `tusker models show --json` |
| Create or update a profile | `tusker models profile-set --scope global\|project --name <name> --harness <harness> --model <id> --effort <effort> --preset <preset>` |
| Set an ordered primary/fallback list | `tusker models set --scope global\|project --level <level> --lane execute\|review --profiles primary,fallback --if-revision <sha256>` |
| Reset a project field to inheritance | `tusker models reset --scope project --level <level> --lane execute\|review --if-revision <sha256>` |
| Author task-level choices | `tusker new task ... --work-level standard --review-level demanding` |
| Explain one task's effective route | `tusker runner route <TASK-ID> --lane execute\|review --json` |

Writes are atomic and accept the revision returned by `models show`. Catalog
entries carry their installed-harness provenance and supported reasoning values.
Manual profile values remain configured even when discovery is unsupported; the
first live preflight decides whether the route is actually available.

Agent callers should read once, retain `revision`, make one guarded write, then
use the returned document as the new state. A stale write fails with an
actionable refresh error. Reuse `tusker runner route`; there is no duplicate
model-resolution command or client library to learn. Prefer `--compact` for
routine routing so unused profiles do not consume context.

## Documentation and skills

- `tusker docs find <query>` searches the managed corpus: system pages in
  `docs/system/`, specs in `.tusker/specs/`, and decisions in
  `.tusker/specs/decisions/`.
- Search returns a bounded shortlist with `read_when` and `skip_when` guidance.
  JSON also reports `total_matches` and `truncated`; open the returned subject
  or stable path to read the full document.
- `tusker docs new --kind doc` creates a system page. `tusker docs new --kind
  spec` creates a governing spec under `.tusker/specs/`.
- `tusker docs browse [<managed-directory>] [--limit <n>] [--json]` returns one
  bounded directory level. It defaults to `docs/system`, accepts `--limit` up
  to 200, and includes folder summaries plus file discovery metadata.
- `tusker docs read <subject-or-path> [--section <heading>] [--current]
  [--json]` reads exactly one managed document. Use `--section <heading>` for
  one Markdown section and `--current` to follow a supersession link. Reads
  are side-effect free.
- `tusker docs backlinks <subject-or-path> [--limit <n>] [--json]` shows
  incoming metadata and body relationships with a bounded `--limit`; dangling
  managed routes are listed separately. `tusker docs check
  [<subject-or-path>] [--json]` reports named defects with repair guidance and
  exits nonzero when the selected corpus is invalid.
- `tusker docs new <subject> --print` prints a validated scaffold without
  writing it. It is the non-destructive template path; ordinary `docs new`
  remains the only scaffold writer.
- `tusker docs map` rebuilds the index and graph from the same resolver. It
  includes metadata relationships, Markdown/Obsidian links, backlinks, and
  supersession edges; broken managed routes fail validation.
- `tusker docs status --json` reports freshness.
- `tusker skill doctor --strict --json` checks the skill routes.

## Output contract

Use `--json` for scripts. A command error returns a non-zero exit code and a
typed error. Normal output is for a person and can change its layout.

## Code sources

- `cmd/tusker/cli.go`
- `cmd/tusker/capabilities_cmd.go`
- `cmd/tusker/commands_*.go`
- `cmd/tusker/install.go`
