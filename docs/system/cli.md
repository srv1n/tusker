---
title: "Tusker CLI reference"
subject: cli
keywords: [cli, commands, flags, json, capsule, capabilities, exit-codes, vault-discovery, config-resolve, deprecations, adoption tier, reset, relaunch, preserve specs]
part_of: overview
status: canonical
read_when: "You need the command inventory, global flag/output conventions, vault and config resolution, capability reporting, or exit/error semantics of the `tusker` binary."
skip_when: "You need what a command *means* — lifecycle, proof, gate, wave, landing, or orchestration semantics live in the subject docs this one links to."
sources:
  - cmd/tusker/cli.go
  - cmd/tusker/main.go
  - cmd/tusker/commands_index.go
  - cmd/tusker/capabilities_cmd.go
  - cmd/tusker/docs_adopt_cmd.go
  - cmd/tusker/removed_surfaces.go
  - cmd/tusker/helpers.go
  - cmd/tusker/config_resolve_cmd.go
  - cmd/tusker/commands_show.go
  - cmd/tusker/commands_search.go
  - cmd/tusker/commands_pickup.go
  - cmd/tusker/commands_open_print.go
  - cmd/tusker/commands_compact.go
  - cmd/tusker/commands_context.go
  - cmd/tusker/install.go
  - cmd/tusker/project_reset.go
  - cmd/tusker/spec_snapshot.go
  - cmd/tusker/terminal_layout.go
---

# Tusker CLI reference

One binary, one flat dispatch switch (`runInner` in `cmd/tusker/cli.go`). There is
no flag library: `parseCLI`/`parseArgs` split `argv` into a command string and a
`map[string]string`, so every flag is `--key value` or a bare `--key` (which
becomes `"true"`). There are no short flags — `-h` is a top-level *command* word,
not a flag — and `--key=value` is **not** parsed: it becomes the key `key=value`.

The authoritative inventory is the binary itself:

```
tusker capabilities --json      # read-only; refuses without --json
```

## Invocation grammar

| Rule | Behavior | Code |
|---|---|---|
| Command word | `argv[1]`; a second bare word joins it (`work start`) only for commands in `commandTakesSubcommand` | `cli.go` |
| Positionals | Collected into `_pos` (newline-joined) plus `_pos0`, `_pos1`, … | `parseArgs` |
| Bare flag | `--json` with no following value (or followed by another `--flag`) parses as `"true"` | `parseArgs` |
| `help <cmd>` | `tusker help show` and `tusker show --help` reach the same help page | `printCommandHelp` |
| `legacy …` | Whole namespace refuses: prints legacy help / JSON error, exit 1 | `cli.go` |
| Unknown | Prints `Unknown command:` + full help, exit 1 | `runInner` default |

## Global conventions

| Concern | Contract |
|---|---|
| `--vault <path>` | Explicit vault root, made absolute. Accepted by every command that resolves a vault. |
| Vault discovery | With no `--vault`, `resolveVaultPath` walks up from cwd via `discoverVault`: a vault dir, a repo-configured vault path, `./.tusker`, then the legacy child dir. Failure is `MISSING_ARG` with a `tusker init --yes` hint. `TUSKER_VAULT` is **not** read for discovery — it is only exported *into* hooks and runner workspaces. |
| `--json` | Success payloads and errors both switch to JSON. Success shape is per-command; the error shape is always `{"ok":false,"error":{code,message,path,hint,context}}` (`main.go`). |
| `--quiet` | Suppresses human confirmation/summary lines. Honored broadly — ~70 call sites across create, lifecycle, wave, delivery, proof, skill, and read commands. |
| Terminal width | `--width`, else `TUSKER_WIDTH`, else the tty size, else `COLUMNS`, else 100; clamped to ≥40 (`terminal_layout.go`). |
| Markdown style | `print` renders through glamour: `--style <name>`, else `GLAMOUR_STYLE`, else `dark`. `--plain` / `--style plain` bypasses rendering. |
| Daemon nudge | After any successful vault-mutating command, `run()` notifies a resident daemon and, for project-registry mutations, sends `reconcile_registry`. Nothing is dispatched from the CLI itself. |
| Worker context | `TUSKER_ATTEMPT_ID` marks a dispatched worker process; other `TUSKER_*` vars are runtime/test seams, not a user flag surface. |

### Exit codes and errors

Only three outcomes exist. `main.go` prints `[CODE] path: message` (+ `hint:`) to
stderr and exits.

| Exit | Meaning |
|---|---|
| 0 | Success. |
| 1 | Any returned error (default when a handler returns exit 0 with an error), unknown command, refused legacy/removed surface, or a clean "checked and found problems" result — `validate` returns 1 on errors without an error object. |
| >1 | No `runInner` handler returns >1. The only case is `daemon run`'s watchdog, which `os.Exit(70)`s outside this path. |

Error codes are string constants in `cmd/tusker/schema.go` (`MISSING_ARG`,
`INVALID_ARG`, `NOT_FOUND`, `ALREADY_EXISTS`, `INVALID_TRANSITION`,
`EVIDENCE_GATE`, `CONFIG_INVALID`, `HOOK_FAILED`, `HOOK_TIMEOUT`, …). Two codes
live outside that file: `READINESS_CONTRACT_INVALID` (`errors.go`) and
`CAPABILITY_CONTRACT_INVALID` (`capabilities_cmd.go`).

## Read surfaces (cheap context)

These are the commands an agent should reach for before reading raw files.

| Command | Modes and key flags | Owning file |
|---|---|---|
| `show <ID>` | Default mode is **capsule**. Mode flags: `--full`, `--acceptance`, `--evidence`, `--verification`, `--capsule`; `--section <heading>` (prefixed `## ` unless it starts with `#`). `--json` always emits the `tusker.task-status/v1` projection (id, kind, title, status, readiness, capsule, cross-scope deps) regardless of mode. | `commands_show.go` |
| `search <text>` | `--query`, `--type`, `--status`, `--epic`, `--limit` (default 20, hard cap 100), `--json`. Skips generated files and attachments. | `commands_search.go` |
| `list [EPIC]` | `--type`, `--status`, `--epic`, `--wave`, `--assignee`, `--project`, `--limit`, `--format table\|ids`, and the boolean narrowers `--ready --running --review --mine --open --closed --runnable`. A positional epic implies `--type task --open`; any narrower implies `--type task`. | `commands_index.go` |
| `next` | Ranked pick of runnable work. `--epic`, `--owner`/`--assignee`, `--domain`, `--lane`, `--explain` (per-task skip reasons), `--claim` (delegates to `claim`), `--json`, `--quiet`. Without a pick it errors `NOT_FOUND`, except under `--explain`, which reports the skip list and exits 0. | `commands_pickup.go` |
| `print <ID>` | Terminal-rendered markdown. Same mode flags as `show` but the **default is `--full`**; `--plain`, `--style`, `--json` (returns the markdown as a field), `--project`. | `commands_open_print.go` |
| `open <ID>` | Opens in OS handler, `--app <name>`, `--editor` (`$VISUAL`/`$EDITOR`), or `--obsidian` (URI). `--path`/`--print` prints the target instead; `--json` reports `target`/`target_kind`. `--editor` with `--obsidian` is `INVALID_ARG`. | `commands_open_print.go` |
| `compact [<ID>\|--all]` | **Dry run by default**; `--write` persists. `--archive-logs` moves legacy log sections out of the note; `--verbose` shows unchanged notes under `--all`; `--json`. | `commands_compact.go` |
| `context audit --file <jsonl>` | Offline audit of a Codex session transcript for tool-output bloat: `--top <n>` (default 12), `--json`. Reads a file path only — it does not touch the vault. | `commands_context.go` |

`print`/`open` are the only read commands that resolve records **across
registered projects**: with no `--project`, they try the current vault first,
then every registered project; an ID present in more than one is `INVALID_ARG`
asking for `--project` or `--vault`.

## Command families

Semantics belong to the subject docs; this table is the routing map.

| Family | Purpose | Key subcommands / flags | Owning code | Semantics |
|---|---|---|---|---|
| `init`, `reset`, `relaunch`, `install`, `update`, `sync-repo-contract`, `setup doctor\|repair` | Install, reset, and wire a repo | `init --yes --fresh --purge-state --vault-only`; `reset --yes --repo <path>` preserves `.tusker/specs/**` | `install.go`, `project_reset.go`, `setup_doctor.go` | [[skills]], [[project-reset-and-relaunch]] |
| `new epic\|task\|gate\|decision`, `knowledge new`, `docs find\|new\|map` | Create records | `new task --epic --size --risk` | `commands_v7.go`, `v7_domain_cmd.go` | [[tasks-and-proof]] |
| `docs adopt` | Review and apply a fingerprinted brownfield documentation migration | `--table --approve --by human:<name>`; interactive agent receipt: `--by user-session:<id> [--approval-token user-session:<id>@<fingerprint>]` | `docs_adopt_cmd.go` | [[knowledge-and-feedback]] |
| `list`, `search`, `show`, `print`, `open`, `next`, `brief`, `packet` | Read work | see read-surface table | `commands_*.go`, `v7_migration_cmd.go` | [[tasks-and-proof]] |
| `status`, `discard`, `close`, `accept`, `finish`, `release`, `heartbeat` | Lifecycle moves | `status <ID> <status>` (either order is accepted) | `removed_surfaces.go` shim → `commands_v7.go` | [[tasks-and-proof]] |
| `work start\|status\|heartbeat\|submit\|fail\|release`, `claim` | One owned interactive session | `--json`, `--vault`; `claim` is a compatibility alias that forwards to `work start` when a workflow file exists | `work_session_cmd.go`, `commands_pickup.go` | [[tasks-and-proof]] |
| `verify add\|recipe`, `evidence add\|promote\|prune`, `proof`, `attachments`, `redact`, `attempt`, `handoff` | Proof and artifacts | `verify` and `evidence` route on a positional word; a bare `verify` is refused | `v7_proof_cmd.go`, `removed_surfaces.go` | [[proof-and-closeout]] |
| `gate`, `gate-run`, `gate-ledger check\|record` | Gate tiers and tree-keyed results | `gate-run --changed --base <ref>` | `v7_control_cmd.go`, `gate_tier.go`, `gate_ledger.go` | [[gates]] |
| `review submit` | Attempt-bound reviewer verdict | `--verdict`, `--attempt`, `--covers`, `--source-sha`, `--task-rev`, `--work-rev`, `--gate-fingerprint`, `--proof-fingerprint` | `review_result.go` | [[gates]] |
| `wave create\|add\|remove\|show\|brief\|preflight\|arm\|pause\|resume\|disarm` | Named task batches | | `v7_wave_cmd.go` | [[delivery-and-waves]] |
| `delivery plan\|context\|doctor\|review\|import\|start\|rollout` | Delivery contract and authorization | `--plan`, `--confirm`, `--by`, `--scope`, `--json` | `delivery_cmd.go` | [[delivery-and-waves]] |
| `land`, `closeout [status]`, `departure check\|status\|history\|hold\|resume` | Merge lane and terminal checkpoints | | `v7_land_cmd.go`, `v7_closeout_cmd.go`, `departure_commands.go` | [[landing-and-completion]] |
| `daemon run\|install\|uninstall\|status\|stop\|limits\|resume\|service`, `refresh` | Resident operator loop | interactive sessions must not start `daemon run` | `daemon.go` | [[orchestration]] |
| `automation status\|queue\|explain\|plan\|dispatch\|collect-external\|external-loop\|advance-external` | Daemon work queue | `plan`/`explain`/`queue`/`status` are read-only; `dispatch` is daemon-only | `automation_commands.go` | [[orchestration]] |
| `projects add\|list\|limits\|enable\|disable\|rebind\|remove\|prune` | Repo registry for daemon pickup | | `daemon.go` | [[orchestration]] |
| `runs claim\|start\|heartbeat\|submit\|fail\|reclaim\|inspect\|logs\|events\|interrupt\|release\|retire\|redrive`, `redrive` | Run/lease management | | `runs*.go` | [[orchestration]] |
| `runner catalog\|profiles\|route`, `runner-wrapper`, `acp setup\|install\|doctor` | Runner adapters and local ACP runtime | `runner --lane --bundled --write --json`; `acp setup --npm-prefix --node --auth-source --auth-principal` | `runner_catalog.go`, `runner_wrapper.go`, `acp_setup.go` | [[runners-and-acp]] |
| `execution register\|attach\|rename\|bind\|detach\|rebind\|inbox\|list\|show\|cancel\|launch` | Direct-work identity and provider correlation (`tusker execution inbox` lists unbound work) | `--json` | `execution_commands.go` | [[execution-observability-system]] |
| `logbook`, `digest`, `streams`, `dashboard`, `serve`, `trace list\|show\|replay` | Read the state as a human | `logbook --date --write`; `digest --since --json` | `v7_logbook_cmd.go`, `v7_escalation_digest_cmd.go`, `serve_command.go`, `trace.go` | [[serve-ui]] |
| `validate`, `reconcile`, `reindex`, `compact`, `purge`, `gc`, `state`, `migrate close-policy\|evidence-policy\|vault-root` | Vault hygiene and state movement | `validate --branch-policy-only --json`; `reconcile --id --dry-run --json`; `gc --ttl --yes --json` | `commands_index.go`, `scratch_gc.go`, `purge.go`, `vault_root_migration.go` | [[storage-and-runtime]] |
| `feedback add\|digest\|ingest\|signals\|review\|promote`, `improve scan`, `escalate ack`, `proposal` | Friction capture and triage | | `v7_feedback_cmd.go`, `v7_improve_cmd.go`, `v7_proposal_cmd.go` | [[knowledge-and-feedback]] |
| `domain list\|show\|new\|canon` | Project canon | | `v7_domain_cmd.go` | [[knowledge-and-feedback]] |
| `skill doctor\|route\|pack\|sync\|bundle\|audit-agent-guidance`, `publish skill` | Skill install integrity | `publish skill --v7 --out <dir>` (refuses without `--v7`; refuses unsafe output paths) | `v7_skill_cmd.go`, `removed_surfaces.go` | [[skills]] |
| `factory operations` | Read-only factory projection | | `factory_operations.go` | [[factory-intake]] |
| `capabilities`, `version`, `config resolve`, `vault mount\|unmount\|set\|status\|repair\|move`, `xcode doctor` | Introspection and host wiring | `--json` | `capabilities_cmd.go`, `version_cmd.go`, `config_resolve_cmd.go`, `vault_workspace.go`, `xcode_doctor.go` | [[platform-support]] |

## Reset and relaunch

Use this path when a repository's disposable Tusker tracker state is stale
after an API change and repairing old tickets is not worth the effort:

```sh
tusker reset --dry-run [--repo <path>]
tusker reset --yes [--repo <path>]
# equivalent alias:
tusker relaunch --yes [--repo <path>]
```

The apply form requires `--yes`. It deletes the known repo-local Tusker state,
including tickets, epics, proof, scratch, events, generated indexes, and
generated tracker wiring, then initializes a clean V7 vault. It snapshots and
restores `.tusker/specs/**`; `docs/specs/**`, source files, and ordinary
documentation are outside the deletion boundary. `--dry-run` is read-only.

For a lower-level init composition, use
`tusker init --yes --purge-state --preserve-specs`. The reset operation does not
recreate repo pointers, repo-contract files, or Obsidian mounts; add those
explicitly with the normal install/init flags when needed.

## Capability reporting

`tusker capabilities --json` (`capabilities_cmd.go`) is the read-only installed-binary
contract. It **rejects any other argument shape** — `len(args) != 1 || !--json`
returns `INVALID_ARG`, so `--vault` cannot be combined with it. Payload
(`tusker.capabilities/v1`, plus a constant `read_only: true`):

| Field | Content |
|---|---|
| `binary` | `tusker.version/v1` projection: version, revision, `modified`, vcs time, go version, GOOS/GOARCH, binary sha256. Same projection `tusker version --json` prints. |
| `commands` | Full public inventory, each `{command, subcommands?, flags?}`, sorted. Hand-maintained in `installedCapabilityCommands()`, guarded against `runInner` drift by `capabilities_cmd_test.go`. |
| `schemas` | Record schema IDs by family: task (`tusker.task/v7`, `epic/v7`, `gate/v1`, `evidence/v1`, `wave/v7`), delivery plan, review, completion, receipt. |
| `runner_adapters` | `claude-code`, `codex`, `codex_acp`, `codex_app_server`, `codex_cloud`, `codex_exec`. A sibling `runner_catalog_schema` key carries `tusker.runner-catalog/v1`. |
| `optional_capabilities` | `{capability, available}` pairs — currently only `strict_v2_proof_authority/v1`. |
| `deprecations` | `{command, replacement}` table (below). |
| `compatibility` | Skill contract: workflow min/max, tracker schema versions, wave-authorization schemas, factory intake contract version+fingerprint, canonical skill source and payload fingerprint, provenance manifest filename, primary guides, plus a `fingerprint` over the whole manifest. |

Host discovery and project policy are deliberately excluded: neither can make an
installed CLI capability appear.

## Deprecated and removed surfaces

The deprecation table in the manifest is the contract. Every listed command still
*dispatches* — it returns `INVALID_ARG` "… was removed from the V7-only Tusker
build" (`removedSurfaceError`) and exit 1, so a stale caller gets a typed refusal
rather than silent legacy behavior.

| Removed | Use instead |
|---|---|
| `docs apply\|build\|catalog\|check\|dev\|export\|freshness\|init\|model\|noop\|waive`, bare `docs` | `docs find\|new\|map`; `validate` for `check`; task proof or a decision for `noop`/`waive`; the repo publication pipeline for build/dev/export |
| `knowledge apply\|check\|freshness\|list\|map\|noop\|route\|show\|waive` | `domain list\|show`, `docs map`, `skill route`, `show --capsule`, `validate` |
| `domain graph`, `graph` | `domain list\|show` |
| `new bug`, `new doc` | `new task`, `docs new` |
| `migrate gates`, `migrate v7` | `migrate evidence-policy\|close-policy\|vault-root`; V7 is the only tracker |
| `publish build\|dev\|export\|llms` | repository publication pipeline |
| `propose` | `proposal` (note: `propose` currently still runs `proposalV7Cmd`, it does not refuse) |
| bare `verify` | `verify add\|recipe` |
| `legacy …` (whole namespace) | V7 commands listed by `tusker capabilities --json` |

## Config resolution

`tusker config resolve <key> [--vault <path>] [--json]` (`config_resolve_cmd.go`)
answers "which layer set this, and which layers lost". Human output prints the
key, the effective value, the winning source with its path, then every candidate
source labelled `winning`/`losing` and `unset` where absent. `--json` wraps the
same report under `{"ok":true,"resolution":…}`.

Runtime policy itself lives in `WORKFLOW.md` plus `.tusker/config.yaml`;
`_system/config.yaml` is read only for explicit legacy/migration flows and is not
written by `init` (`config.go`). `init` generates `.tusker/config.yaml` from
`writeDefaultTuskerConfig` (`commands_create.go`) and never overwrites an existing
one; the legacy `Config` struct's own fallbacks live in `defaultConfig()`. There is
no third, unused default-config template. Hook commands from that config run as
`sh -c` with `TUSKER_VAULT`, `TUSKER_EVENT`, `TUSKER_ID`, `TUSKER_ACTOR`,
`TUSKER_DISPATCH_STATE` exported; output is capped at 64 KiB and secret-redacted,
and a timeout raises `HOOK_TIMEOUT` rather than `HOOK_FAILED`.

### Adoption tier

`.tusker/config.yaml` may declare `tier: 1`–`5` (`TuskerConfigFile.Tier`,
`internal/v7schema/schema.go`). Absent, out-of-range, or unreadable resolves to
**5**, the strict default (`tuskerTier`, `commands_v7.go`). The tier is ordinal:
each lower number relaxes one class of guard. Only these five guards read it;
`tusker factory operations` reports it per project but does not branch on it.

| Guard | Relaxed at |
|---|---|
| `new task --status ready` runs the dispatchability preflight | tier ≥ 2 (tier 1 creates ready tasks with no contract/proof check) |
| `tusker next` / `validate --dispatchable` use the full dispatch blockers | tier ≥ 2 (tier 1 checks only status ∈ trigger states and `readiness: ready`, `v7TierOneNextBlockers`) |
| demanding ready tasks require a governing `spec_refs` link | tier ≥ 2/default requires at least one resolvable path or decision ID; tier 1 allows ready work with a `TASK_SPEC_REF_REQUIRED` warning |
| `status <id> done` is refused in favour of `tusker close` | tier ≥ 2 (tier 1 may set `done` directly) |
| `projects add` requires a loadable `WORKFLOW.md` | tier ≥ 2 (tier 1 registers with a warning) |
| `runtime.serve.enabled` / `reviewer.enabled` default on | tier ≥ 4 (tiers 1–3 default both off unless the workflow sets them explicitly, `applyTuskerTierWorkflowDefaults`) |
