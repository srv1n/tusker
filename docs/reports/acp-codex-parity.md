# Codex ACP parity slice

Status: local-primary Codex ACP integration with hermetic proof plus
authenticated read-only and workspace-write smokes on the development machine. The runner factory,
pre-claim admission, detached wrapper, exact handshake identity, session
configuration, workspace-write permission broker, npm bootstrap packager, and
local setup/doctor surfaces are wired. This is a local operating posture, not
a public-distribution or unattended-release claim.

## What this slice establishes

`cmd/tusker/runner_acp_codex.go` is the Codex-specific boundary around the
generic ACP v1 transport. It deliberately owns no daemon dispatch, task state,
lease authority, evidence acceptance, profile parsing, or legacy Codex runner
behavior.

| Concern | Contract |
| --- | --- |
| Identity | Reserved local identity is `codex_acp`; wiring must add it as a new persisted kind. Legacy `codex`, `codex_exec`, and `codex_cloud` records must never be reinterpreted. |
| Launch | Launch argv is returned only after immediate revalidation of an externally anchored complete-bundle receipt and exact binding to either one native executable or `[absolute-node, absolute-entrypoint]`. The provider descriptor alone cannot authorize launch. It rejects attempt-time `npx`, shells, PATH lookup, symlinked files, and package-manager launchers. |
| Bundled Codex only | External `CODEX_PATH` is unsupported and never forwarded. The npm packager seals the exact compatible `@openai/codex` platform package and binds each native support executable in the manifest. |
| Verified install | The local descriptor fingerprint commits to adapter version, interpreter/entrypoint, and every declared runtime asset. Launch additionally requires the complete sealed tree, host platform, exact manifest/root receipt, and immediate pre-spawn revalidation. This proves local bytes, not publisher identity. |
| Bootstrap settings | `CODEX_CONFIG` carries model/reasoning plus a fail-closed child-process environment policy: core variables only, normal secret exclusions enabled, `OPENAI_API_KEY`/`CODEX_API_KEY`/`CODEX_HOME` explicitly excluded, `allow_login_shell=false`, and the pinned Codex key `experimental_use_profile=false`. Codex itself receives the one selected auth source, but model-invoked commands do not. `INITIAL_AGENT_MODE` is explicitly `read-only` or `agent` from the resolved profile; `agent-full-access` remains refused. Bootstrap mode never grants Tusker authority. |
| Verified settings | Before prompt, integration must read advertised `configOptions`, require the exact official IDs `model`, `reasoning_effort`, and `mode` with the desired selectable values, call `session/set_config_option`, then require exact returned values from `VerifyApplied`. Aliases, missing options, and coercion fail admission. |
| Cancellation and streaming bounds | Prompt publication is linearized before the active call is cancellable, so `session/cancel` cannot overtake `session/prompt` on the wire. The ACP queue remains bounded at 256 unread updates, while a separate 32 MiB cumulative update-byte budget resets per prompt. The detached wrapper receives the daemon's first interrupt, drains ACP cancellation and terminal status for up to the configured window, and only then permits process-group escalation. |
| Crash recovery | Native and npm installers seal and validate the complete content-addressed tree before writing its receipt. A later identical setup can reconstruct a missing receipt only after independently revalidating source identity, manifest, modes, tree, and root digest; divergent or incomplete roots are rejected. |
| Auth/readiness | The caller selects exactly one auth contract: ChatGPT session (`CODEX_HOME`), `CODEX_API_KEY`, or `OPENAI_API_KEY`. Only that source is forwarded. The non-secret method and principal digest are retained for correlation; credentials are not. Install, conformance, auth, and Tusker task authorization remain separate conditions. |
| Permission callbacks | The pinned adapter's official nested `toolCall` shapes are normalized into read/write/execute/network requests. Workspace-write operations require exact attempt/session binding, canonical workspace confinement, policy and budget allowance, and an offered `allow_once`; unknown shapes and full access fail closed. |
| Session references | A local correlation token binds provider session, adapter version/fingerprint, project, canonical workspace, profile, non-secret auth-principal digest, originating attempt, and work revision. Decode requires the expected binding. The token is not resume authority: shared integration must still verify the durable originating attempt and current authorization. |
| Stop semantics | `delivery_unknown` maps to failed with automatic retry and automatic resume both forbidden. Reissuing an ambiguous turn risks duplicate edits. |
| Updates | ACP updates are decoded as bounded observations and sent through `CodexExecutionAdapter` with the raw provider session only for ledger correlation. They cannot change a run, lease, task, proof, or terminal authority. |

## Upstream distribution reality

As of Codex ACP `v1.1.14`, the official ACP registry points to the npm package
`@agentclientprotocol/codex-acp@1.1.14`. The GitHub release publishes no native
binary assets, checksums, SBOM, or binary signature. The repository contains
Bun recipes for building standalone Darwin/Linux binaries, but those build
outputs are not publisher-hosted release artifacts.

Tusker therefore does not download or invoke npm/npx during an attempt. For
local development, the operator first performs an explicit exact-version npm
install (or `npx` discovery), then runs `tusker acp setup --npm-prefix ...`.
Setup walks the complete pinned dependency closure, preserves and validates
the platform Codex executables, seals Node plus all runtime bytes, runs the
sealed adapter's exact `--version` probe, and writes machine-local primary
configuration. `tusker acp install` remains the separate native-artifact path.

## Current runtime path

The concrete local-primary path now:

1. validates the configured bundle and resolved read-only or workspace-write profile before lease
   claim;
2. carries a non-secret exact provider plan through the private detached
   wrapper request;
3. reconstructs admission from the registered project's canonical workflow and
   binds the active attempt, lease generation, work revision, profile, model,
   effort, and workspace before launch;
4. immediately revalidates the complete sealed bundle, launches exact absolute
   argv without a shell/package manager, and forwards exactly one selected auth source;
5. requires ACP v1 plus exact `agentInfo.name=@agentclientprotocol/codex-acp`
   and configured adapter version, creates a fresh session, applies and verifies model/effort/mode
   configuration, then sends the prompt; and
6. records bounded observations only. Permissions remain one-shot and
   fail-closed, resume remains disabled, and `delivery_unknown` cannot retry or
   fall back to the direct runner.

The review packaging path is also provenance-bound: a clean worktree is
required by default; `--allow-dirty` is explicit and records `HEAD`, requested
base, merge-base, porcelain-v2 state, per-file source classification, and
SHA-256 digests, with an embedded post-extraction verifier. This archive is a
review handoff, not a release artifact or publisher-signing claim.

The local doctor proves bundle integrity and selected auth-source presence only.
It deliberately reports neither workflow configuration nor authentication as
successful.

## Evidence

Hermetic tests cover strict descriptor and
asset drift validation, shebang/symlink refusal, profile/model/mode mapping,
positive environment construction, config-option plan verification,
execute/edit/other broker normalization, workspace confinement, session binding, delivery-unknown
recovery posture, observation-only bridging, and readiness separation.

The focused command is:

```text
go test ./cmd/tusker -run 'CodexACP' -count=1
```

The local machine additionally passed all three variants of the environment-gated
`TestLiveCodexACPPrimarySmoke`: exact sealed Node/adapter/Codex launch,
authenticated initialize, config negotiation, prompt, and durable terminal
status. The workspace-write mode created and verified one exact file inside a
disposable workspace using `execute-standard`; the read-only mode used
`review-independent` without tools. A second workspace-write smoke ran a bounded
helper and proved `OPENAI_API_KEY`, `CODEX_API_KEY`, and `CODEX_HOME` were all
absent from the model-invoked process while ChatGPT-session authentication still
succeeded. Direct `codex_exec` remains an explicit
emergency profile only; there is no automatic fallback after prompt delivery.
Resume and public-distribution gates remain separate future work.
