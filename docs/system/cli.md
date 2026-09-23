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

Direct authoring is canonical: `tusker new task ... --body-file` writes one
task whose body is the implementation contract, and `tusker wave create
--file <request.yaml> --request-key <key>` atomically authors a complete
`tusker.wave-authoring/v1` request. `--epic` and `--spec-refs` are optional;
every supplied spec ref must resolve. Creation is inert — execution begins
only at `task start` or `wave start`.

| Need | Command |
| --- | --- |
| Find runnable work | `tusker next` |
| Read one task | `tusker show <TASK-ID> --capsule` |
| List work | `tusker list` |
| Search tracker text | `tusker search <text>` |
| Create a task | `tusker new task --vault ./.tusker --title "..." --work-level standard --body-file task-body.md` |
| Author a task batch | `tusker wave create --file wave.yaml --request-key <key> --json` |
| Start one task | `tusker task start <TASK-ID> --mode interactive\|background --by <actor> [--current-workspace] --json` |
| Start a wave | `tusker wave start <WAVE-ID> --mode background --by human:<name>\|operator:<name> --json` |
| Pause / resume a wave | `tusker wave pause\|resume <WAVE-ID> --by human:<name>\|operator:<name> --json` |
| Change task state | `tusker status <TASK-ID> <STATE> --reason "..."` |
| Add a check result | `tusker verify add <TASK-ID> ...` |
| Submit review | `tusker review submit <TASK-ID> ...` |
| Close a task | `tusker close <TASK-ID>` |
| Check the tracker | `tusker validate --vault ./.tusker --json` |

One `wave start` durably authorizes the exact current wave material; daemon
polling then releases each dependency frontier automatically — no second
start. `wave pause` blocks new wave-owned admissions while admitted attempts
finish; `wave resume` restores the same authorization and refuses when the
material drifted. A task `start` inside a paused wave stays task-scoped and
leaves the wave paused. `--mode background` persists a durable run directive
for the configured runtime; it does not itself launch a runner.

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
| Create or update a profile | `tusker models profile-set --scope global\|project --name <stable-id> --display-name <name> --eligible-tiers light,standard --harness <harness> --model <id> [--effort <effort>] --preset <preset> --if-revision <sha256>` |
| Disable or enable future use | `tusker models profile-disable\|profile-enable --scope global\|project --name <name> --if-revision <sha256>` |
| Remove an unreferenced profile | `tusker models profile-remove --scope global\|project --name <name> --if-revision <sha256>` |
| Set an ordered primary/fallback list | `tusker models set --scope global\|project --level <level> --lane execute\|review --profiles primary,fallback --if-revision <sha256>` |
| Reset a project field to inheritance | `tusker models reset --scope project --level <level> --lane execute\|review --if-revision <sha256>` |
| Author task-level choices | `tusker new task ... --work-level standard --review-level demanding --review-reason "Security-sensitive review"` |
| Explain one task's effective route | `tusker runner route <TASK-ID> --lane execute\|review --json` |

Writes are atomic and accept the revision returned by `models show`. Catalog
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
in the profile document, for example:

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
