---
title: "Runners and the ACP adapter stack"
subject: runners-and-acp
keywords: [runners, adapters, codex_acp, acp, runner profiles, routing, wrapper, raw log, bundle verification]
part_of: overview
status: canonical
read_when: "You need to pick, configure, route, launch, bound, or debug a model runner — including the codex_acp ACP transport and its install/setup/doctor bundle verification stack."
skip_when: "You need scheduling, admission, or wave policy ([[orchestration]]) or execution identity, lineage, and provider observations ([[execution-observability-system]])."
sources:
  - cmd/tusker/runner.go
  - cmd/tusker/runner_wrapper.go
  - cmd/tusker/runner_profiles.go
  - cmd/tusker/runner_route_preview.go
  - cmd/tusker/runner_catalog.go
  - cmd/tusker/runner_raw_log_limit.go
  - cmd/tusker/runner_acp.go
  - cmd/tusker/runner_acp_codex.go
  - cmd/tusker/runner_acp_codex_live.go
  - cmd/tusker/acp_permission.go
  - cmd/tusker/acp_setup.go
  - cmd/tusker/acp_adapter_install.go
  - cmd/tusker/acp_adapter_npm.go
  - cmd/tusker/acp_adapter_bundle.go
---

# Runners and the ACP adapter stack

A **runner** is a transport that turns one admitted Tusker attempt into one provider process (or one
remote cloud task). Runners never own tasks: the `runs` lease decides ownership, and everything a
runner reports is either a bounded process fact or an observation. Identity and lineage live in
[[execution-observability-system]]; dispatch policy lives in [[orchestration]].

## Adapter matrix

`RunnerName` constants and `runnerForName` (`cmd/tusker/runner.go`, `cmd/tusker/daemon.go`):

| Runner | Implementation | Transport | Wrapper-launched | `Resume()` | Notes |
|---|---|---|---|---|---|
| `codex_acp` | `CodexACPRunner` | ACP v1 over stdio, sealed bundle | yes | refuses | Built-in local primary. Requires a complete named runner definition; refuses a `command` string. |
| `acp_v1` | `ACPRunner` | ACP v1 over stdio, generic | yes | refuses (`INVALID_TRANSITION`) | Provider-neutral transport. Never inferred from legacy records. |
| `codex_exec` | `CodexExecRunner` | `codex exec` child process | yes | yes | Completion-authoritative lane; bounded raw log. |
| `codex_app_server` | `CodexAppServerRunner` | `codex app-server` JSON stream | yes | yes | Wrapper then `startLiveCodex`. |
| `codex` | `CodexRunner` | legacy `codex` command | only when the command is app-server (delegates to `codex_app_server`) | yes | Legacy; command from `workflow.codex.command`. |
| `codex_cloud` | `CodexCloudRunner` | remote cloud task | no | refuses | Own `Start`/`Reconcile`/`Collect`; no local PID, no heartbeat. |
| `claude-code` | `ClaudeRunner` | `claude` CLI | no — `startLiveClaude` or `executeRunnerCommand` in process | yes | Command from `workflow.claude.command`. |

Every runner implements `Runner`: `Name`, `Capabilities`, `Start`, `Resume`, `Reconcile`,
`Interrupt`, `Collect`. `RunnerCapabilities` advertises `StructuredEvents`, `ResumeSession`,
`ExplicitApprovals`, `Heartbeats`, `MachineFinalStatus`, `UsageMetrics`, `ArtifactEnumeration`.
`ResumeSession` is advertised true only by `codex_exec`; a working `Resume()` elsewhere does not
imply a resumable persisted session. An unknown name is `CONFIG_INVALID`, never a silent fallback.

`ACPRunner.Reconcile` deliberately returns `released` + `abandoned` whenever the fenced wrapper
process is gone: the shared transport cannot prove whether a provider turn ran, so it never
auto-resumes.

## Catalog, profiles, routing

```bash
tusker runner catalog [--bundled] [--json]   # machine-local observation, never policy
tusker runner profiles                        # bootstrap profile config
tusker runner route <TASK-ID> --lane execute|review [--json]
```

`runner catalog` (`cmd/tusker/runner_catalog.go`) shells `codex debug models` (3 s timeout) and
`claude --version`, reporting `source` ∈ {`live`, `bundled`, `declared`} and `confidence` ∈
{`high`, `medium`, `lower`, `none`}. Claude models are *declared*, not discovered. `--bundled` falls
back to a static `gpt-5.x` entry when the live probe fails.

A **profile** (`RunnerProfileDefinition`, `cmd/tusker/runner_profiles.go`) is
`harness`, `model`, `effort`, `permission_preset`, optional `command`, `sandbox{mode, network}`,
`subagents{allowed, max_concurrent}`. Config layers resolve built-in → user-global → project →
machine-local.

`resolveRunnerProfileForNote` picks in strict precedence order; `tusker runner route --json` prints
the whole ladder with the winner flagged (`runnerRoutePreview`, schema `tusker.runner-route/v1`,
read-only — it opens no runtime store and makes no claim):

| # | Source | Key |
|---|---|---|
| 1 | task frontmatter | `runner_profile:` |
| 2 | `automation.routing` | first rule matching epic/risk/size/domains/title keywords |
| 3 | `automation.lane_profiles` | lane → profile |
| 4 | task complexity | `routine→execute-fast`, `standard→execute-standard`, `complex→execute-complex`, `frontier→execute-frontier`; any complexity in the review lane → `review-independent` |
| 5 | `automation.default_profile` | else built-in `default` |

Complexity must be one of `routine|standard|complex|frontier`; anything else is a blocker, not a
default. A named profile that does not exist is `CONFIG_INVALID` — routing never invents one.

The review lane hard-clamps Codex policy regardless of profile (`codexPolicyForLane`):
`approval_policy=never`, `thread_sandbox=read-only`, `turn_sandbox_policy=read-only`.

## Runner wrapper

Every wrapper-launched runner goes through `startDetachedRunnerWrapper`
(`cmd/tusker/runner_wrapper.go`). The daemon never holds the child directly.

1. Serialize a `runnerWrapperRequest` next to the status path (`…​.wrapper-request.json`, private).
2. `exec tusker runner-wrapper --request <path>` with `Setsid`, `cwd == workspace_path`, stdin
   `/dev/null`, stdout+stderr to `<vault>/scratch/<task>/runner-wrapper.log`.
3. The wrapper deletes the consumed request, registers its own PID/PGID/process-start against the
   lease (`registerRunnerWrapperSpawn`), and emits `attempt_wrapper_spawned`. If the lease was
   recovered or revoked in between, it writes status exit 130 and refuses to spawn.
4. A heartbeat goroutine renews the lease every `defaultRunHeartbeatInterval`
   (`TUSKER_WRAPPER_HEARTBEAT_MS` overrides). It stops on generation advance, owner change, missing
   row, or a lease state outside `claimed`/`running`.
5. The wrapper polls for the terminal status file every 100 ms, records the direct outcome via
   `classifyRunnerProcessExit`, and exits.

Invariants worth knowing:

- **cwd is load-bearing.** `assertRunnerCommandDir` fails `CONFIG_INVALID` if `cmd.Dir` is not the
  symlink-resolved `workspace_path`.
- **Containment.** The wrapper's own PGID is the containment group. ACP children must launch inside
  it; after a durable ACP status the wrapper `SIGKILL`s its own group so a provider descendant
  holding stdout cannot outlive the attempt (`runnerWrapperReapACPContainmentAfterStatus`).
- **Interrupt escalation.** Prefer the in-process `liveRegistry` handle; otherwise `SIGINT` to the
  group, wait up to 2 s, then `SIGTERM`. Stop timeout is 10 s (`TUSKER_WRAPPER_STOP_TIMEOUT_MS`).
- **No silent hang.** If an ACP child is reaped without publishing a status, the wrapper publishes
  one bounded `failed` status rather than heartbeating forever.
- `runnerEnv` injects the full `TUSKER_*` environment (project, record, item, attempt, lane,
  revision, lease generation, workspace, vault, prompt/event/raw-log/status paths, Codex policy,
  runner profile/harness/model/effort, external-loop context, extension policy).

## Bounded raw logs

Completion-authoritative lanes cap the raw log at `completionAuthoritativeRawLogMaxBytes = 16 MiB`
(`cmd/tusker/completion_worker_safety.go`), set on `StartRequest.RawLogMaxBytes` for `codex_acp` and
for completion-bound `codex_exec` (`cmd/tusker/daemon.go`). `codex_acp` refuses to launch at all
with a non-positive limit.

`boundedRawLogWriter` (`cmd/tusker/runner_raw_log_limit.go`) is the trusted producer boundary:

- Opens with `O_APPEND|O_NOFOLLOW|O_CLOEXEC` at mode `0600` via a parent-fd handle; requires a
  regular file, exactly `0600`, and `nlink == 1`. Bytes already present count toward the cap.
- On overflow it truncates the write, returns `errAuthoritativeRawLogOverflow`, and asynchronously
  kills the process group (async so the stderr copier cannot self-deadlock). The terminator is bound
  only after `attempt_spawned` durably published the PID.
- Overflow settles as exit code **74**, outcome `failed`. Context cancellation settles as **130**,
  outcome `interrupted`. The monitor never signals the group after `Wait` — a reused PGID could
  belong to something else.
- The status file is published exactly once via a link-based atomic create, then the parent directory
  is fsynced.

## ACP transport

`ACPRunner`/`CodexACPRunner` speak ACP v1 over local stdio to one preinstalled, fingerprinted adapter
process per attempt. `startLiveACPForRunner` (`cmd/tusker/runner_acp.go`) refuses unless:

- it is inside the wrapper containment group and `processGroupID(getpid()) == ContainmentPGID`;
- the status path does not already exist;
- `CommandArgv[0]` is absolute, `CommandExecutableFP` is a valid `sha256:` digest, no argv entry
  contains `{{`/`}}` template markers, and `RawLogMaxBytes > 0`;
- the resolved executable is a regular file with an exec bit, reached through `EvalSymlinks`.

For `codex_acp` the serialized `CodexACPProviderPlan` is revalidated **twice more** immediately
before spawn, and any argv drift is `CONFIG_INVALID`. The `initialize` handshake must return agent
name `@agentclientprotocol/codex-acp` at the exact expected version.

`CodexACPProviderPlan` (`tusker.codex-acp-provider-plan/v1`) is serializable and credential-free:
bundle root, manifest path + sha256, adapter version, launch kind, auth source, non-secret
`auth_principal_sha256`, model, effort, mode, `network_enabled`, and the full bundle receipt. The
wrapper resolves the actual credential from its own inherited environment after revalidating.

Auth is exactly one source (`CodexACPAuthContract`): `chatgpt_session` → `CODEX_HOME` (falling back
to `~/.codex`), `codex_api_key` → `CODEX_API_KEY`, `openai_api_key` → `OPENAI_API_KEY`. Exactly that
one variable crosses the process boundary; `HOME`, XDG auth homes, unselected keys, `CODEX_PATH`, and
`TUSKER_*` never do. `PATH` is the fixed `codexACPDefaultFixedPath`.

Permission presets map one-way to an adapter mode (`CodexACPModeForPermissionPreset`):
`read-only → read_only`, `workspace-write-offline`/`workspace-write-network` → `workspace_write`.
`danger-full-access` is refused outright.

## The ACP permission broker

`EvaluateACPPermission` (`cmd/tusker/acp_permission.go`) is pure, fail-closed, and has **no approval
memory** — every callback is evaluated fresh. Tool kinds normalize to exactly `read|write|execute|
network`; anything else is `unknown_tool` and a reject.

Checks in order: cancelled → attempt ID binding → session ID binding → request bounds (identities
≤256 B, workspace/target ≤4096 B, tool kind ≤64 B, ≤32 options) → known tool kind → budget authorized
→ tool allowlist → workspace containment (lexical *and* symlink-resolved, for read/write/execute;
`execute`'s target is its working directory, never the command text) → read-only / workspace-write /
execute / network policy → an advertised `allow_once` option must exist.

The only success outcome is `allow_once` on that exact option ID. Reason codes are stable and
data-free (`attempt_mismatch`, `target_outside_workspace`, `tool_not_allowed`, `budget_exceeded`,
`read_only`, `network_not_allowed`, `allow_once_unavailable`, …). The audit record carries only an
operation class, a SHA-256 digest of the normalized target, a policy rule, and the outcome — never the
target, command, prompt, or credential.

## Install, setup, doctor

Three commands, all `--json`-only, all refusing unknown flags and positionals
(`validateACPAdapterCommandArgs`). None of them download, execute a package manager, authenticate,
prompt a provider, or start the daemon.

```bash
tusker acp install --provider codex --artifact /abs/codex-acp --version V \
  --artifact-sha256 sha256:… --source-url https://… --publisher agentclientprotocol --json
tusker acp setup --npm-prefix /abs/npm-prefix [--node /abs/node] \
  [--auth-source chatgpt_session|codex_api_key|openai_api_key] [--auth-principal LABEL] --json
tusker acp doctor --bundle-digest sha256:… [--auth-source …] --json
```

State lives under `<state-root>/acp-adapters/` (`0700`), with `.staging/`, `bundles/`, and
`receipts/`. A sealed bundle is `bundles/<digest>` (native) or `bundles/npm-<digest>` (npm closure);
its receipt is `receipts/<digest>.json` at mode `0400` with exactly one hard link. Bundle roots and
directories seal to `0500`, executables to `0500`, other files to `0400`. Names are content-addressed
by an *identity* digest, not by artifact bytes alone.

**`acp install`** takes an already-downloaded local binary. `--provider` must be `codex` and
`--publisher` must be `agentclientprotocol`; `--source-url` must be an https URL with no userinfo,
query, or fragment. Publisher and source URL are recorded as
`unverified_caller_metadata` — there is no signature verification. Trust comes from
`--artifact-sha256` equality plus filesystem invariants. The pipeline hashes the source, copies into
`.staging` (re-hashing before and after, with `SameFile`/size/mtime TOCTOU brackets), writes
`manifest.json`, fsyncs, publishes by no-overwrite rename, seals, re-verifies the whole tree, then
hardlinks the receipt into place. Existing receipts short-circuit; an orphaned final root without a
receipt is revalidated and recovered rather than re-staged.

**`acp setup`** packages an already-installed npm prefix into a sealed bundle
(`PackageACPAdapterNPM`) and then writes machine-local config making `codex_acp` primary. Versions
are hard-pinned in code: `@agentclientprotocol/codex-acp@1.1.14` and `@openai/codex@0.147.0`; any
other installed version fails. It resolves the runtime closure by walking `node_modules` inside the
prefix only (no registry, no PATH, no ambient global root), requires exactly one declared platform
package (`@openai/codex-{darwin,linux}-{arm64,x64}`), and refuses any package declaring
`preinstall`/`install`/`postinstall` scripts. It then probes the sealed bundle with `--version` under
a 15 s timeout and a minimal env, requiring exactly
`@agentclientprotocol/codex-acp <version>`. Config writes are transactional: `config.local.yaml` is
rewritten atomically, then the workflow is reloaded and `runnerForName("codex_acp")` must succeed —
otherwise the previous file content is restored.

**`acp doctor`** re-runs the full verification every invocation; there is no cached trust. Report
`tusker.acp-adapter-doctor/v1`: `installed`, `integrity` ∈ `not_installed|valid|invalid`,
`validation_error`, `auth_source`, `auth_source_present`. Note two fields are hardcoded false today:
`configured` (doctor deliberately does not inspect workflow/profile config) and `authenticated`
(doctor never authenticates and never reads a credential value).

## Bundle verification

`ValidateACPAdapterBundle` (`cmd/tusker/acp_adapter_bundle.go`) proves that the bytes under one root
match `manifest.json` (`tusker.acp-adapter-bundle/v1`) under a **same-UID trust boundary**. It does
not prove immutability — a same-UID process could still swap files — so every launch path calls
`RevalidateACPAdapterBundleReceipt` immediately before spawn, and `descriptorAndArgv` + `LaunchArgv`
each do so independently.

Refused unconditionally: non-darwin/linux hosts; symlinks anywhere in the tree; group/world-writable
files or directories; setuid/setgid/sticky bits; files not owned by the current UID; `nlink != 1`;
shebang scripts in an `executable` role; argv naming a shell or package manager (`sh`, `bash`, `zsh`,
`fish`, `cmd`, `powershell`, `pwsh`, `npx`, `npm`, `pnpm`, `yarn`); argv outside the bundle root or
not matching a declared asset; undeclared files or empty directories; `..`, absolute components,
backslashes, or colons in asset paths; manifest/receipt JSON that is not byte-exact canonical or has
trailing content. Size ceilings: 4096 assets, 1024-byte paths, 1 MiB manifest, 512 MiB per file,
2 GiB total, 8192 tree entries, depth 64.

Provider/adapter pairs are hardcoded: `codex/codex-acp` may launch native or interpreter;
`claude/claude-agent-acp` is interpreter-only. Only `codex` is implemented by the installer today.
