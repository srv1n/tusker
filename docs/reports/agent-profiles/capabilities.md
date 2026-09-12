# Agent capabilities and discovery

**Observed:** 2026-09-09 (Asia/Kolkata)  
**Report schema:** `tusker.agent-capabilities/v1` (research contract; the
current CLI catalog is `tusker.runner-catalog/v1`)  
**Scope:** installed Codex and Muse routes, supported metadata discovery,
bounded access controls, and the boundary between setup checks and an explicit
profile test.

This report is based on the installed binaries, their local help and offline
protocol negotiation, the current Tusker adapter source, and the primary
vendor sources listed at the end. No model turn, `session/start`,
`turn/start`, credential mutation, install, daemon, or guessed flag was run.
Authentication and entitlement values are not copied into this report.

## Executive result

There are two distinct Muse-capable installations and they must not be
conflated:

1. **Tusker's current `muse` profile route** is the existing Codex CLI with
   the configured `muse` profile: `codex --profile muse exec`. It is a real
   CLI route, not ACP. The profile's local non-secret configuration names
   `muse-spark-1.3`, effort `high`, provider `meta`, and the Meta Responses
   endpoint. The installed Codex CLI accepts the profile for `exec`, but
   rejects it for `debug models`; profile-scoped model discovery is therefore
   **unsupported**. Keep the exact model and effort as manual, unverified
   values until an explicit test succeeds.
2. **Muse Code** is a separate installed executable, `muse` 1.0.3, exposing
   the Muse Session Protocol (MSP) over stdio. Its non-model `initialize` and
   `model/list` exchange succeeded. This is a real protocol surface, but the
   current Tusker provider-neutral transport enum has `cli` and `acp_stdio`,
   not `msp_stdio`; treating MSP as ACP would be an unsupported equivalence.
   Adding a first-class MSP adapter is a separate implementation decision.

Codex model discovery is available locally from the catalog bundled with the
installed Codex binary. A model list is metadata only: it does not establish
subscription entitlement, profile authentication, or successful execution.
There are no invented `gpt-5.x` rows in the discovery fallback in the current
`runner_catalog.go`; an empty or failed discovery remains empty/unsupported.

## Provenance and exact safe probes

| Installation | Executable and observed version | Identity evidence |
| --- | --- | --- |
| Codex CLI | `/Users/sarav/.bun/bin/codex`; `codex-cli 0.153.4` | SHA-256 `61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70` |
| Muse Code | `/Users/sarav/.local/bin/muse`; `Muse Code 1.0.3 (1.0.3-R2198.1)` | SHA-256 `21c66e550a71cac2e4af081cc33d10bec81993d0043ec492761fc449e6c440f6` |

The probes below are the exact commands used (all are metadata/help/status
operations):

```text
codex --version
codex --help
codex exec --help
codex debug models --bundled
codex login status
codex --profile muse --version
codex --profile muse exec --help
codex --profile muse debug models --bundled       # intentionally records unsupported

muse --version
muse --help
muse exec --help
muse schema --help
muse schema generate-json-schema --out <designated-temporary-directory>
muse serve --no-session-log --disable-shell --disable-write
```

`codex debug models --bundled` is the local-only form: the official reference
says `--bundled` skips refresh and prints only the catalog shipped with the
current binary. The non-bundled command is a separate refresh-capable path and
must remain metadata-only. `codex login status` printed `Logged in using
ChatGPT` and exited 0. That is an authentication-mode/status observation, not
an entitlement or execution proof.

The profile-scoped negative result is important. `codex --profile muse
debug models --bundled` exits non-zero with the installed CLI's message that
`--profile` applies only to runtime commands (including `codex exec`) and not
to this model-discovery command. `codex --profile muse exec --help` succeeds.
Likewise, `codex --profile muse login status` is not a valid profile-scoped
probe because `login` is outside the profile's accepted command set. Do not
substitute the generic Codex catalog for the Muse profile's admitted models.

## Capability matrix

The values below distinguish `unsupported` (the adapter cannot perform the
operation) from `unknown` (the surface exists but the value is not established
by a safe probe). `available` means only that the metadata operation or
executable was observed; it never means a model turn was authorized.

| Field | Codex CLI (`codex_exec`) | Muse profile (`muse`, Codex CLI route) | Muse Code (`muse`, MSP stdio) |
| --- | --- | --- | --- |
| Adapter identity | `codex`, `codex-cli 0.153.4` | provider label `muse`; executable is the same Codex CLI 0.153.4; profile `muse` | `muse`, `Muse Code 1.0.3` (`1.0.3-R2198.1`) |
| Transport | `cli`, Codex dialect | `cli`, Codex dialect; `--profile muse` | MSP over stdio (`msp_stdio`); not ACP |
| Safe version probe | `codex --version` | `codex --profile muse --version` (same version) | `muse --version` |
| Model discovery | **available**: `codex debug models --bundled`; JSON catalog parsed by `parseCodexModels` | **unsupported** for this installed CLI: profile is rejected by `debug models`; use manual exact model/effort and explicit test | **available**: `model/list` after MSP initialization; response source `providerCatalog` |
| Discovery freshness/source | Bundled: local to this binary, confidence medium; refresh-capable live source is distinct | `installed_help` only; no profile model catalog | Protocol response observed 2026-09-09; provider catalog, not local entitlement proof |
| Model display data | ID, display name, description, visibility, default-known flag, service tiers | No profile-scoped catalog; preserve manual values | ID, display label, provider/profile IDs, release date, context/output limits, active/default flags |
| Per-model reasoning | **known from catalog** where `efforts`/`reasoning_efforts`/`supported_reasoning_levels` are present; default is prepended from `default_reasoning_level` | **unknown from discovery**; local profile currently says `high` | **unknown per model** from `model/list`; CLI/schema exposes a closed global effort enum |
| Authentication status | `codex login status`: observed authenticated via ChatGPT | **unknown**: no profile-scoped status probe; current runner requires an explicit live canary | **unknown** from safe probes; `muse login` and `muse auth set --api-key-stdin` exist, but neither was invoked |
| Structured output | `codex exec --json` emits JSONL; no turn was run | Same Codex JSONL route | `muse exec --json` emits JSONL; `muse serve` is MSP JSON-RPC over stdio |
| Filesystem/sandbox | `read-only`, `workspace-write`, `danger-full-access` at the CLI; Tusker compiles bounded presets | Inherits Codex route; profile does not widen project policy | CLI/server options include `--disable-write`, `--disable-shell`, `--disable-sandbox`; server sandbox posture is fixed at host start |
| Network | Codex sandbox has workspace-write network boolean; Tusker presets are offline/network; full access is unrestricted | Same | `restricted`, `enabled`, or `proxy-only` (`proxy-only` is the local CLI default) |
| Approval | Official Codex reference: `untrusted`, `on-request`, `never`; installed 0.153.4 help exposed `on-request` and `never` | Same | `untrusted`, `on-request`, `never`; optional `approval-judge` `off`/`on` |
| Profile/config | `--profile <name>` for runtime commands | Existing profile config is the source of model/provider/effort; no secrets are copied | Provider/model/effort flags and MSP `session/setModel`/`session/setApprovalMode` methods |
| Setup versus test | Setup can use executable/version/auth status; a test is an explicit `codex exec` turn | Setup is executable/help only; auth remains unknown until explicit test | Setup can negotiate protocol and list models; auth/execution remains unknown until explicit test |

### Codex bundled model observation

The 2026-09-09 bundled catalog contained these rows. Efforts are listed in
catalog order; `*` marks the catalog's default effort. Visibility is retained
because a hidden row is not automatically a user-selectable model.

| Model ID | Display name | Efforts (default) | Visibility / service tier |
| --- | --- | --- | --- |
| `gpt-6-astra` | GPT-6-Astra | `low*`, `medium`, `high`, `xhigh`, `max`, `ultra` | visible; `priority` |
| `gpt-5.6-sol` | GPT-5.6-Sol | `low*`, `medium`, `high`, `xhigh`, `max`, `ultra` | visible; `priority`, `ultrafast` |
| `gpt-5.6-terra` | GPT-5.6-Terra | `low`, `medium*`, `high`, `xhigh`, `max`, `ultra` | visible; `priority` |
| `gpt-5.6-luna` | GPT-5.6-Luna | `low`, `medium*`, `high`, `xhigh`, `max` | visible; `priority` |
| `gpt-daybreak-blue-latest` | (no display label) | `low*`, `medium`, `high`, `xhigh`, `max`, `ultra` | hidden |
| `gpt-daybreak-red-latest` | (no display label) | `low`, `medium*`, `high`, `xhigh`, `max`, `ultra` | hidden |
| `gpt-5.5` | GPT-5.5 | `low`, `medium*`, `high`, `xhigh` | visible |
| `gpt-5.4` | (no display label) | `low`, `medium*`, `high`, `xhigh` | hidden; `priority` |
| `gpt-5.4-mini` | (no display label) | `low`, `medium*`, `high`, `xhigh` | hidden |
| `gpt-5.2` | GPT-5.2 | `low`, `medium*`, `high`, `xhigh` | visible |
| `codex-auto-review` | (no display label) | `low`, `medium*`, `high`, `xhigh`, `max` | hidden |

The raw catalog also carries fields such as API support and availability
metadata. Those fields were not treated as entitlement. A discovery adapter
must not invent a fallback model if this command errors or returns no
parseable rows.

### Muse profile configuration observation

The local profile configuration (observed without printing credential-bearing
values) contains:

```toml
model = "muse-spark-1.3"
model_provider = "meta"
model_reasoning_effort = "high"
model_reasoning_summary = "auto"
model_context_window = 1048576
model_supports_reasoning_summaries = true

[model_providers.meta]
name = "Meta Model API"
base_url = "https://api.meta.ai/v1"
wire_api = "responses"
```

This establishes the configured route and selected values, not whether the
account can execute them. The safe, exact runtime shape is the existing route
used by Tusker:

```text
codex --profile muse exec --json --skip-git-repo-check -
```

Tusker adds the selected model, effort, and permission policy through its
Codex compiler. Do not run that command as part of metadata refresh; it is an
explicit model test/execution operation.

### Muse Code MSP observation

`muse schema generate-json-schema` is offline and produces a schema exact for
the installed binary. The observed stable schema manifest was:

```json
{"experimental":false,"fingerprint":"sha256:03312c213efd14277a0e0a102f70adeae497a469ca4edf7242f479953ed758b7","schemaVersion":1}
```

The stable method index includes `initialize`, `model/list`, session methods,
turn methods, approval methods, view methods, and subagent methods. The
following safe handshake was sent to a host started with
`muse serve --no-session-log --disable-shell --disable-write`:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"clientInfo":{"name":"tusker_capability_research","version":"1"}}}
```

The host replied (the local `museHome` path is intentionally omitted):

```json
{"jsonrpc":"2.0","id":1,"result":{"serverInfo":{"name":"muse","version":"1.0.3"},"userAgent":"muse-build/1.0.3 (non-interactive; macos-aarch64; build <omitted>)","platformFamily":"unix","platformOs":"macos","schema":{"version":1,"fingerprint":"sha256:03312c213efd14277a0e0a102f70adeae497a469ca4edf7242f479953ed758b7"},"grantedCapabilities":[],"experimentalApi":false,"sessionDurability":"ephemeral"}}
```

After the required `initialized` notification:

```json
{"jsonrpc":"2.0","method":"initialized","params":{}}
{"jsonrpc":"2.0","id":2,"method":"model/list","params":{}}
```

The observed result was:

```json
{"jsonrpc":"2.0","id":2,"result":{"providerId":"meta","profileId":"tbh","source":"providerCatalog","models":[
  {"modelId":"muse-spark-1.3","displayLabel":"muse-spark-1.3","providerId":"meta","profileId":"tbh","releaseDate":"2026-09-02","description":null,"contextLimit":1007997,"outputLimit":128000,"cost":null,"isActive":false,"isDefault":false},
  {"modelId":"muse-spark-1.3-contributor","displayLabel":"muse-spark-1.3-contributor","providerId":"meta","profileId":"tbh","releaseDate":"2026-09-02","description":"Your content, including inter-session messages, may be used for product improvement.","contextLimit":1007997,"outputLimit":128000,"cost":null,"isActive":false,"isDefault":true},
  {"modelId":"muse-spark-1.2","displayLabel":"muse-spark-1.2","providerId":"meta","profileId":"tbh","releaseDate":"2026-08-05","description":null,"contextLimit":1007997,"outputLimit":128000,"cost":null,"isActive":false,"isDefault":false},
  {"modelId":"muse-spark-1.2-contributor","displayLabel":"muse-spark-1.2-contributor","providerId":"meta","profileId":"tbh","releaseDate":"2026-08-05","description":null,"contextLimit":1007997,"outputLimit":128000,"cost":null,"isActive":false,"isDefault":false}
]}}
```

`model/list` is a query and may legally return an empty array. It reports a
provider catalog and does not prove authentication or execution. The
response does not declare per-model effort support. The exact closed effort
enum in the installed CLI/schema is `none`, `minimal`, `low`, `medium`,
`high`, `xhigh`, and `ultra`; model-specific support must remain unknown until
the host declares it or an explicit test establishes it. The Meta product
announcement says Muse Spark 1.3 has maximum reasoning available in Muse
Code/Meta Model API, but that statement is not a substitute for a
profile-specific capability response.

## Typed report contract

The smallest useful persisted shape is a discriminated, versioned observation,
not a generic form schema:

```json
{
  "schema": "tusker.agent-capabilities/v1",
  "version": 1,
  "observed_at": "2026-09-09T00:00:00Z",
  "adapters": [
    {
      "id": "codex_exec",
      "provider": "codex",
      "version": "codex-cli 0.153.4",
      "transports": ["cli"],
      "discovery": {
        "state": "available",
        "source": "bundled",
        "checked_at": "2026-09-09T00:00:00Z"
      },
      "models": [
        {
          "id": "gpt-6-astra",
          "display_name": "GPT-6-Astra",
          "reasoning": {
            "state": "known",
            "values": ["low", "medium", "high", "xhigh", "max", "ultra"],
            "default": "low"
          }
        }
      ],
      "options": [
        {"id":"permission_preset","kind":"enum","values":["read-only","workspace-write-offline","workspace-write-network","danger-full-access"]}
      ]
    }
  ]
}
```

Required semantics:

- `discovery.state` must allow at least `available`, `unsupported`,
  `unknown`, `stale`, and `error`; `source`, `checked_at`, and `error` are
  retained independently.
- `reasoning.state = known` is different from a supported model with an
  unknown effort list. An unsupported discovery mechanism is different again.
- `options` are bounded discriminated values (`boolean`, `enum`, or a
  constrained scalar), translated by the owning adapter. Do not equate
  similarly named permission modes across vendors.
- A failed refresh must retain the last successful models and mark them
  `stale`; an empty/error response must not erase a saved manual model.
- Cache identity includes adapter ID, executable version, transport, and
  non-secret configuration context. Never serialize API keys, tokens, auth
  command output, or environment values.

The current `RunnerCatalog` source already has most of this shape: schema,
version, observation timestamp, harness identity/version, transport,
discovery state/source, checked time, models, per-model efforts, bounded
options, and bounded error strings (the current catalog error paths are
static). Its cache is keyed by harness, version,
transport, and context with a 24-hour TTL; a failed refresh retains the prior
entry as `stale`. One limitation to resolve before claiming full profile
freshness is that the current Muse cache context is the literal
`profile=muse`; it does not include a hash of the profile's non-secret model /
provider / effort configuration. A profile edit can therefore outlive the
cache TTL unless the caller invalidates it explicitly.

## Permission and test boundary

Tusker's provider-neutral policy presets are:

| Preset | Filesystem | Network | Approvals | Codex compilation |
| --- | --- | --- | --- | --- |
| `read-only` | read-only | disabled | deny | `--sandbox read-only`, `-c approval_policy="never"` |
| `workspace-write-offline` | workspace-write | disabled | deny | `--sandbox workspace-write`, `sandbox_workspace_write.network_access=false` |
| `workspace-write-network` | workspace-write | enabled | deny | `--sandbox workspace-write`, `sandbox_workspace_write.network_access=true` |
| `danger-full-access` | unrestricted | enabled | bypass | `--dangerously-bypass-approvals-and-sandbox` |

The Codex compiler also adds `--ignore-user-config`, `--strict-config`, an
exact selected `--model` when present, an exact `model_reasoning_effort`
configuration value when present, `--json`, and stdin `-`. The Muse profile
route inherits this compiler. Muse Code has a different vocabulary (`disable`
flags and approval/sandbox-network enums); no equivalence should be guessed.

Setup and Test are separate operations:

- **Check setup** may probe an executable, version, local help, non-model
  discovery, and (for generic Codex) `codex login status`. For the Muse
  profile, authentication remains unknown because the profile has no safe
  status probe. For MSP, initialize/model-list success is protocol/catalog
  proof only.
- **Run test** is an explicitly authorized, disposable model turn using the
  exact selected profile revision, model, effort, transport, and access
  options. The current conformance command defaults to a non-live check;
  `--live` and any exercise are explicit. Results are bound to the prepared
  executable/configuration identity and should become outdated after edits.

No explicit Test was run for this report. A future authorized test should use
the existing conformance path rather than an ad-hoc shell command. A safe
non-model discovery refresh must never call `exec`, `turn/start`, or any
equivalent model operation.

## Unsupported and deferred integrations

| Integration | Observed state | Decision |
| --- | --- | --- |
| Claude Code (“Cloud Code”) | No current Codex/Muse capability inventory was collected; the source catalog marks it future for this story | Deferred; do not expose a runnable profile from this report |
| OpenCode | No Tusker adapter/conformance route | Deferred |
| Cursor | No Tusker adapter/conformance route | Deferred |
| Devin | No Tusker adapter/conformance route | Deferred |

The generic `acp_stdio` transport in `internal/runner` is not evidence that
Muse Code speaks ACP. Bounded ACP additionally requires a verified native
containment boundary and a successful protocol handshake. MSP should remain a
separate transport until an adapter is deliberately implemented.

## Sources

All web sources below were checked on 2026-09-09. Local help/source observations
were made against the versions in the provenance table above.

### Primary vendor sources

- [OpenAI Codex CLI repository](https://github.com/openai/codex) — official
  CLI source and installation/sign-in entry point.
- [OpenAI Codex CLI command reference](https://developers.openai.com/codex/cli/reference)
  — official command/flag reference: `exec`, `--json`, `--model`, `--profile`,
  sandbox levels, `debug models --bundled`, and `login status` semantics.
- [Meta Research: Introducing Muse Spark 1.3](https://research.meta.ai/blog/introducing-muse-spark-1-3)
  — dated 2026-09-02 announcement; Muse Spark 1.3 availability in Muse Code
  and Meta Model API. It does not define local CLI or MSP behavior, so local
  help/schema is authoritative for those details.
- [Meta Muse Code SDK](https://github.com/meta-models/muse-code-sdk) — official
  developer-preview SDK for programmatic Muse Code control over the Muse
  Session Protocol, including protocol schemas and fixtures.
- [Meta Model Cookbook](https://github.com/meta-models/meta-model-cookbook) —
  official examples for the Meta Model API, OpenAI-compatible base URL/model
  identity, reasoning recipes, and Muse Code operational concepts. API
  examples were not executed here.

### Local source of truth in this checkout

- `cmd/tusker/runner_catalog.go`: `RunnerCatalog`/model/option types,
  Codex bundled discovery, Muse-profile unsupported discovery, timeout and
  cache behavior, and future integration entries.
- `cmd/tusker/runner_conformance.go`: exact Codex and Muse-profile command
  definitions and explicit `--live` conformance boundary.
- `internal/runner/types.go`: `cli`/`acp_stdio` transport enum, typed run
  input, prepared launch identity, policy, and event/receipt structures.
- `internal/runner/policy.go`: bounded permission presets and Codex argument
  compilation.
- `internal/runner/prepare.go`: version/executable identity, auth probes,
  profile-Muse unknown-auth behavior, timeout, and capability negotiation.
