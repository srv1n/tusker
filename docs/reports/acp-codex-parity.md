# Codex ACP parity slice

Status: opt-in, read-only provider integration with hermetic proof. The runner
factory, pre-claim admission, detached wrapper, exact handshake identity,
session configuration, and local install/doctor surfaces are wired. This is
**not** a claim that a real adapter is installed or authenticated, and it is
not safe to make Codex ACP the default runner yet.

## What this slice establishes

`cmd/tusker/runner_acp_codex.go` is the Codex-specific boundary around the
generic ACP v1 transport. It deliberately owns no daemon dispatch, task state,
lease authority, evidence acceptance, profile parsing, or legacy Codex runner
behavior.

| Concern | Contract |
| --- | --- |
| Identity | Reserved local identity is `codex_acp`; wiring must add it as a new persisted kind. Legacy `codex`, `codex_exec`, and `codex_cloud` records must never be reinterpreted. |
| Launch | Launch argv is returned only after immediate revalidation of an externally anchored complete-bundle receipt and exact binding to `[absolute-native-codex-acp]`. The provider descriptor alone cannot authorize launch. It rejects `npx`, shells, PATH lookup, symlinked files, and shebang launchers. Publisher authenticity remains an installer/release-policy input, not something inferred from a filename. |
| Bundled Codex only | External `CODEX_PATH` is unsupported and never forwarded. The production slice uses the compatible Codex bundled in the verified standalone adapter, eliminating a separately mutable child-executable seam. |
| Verified install | The local descriptor fingerprint commits to the adapter version, native adapter, and every declared runtime asset, but is not publisher identity. Launch additionally requires the shared bundle verifier's externally trusted manifest digest, exact provider/version/platform/native policy, complete sealed tree, content-addressed root binding, and immediate pre-spawn receipt revalidation. |
| Bootstrap settings | `CODEX_CONFIG` carries model/reasoning and `INITIAL_AGENT_MODE=read-only` is emitted through a positive environment allowlist. Writable/full-access mappings are refused in this parity slice. This is only bootstrap configuration. |
| Verified settings | Before prompt, integration must read advertised `configOptions`, require the exact official IDs `model`, `reasoning_effort`, and `mode` with the desired selectable values, call `session/set_config_option`, then require exact returned values from `VerifyApplied`. Aliases, missing options, and coercion fail admission. |
| Auth/readiness | The caller selects exactly one auth contract: ChatGPT session (`CODEX_HOME`), `CODEX_API_KEY`, or `OPENAI_API_KEY`. Only that source is forwarded. The non-secret method and principal digest are retained for correlation; credentials are not. Install, conformance, auth, and Tusker task authorization remain separate conditions. |
| Permission callbacks | The decoder accepts the official nested `toolCall` callback shape. Public `edit` callbacks lack a trustworthy file target and are denied; `execute` and `other` are also denied. Current parity is therefore conformant read-only only, and Codex ACP is blocked from becoming the production default. |
| Session references | A local correlation token binds provider session, adapter version/fingerprint, project, canonical workspace, profile, non-secret auth-principal digest, originating attempt, and work revision. Decode requires the expected binding. The token is not resume authority: shared integration must still verify the durable originating attempt and current authorization. |
| Stop semantics | `delivery_unknown` maps to failed with automatic retry and automatic resume both forbidden. Reissuing an ambiguous turn risks duplicate edits. |
| Updates | ACP updates are decoded as bounded observations and sent through `CodexExecutionAdapter` with the raw provider session only for ledger correlation. They cannot change a run, lease, task, proof, or terminal authority. |

## Upstream distribution reality

As of Codex ACP `v1.1.14`, the official ACP registry points to the npm package
`@agentclientprotocol/codex-acp@1.1.14`. The GitHub release publishes no native
binary assets, checksums, SBOM, or binary signature. The repository contains
Bun recipes for building standalone Darwin/Linux binaries, but those build
outputs are not publisher-hosted release artifacts.

Tusker therefore does not download or invoke npm/npx during an attempt. The
local `tusker acp install` command accepts an already-built native binary only,
requires its exact caller-supplied SHA-256, validates its host format and
architecture, seals it into a complete content-addressed bundle, and labels
publisher/source fields as `unverified_caller_metadata`. That hash proves which
bytes were installed; it does not prove who published them.

## Current runtime path

The concrete opt-in path now:

1. validates the configured bundle and explicit read-only profile before lease
   claim;
2. carries a non-secret exact provider plan through the private detached
   wrapper request;
3. reconstructs admission from the registered project's canonical workflow and
   binds the active attempt, lease generation, work revision, profile, model,
   effort, and workspace before launch;
4. immediately revalidates the complete sealed bundle, launches one absolute
   native argv without a shell, and forwards exactly one selected auth source;
5. requires ACP v1 plus exact `agentInfo.name=codex-acp` and configured adapter
   version, creates a fresh session, applies and verifies model/effort/read-only
   configuration, then sends the prompt; and
6. records bounded observations only. Permissions remain fail-closed, resume
   remains disabled, and `delivery_unknown` cannot retry or fall back.

The local doctor proves bundle integrity and selected auth-source presence only.
It deliberately reports neither workflow configuration nor authentication as
successful.

## Evidence

Hermetic tests under `runner_acp_codex_test.go` cover strict descriptor and
asset drift validation, shebang/symlink refusal, profile/model/mode mapping,
positive environment construction, config-option plan verification,
execute/edit/other broker normalization, session binding, delivery-unknown
recovery posture, observation-only bridging, and readiness separation.

The focused command is:

```text
go test ./cmd/tusker -run 'CodexACP' -count=1
```

Running that command proves the source and hermetic process contract only. The
remaining human-owned gate must build or otherwise obtain the exact native
adapter, independently pin its SHA-256 and source provenance, install it, select
one existing credential source, and authorize a disposable read-only live
smoke. Resume, writable permissions, automatic fallback, and default cutover
remain out of scope until separate parity and soak gates pass.
