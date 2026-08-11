# Codex ACP parity slice

Status: source-level provider contract and hermetic proof. It is **not** a
claim that Codex ACP is installed, authenticated, selected by the runner
factory, or safe to make the default runner yet.

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

## Required integration seam

The current generic ACP runtime has a single executable fingerprint on
`StartRequest`, no pinned-bundle receipt, no provider-specific session wrapper,
and no `configOptions`/`session/set_config_option` API. Do **not** paper over
that by weakening the generic process checks or by treating environment values
as verification.

The next shared integration needs to:

1. accept a verified generic bundle receipt whose fingerprint covers the native
   adapter with bundled Codex and the complete declared dependency set, then
   bind it to the start request; external `CODEX_PATH` must remain refused;
2. preserve the separate `codex_acp` runner kind through factory validation,
   dispatch, wrapper execution, persistence, and status/event logging;
3. pass the descriptor's exact argv and positive environment to the fenced ACP
   process runtime without falling back to a shell or installer;
4. expose `configOptions` and `session/set_config_option` in the ACP client,
   execute and verify `CodexACPConfigPlan` before the first prompt;
5. use `EncodeSessionRef` only after a negotiated `session/load` or
   `session/resume` capability, and use `DecodeSessionRef` with a binding built
   from a durable originating-attempt lookup before either operation; the
   encoded value alone never authorizes resume; and
6. wire the provider permission decoder to the already-resolved
   `ACPPermissionPolicy`, preserving rejection for execute/edit/other and
   read-only admission until trustworthy target schemas and separately
   reviewed authorities exist.

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

Running that command proves the source contract only. A later human-owned live
gate must use an exact preinstalled adapter release, authenticate it, retain
the conformance/smoke receipt, and exercise fresh, load/resume, permission,
interrupt, malformed, crash, and ambiguous-delivery paths.
