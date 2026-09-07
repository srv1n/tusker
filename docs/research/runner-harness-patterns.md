# Runner-harness patterns: what Tusker should copy

Date: 2026-09-07
Scope: installed-runtime ownership, ACP vs CLI transport, permission/sandbox
projection, capability discovery, recovery, and terminal status.

## Decision first

| Pattern | Evidence | Tusker decision |
|---|---|---|
| Resolve a named runtime to a concrete executable, then pin that path for the run | Buzz resolves PATH/login-shell/common locations and caches both hits and misses in [`discovery.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery.rs#L402-L482); it probes auth with a bounded child in [`discovery.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery.rs#L704-L735) | **Copy.** V1 owns discovery/probe only; record command, absolute path, version/auth result, and the exact launch argv. Invalidate only on explicit refresh. |
| Keep ACP and CLI as two transports over one normalized launch description | Goose's ACP provider takes `command`, `args`, `env`, and `work_dir` in [`acp/provider.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/acp/provider.rs#L61-L76); its Codex provider resolves `codex-acp` from installed search paths in [`providers/codex_acp.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/providers/codex_acp.rs#L67-L103) | **Copy.** One `LaunchSpec` feeds either ACP stdio or direct CLI execution. Do not create separate runtime catalogs or lifecycle semantics. |
| Compile abstract permission intent into adapter-native flags; keep sandbox and approval as separate fields | AgentOps maps Codex to `approval_policy=never` + `sandbox_mode=danger-full-access` in [`agent-entrypoint.sh`](https://github.com/pomerium/agentops/blob/main/deploy/harness/codex/agent-entrypoint.sh#L16-L22), Gemini to `--approval-mode yolo` in [`gemini/agent-entrypoint.sh`](https://github.com/pomerium/agentops/blob/main/deploy/harness/gemini/agent-entrypoint.sh#L16-L19), and OpenCode to config in [`opencode.json`](https://github.com/pomerium/agentops/blob/main/deploy/harness/opencode/opencode.json#L1-L8) | **Copy/adapt.** V1 has explicit profiles (`interactive`, `workspace-auto`, `full-auto`) compiled per runtime. Never silently translate “YOLO” into a host-wide permission grant; record both requested and effective policy. |
| Discover capabilities from the live ACP handshake, not a static guess | Buzz stores model/thinking capability data from `session/new` in [`pool.rs`](https://github.com/block/buzz/blob/main/crates/buzz-acp/src/pool.rs#L88-L103); ACP requirements and `stopReason` are documented in [`buzz-acp/README.md`](https://github.com/block/buzz/blob/main/crates/buzz-acp/README.md#L449-L458) | **Copy.** Start with `initialize`, `session/new`, and advertised config/modes. Persist the handshake summary with the run. |
| Make every turn terminal and observable | Buzz has per-task `TaskMeta`, turn IDs, recoverable batches, and explicit control channels in [`pool.rs`](https://github.com/block/buzz/blob/main/crates/buzz-acp/src/pool.rs#L54-L85); its pool wake state is `Listening/Waking/Ready/Failed` with bounded retry in [`pool_lifecycle.rs`](https://github.com/block/buzz/blob/main/crates/buzz-acp/src/pool_lifecycle.rs#L13-L24) | **Copy/adapt.** Tusker owns `starting/running/succeeded/failed/cancelled/timed_out`, one absolute deadline, stdout/stderr capture, and a terminal receipt. A worker cannot close its own canonical task. |
| Resume from durable session identity, while keeping orchestration state outside the agent | Hermes persists ACP sessions and recreates them after restart in [`session.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/session.py#L148-L178) and [`session.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/session.py#L328-L424); its ACP modes are session-scoped in [`server.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/server.py#L226-L289) | **Adapt.** Persist a Tusker run ID plus optional ACP session ID. Resume only when the adapter advertises it and the workspace/launch fingerprint matches; otherwise start a fresh attempt. |
| User custom harnesses may describe a command, args, env, and docs—but not installation scripts | Buzz's custom schema deliberately omits install commands in [`custom_harnesses.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/custom_harnesses.rs#L1-L10) and [`custom_harnesses.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/custom_harnesses.rs#L42-L70) | **Copy.** Allow explicit user-owned command definitions; reject arbitrary install hooks, remote icons, and silent fallback. |
| Ship agent runtimes/adapters or auto-install them | Buzz tier-1 metadata contains CLI and adapter install commands in [`catalog.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery/catalog.rs#L12-L49), while its own `buzz-agent` is an app-side default in [`discovery.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery.rs#L160-L169). AgentOps preinstalls `codex-acp` in its image [`codex/Dockerfile`](https://github.com/pomerium/agentops/blob/main/deploy/harness/codex/Dockerfile#L19-L35) | **Reject for Tusker V1.** These are valid product choices for a hosted/container system, not for Tusker's desktop contract. Tusker must not bundle Codex, Goose, Claude, or adapters, and must not run install/update/auth setup on behalf of the user. |

## What the reference implementations actually do

### Block Buzz / `block/buzz`

DeepWiki's [agent-system page](https://deepwiki.com/block/buzz/4-agent-system) is useful as an index, but the conclusions above are checked against the repository source.

1. **It is not installed-runtime-only.** The desktop catalog distinguishes compiled-in tier-1 runtimes, preset ACP commands, and custom JSON harnesses. Tier-1 metadata has installer commands for Goose, Claude, Codex, and adapters; `buzz-agent` is resolved as a sidecar shipped with the app. Presets/custom entries set `can_auto_install=false` ([`presets.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery/presets.rs#L25-L103), [`catalog.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery/catalog.rs#L67-L98)).
2. **Its good boundary is explicit command metadata.** A preset records the ACP command, args, optional underlying CLI, availability, and `requires_external_cli`; that cleanly explains “adapter missing” versus “CLI missing” ([`presets.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery/presets.rs#L25-L103)). Copy that distinction, but remove Buzz's managed-bin precedence and installer lane.
3. **Its ACP contract is simple and testable.** The harness launches N subprocesses, sends `initialize`, creates `session/new`, sends `session/prompt`, consumes `session/update`, and requires `stopReason`. Per-channel one-in-flight ordering, crash respawn, relay reconnect, and replay-from-cursor are explicit ([`buzz-acp/README.md`](https://github.com/block/buzz/blob/main/crates/buzz-acp/README.md#L393-L402)). Copy the lifecycle shape; omit relay/Nostr concerns.
4. **It bounds dangerous discovery work.** Auth/PATH probes use a hard deadline and kill the process tree; captured output has a shared ceiling ([`bounded_command.rs`](https://github.com/block/buzz/blob/main/desktop/src-tauri/src/managed_agents/discovery/bounded_command.rs#L325-L360)). Copy this for version/auth probes and CLI attempts.
5. **It has the right recovery vocabulary.** Pool lifecycle is explicit and retries only when pending work remains, with 5s initial and 300s maximum backoff ([`pool_lifecycle.rs`](https://github.com/block/buzz/blob/main/crates/buzz-acp/src/pool_lifecycle.rs#L7-L24), [`pool_lifecycle.rs`](https://github.com/block/buzz/blob/main/crates/buzz-acp/src/pool_lifecycle.rs#L95-L131)). Adapt to Tusker attempts, not a permanent worker pool.

### Goose

Goose is the clearest example of one core driving multiple transports. Its ACP provider resolves an installed `codex-acp` command and passes command/args/env/workdir as data; its direct Codex provider appends mode-specific flags ([`acp/provider.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/acp/provider.rs#L61-L76), [`providers/codex.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/providers/codex.rs#L97-L119)).

The useful mapping is concrete:

| Intent | Goose direct-Codex projection |
|---|---|
| auto/full access | `--yolo` |
| smart/workspace auto | `--full-auto` |
| interactive approval | no extra flag |
| chat/read-only | `--sandbox read-only` |

Its ACP mapping instead advertises session modes (`agent-full-access`, `agent`, `read-only`) and maps them to the same abstract Goose mode ([`providers/codex_acp.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/providers/codex_acp.rs#L74-L103), [`acp/provider.rs`](https://github.com/aaif-goose/goose/blob/main/crates/goose/src/acp/provider.rs#L2354-L2359)). Copy the “abstract intent → transport-specific projection” pattern. Reject Goose's LLM-assisted SmartApprove classifier for Tusker V1; it adds policy ambiguity where a fixed profile is enough.

### Hermes Agent

Hermes exposes the same agent core through ACP, a TUI JSON-RPC gateway, and an HTTP/SSE API ([`programmatic-integration.md`](https://github.com/NousResearch/hermes-agent/blob/main/website/docs/developer-guide/programmatic-integration.md#L12-L34)). Its ACP adapter keeps stdout protocol-only, pins each session's cwd, and persists session history so `load/resume/fork` can recreate the agent after restart ([`entry.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/entry.py#L171-L205), [`session.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/session.py#L328-L424)).

Hermes also demonstrates permission scopes that are understandable to a user: `default` (ask), `accept_edits` (workspace and `/tmp`, sensitive paths still ask), and `dont_ask` (session-only) ([`server.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/server.py#L226-L289), [`edit_approval.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/edit_approval.py#L138-L158)). Its permission bridge times out and denies on failure ([`permissions.py`](https://github.com/NousResearch/hermes-agent/blob/main/acp_adapter/permissions.py#L77-L123)). Copy the scope names/fail-closed behavior; do not copy Hermes's optional browser bootstrap or dependency installer into Tusker.

### AgentOps (Pomerium)

AgentOps is the strongest reference for a hosted sandbox, not a desktop runtime manager. It runs a prebuilt per-agent image, waits in an idle container, and execs an ACP command into it; its README states the contract and says unattended “yolo” is mandatory for Slack ([`README.md`](https://github.com/pomerium/agentops/blob/main/README.md#L254-L268)).

Copy one thing: the harness entrypoint is a tiny, reviewable command projection. For Codex, approval and sandbox are explicit `-c` settings; for Gemini, the equivalent is a native `--approval-mode yolo`; for OpenCode, a config file sets per-tool permission ([`codex/agent-entrypoint.sh`](https://github.com/pomerium/agentops/blob/main/deploy/harness/codex/agent-entrypoint.sh#L16-L22), [`gemini/agent-entrypoint.sh`](https://github.com/pomerium/agentops/blob/main/deploy/harness/gemini/agent-entrypoint.sh#L16-L19), [`opencode.json`](https://github.com/pomerium/agentops/blob/main/deploy/harness/opencode/opencode.json#L1-L8)). Reject its baked image/adapters for Tusker V1; the local user owns those installations.

## Smallest Tusker V1 contract

Implement one runtime descriptor and one executor; do not build a second agent runtime.

```text
RuntimeDescriptor
  id, display_name
  transport: acp_stdio | cli
  command: user-selected executable (resolved absolute path)
  args: fixed adapter/CLI subcommand args
  capability_probe: initialize/session/new OR --version/auth probe
  policy_projection: named profile -> exact argv/env/config

LaunchSpec (per attempt)
  command_path, argv, cwd, env_delta
  requested_profile, effective_profile
  timeout, output_limit
  runtime_version, capability_summary, auth_state

Terminal receipt
  run_id, attempt_id, launch_spec_hash
  status: succeeded | failed | cancelled | timed_out
  exit_code, stop_reason, stdout/stderr paths, first_error
```

Required behavior:

1. Discover only user-installed commands (PATH, login-shell PATH, explicit absolute path). Never download or mutate the runtime.
2. Probe once, pin the resolved executable, and fail with an actionable `runtime_missing`, `adapter_missing`, `auth_missing`, or `probe_timeout` state.
3. For ACP, require the handshake and stream terminal `stopReason`; for CLI, capture exit status and structured output when available.
4. Compile a small fixed policy table per runtime. Reject unknown flags/config keys before spawn. Store requested/effective policy in the receipt.
5. Enforce one absolute deadline and bounded output. Kill the whole child tree on timeout.
6. Retry only infrastructure failures (spawn/probe/transport), with a bounded attempt count. Never replay a completed mutation merely because a receipt write failed.
7. Tusker, not the worker, owns task state and terminal closure. The worker may edit source and emit evidence; it must not update the canonical tracker.

### Explicit non-goals for V1

No bundled Codex/Goose/Claude runtime, no bundled ACP adapter, no installer/update/auth bootstrap, no hosted sandbox image, no LLM permission classifier, no generic “try every runner” fallback, and no worker-written Tusker state.
