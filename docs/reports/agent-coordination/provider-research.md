---
title: "Provider coordination research: Codex and Muse"
subject: provider-coordination
status: codex-and-muse-live-execution-qualified
observed_at: 2026-09-10
---

# Provider coordination research: Codex and Muse

This report answers one narrow question: which session and coordination
interfaces are actually callable for Tusker today? It distinguishes documented
provider behavior from the routes Tusker calls locally, and from behavior that
still needs a live model-turn qualification.

Claude and ACP are outside this report; they remain future adapters and no
capability claim is made for them here.

## Verdict

1. **Codex app-server is directly callable by Tusker over JSON-RPC.** The
   documented protocol supports starting, resuming, forking, reading and
   listing threads; starting, steering and interrupting turns; and injecting
   Responses API items into a loaded thread without starting a user turn. The
   server streams lifecycle and item events over the same connection.
2. **Codex CLI is the stable, currently implemented execution route.**
   `codex exec --json` emits machine-readable JSONL and a session can be
   resumed with `codex exec resume --last` or an exact session ID. A CLI resume
   takes a follow-up prompt and therefore starts model work; it is not a
   documented zero-turn mailbox append.
3. **Muse is not an app-server route in this installation.** Tusker invokes
   `codex --profile muse exec --json --skip-git-repo-check -`, using the Codex
   CLI compiler with a Muse profile. The installed CLI accepts the profile for
   runtime commands but rejects `--profile muse` on `codex app-server`.
4. **Tusker must own durable coordination state.** Provider calls are delivery
   attempts, not the source of truth. The adapter contract should distinguish
   `resume_session`, `active_turn_steer`, and `zero_turn_history_inject`; do not
   infer any of these from model discovery or `--help` output alone.

## Capability matrix

| Capability | Official Codex contract | Tusker-callable route observed | Live qualification still required |
| --- | --- | --- | --- |
| Start a session/thread | `thread/start` | App-server JSON-RPC; CLI `exec` creates a session | Turn execution and persisted identity |
| Re-open a session after restart | `thread/resume`; CLI `exec resume` | App-server protocol; CLI resume | Resume after process restart/retry with the exact ID |
| Append work to an active turn | `turn/steer` with `expectedTurnId` | App-server only | Correct owner, race and stale-turn behavior |
| Append history without a user/model turn | `thread/inject_items` | App-server only; experimental surface requires opt-in | Persistence, ordering and provider acceptance |
| Interrupt a turn | `turn/interrupt` | App-server only in the documented coordination surface | Cancellation and retry semantics |
| Stream status/items | JSON-RPC notifications and JSONL events | App-server and `codex exec --json` | Reconnect, deduplication and event correlation |
| Muse execution | No separate Codex app-server profile route documented | `codex --profile muse exec --json` | Explicit live Muse conformance/canary |
| Provider-native cross-session message | No documented Codex CLI “send message to another session” method | Not available as a direct Tusker call | Do not design around an undocumented endpoint |

## Codex app-server: documented callable surface

The official [Codex App Server documentation](https://learn.chatgpt.com/docs/app-server)
describes a JSON-RPC 2.0 server. The default transport is JSONL over stdio;
WebSocket and Unix-socket transports are also documented. The lifecycle is an
`initialize` handshake followed by `initialized`, then thread and turn calls.
The documentation calls WebSocket experimental and says the app-server surface
is for local development/debugging rather than production deployment.

The useful coordination primitives are:

- `thread/start` creates a thread and returns its provider-owned identity.
- `thread/resume` re-opens an existing thread by ID and can apply supported
  runtime overrides. This is the durable restart/retry primitive; read the
  returned `thread.sessionId` rather than deriving one.
- `thread/fork` creates an ephemeral fork from a thread, optionally from a
  specified last turn. It is useful for isolation, not for delivering a reply
  to the original owner.
- `thread/read` reads a stored thread without resuming/loading it, and
  `thread/list`/`thread/loaded/list` expose discovery and loaded-state views.
- `turn/start` sends input to a thread and starts model work. Turn input can
  include text, local image/file references, and tool output as specified by
  the protocol.
- `turn/steer` appends input to an active turn. It requires both `threadId` and
  `expectedTurnId`; it fails if no matching turn is active and does not start a
  new turn or accept turn-level overrides.
- `turn/interrupt` interrupts an active turn.
- `thread/inject_items` appends raw Responses API items to a loaded thread's
  history without starting a user turn. This is the closest documented
  provider primitive to zero-turn message bookkeeping, but it is not a
  notification/wakeup API and must not replace Tusker's durable mailbox.

The server emits thread/turn/item notifications, including completion and
failure status. The documentation says clients should initialize once per
connection and opt into experimental methods/fields with
`initialize.params.capabilities.experimentalApi`; otherwise unsupported
experimental methods/fields are rejected. Therefore Tusker should pin the
protocol/schema version it speaks and fail closed when a required method is not
advertised or accepted.

Sources: [app-server lifecycle and transports](https://learn.chatgpt.com/docs/app-server#lifecycle-overview),
[app-server API overview](https://learn.chatgpt.com/docs/app-server#api-overview),
[start/resume a thread](https://learn.chatgpt.com/docs/app-server#start-or-resume-a-thread),
[start a turn](https://learn.chatgpt.com/docs/app-server#start-a-turn), and
[experimental API opt-in](https://learn.chatgpt.com/docs/app-server#experimental-api-opt-in).

### Local app-server probe

Installed binary: `/Users/sarav/.bun/bin/codex`, version `codex-cli 0.153.4`.

The following metadata-only probe was run with delayed JSONL writes so the
server could process each request:

```text
initialize -> userAgent Codex Desktop/0.153.4 ...
             codexHome /Users/sarav/.codex
model/list -> gpt-6-astra (default), gpt-5.6-sol, gpt-5.6-terra,
             gpt-5.6-luna, gpt-5.5, ...
```

No model turn was started. A second probe started an ephemeral thread with
`thread/start`, then observed it in `thread/loaded/list`; again, no model turn
was started. The installed protocol accepted the sandbox spelling
`read-only`. An earlier probe using `readOnly` was rejected with “unknown
variant,” which is a concrete version-drift warning: use the installed
generated schema/live handshake as the authority instead of copying an example
field spelling blindly from a newer documentation page.

The installed CLI can generate the protocol schema without a model turn:

```sh
codex app-server generate-json-schema --out /tmp/tusker-codex-app-schema
```

The generated v2 schema includes `ThreadStartParams`, `ThreadResumeParams`,
`TurnStartParams`, `TurnSteerParams`, and `ThreadInjectItemsParams`. Their
required IDs and fields match the method semantics above, subject to the
version-drift warning.

## Codex CLI: stable execution and resume

The official [non-interactive mode documentation](https://learn.chatgpt.com/docs/non-interactive-mode)
documents `codex exec` for scripts and CI. `--json` writes newline-delimited
JSON events to stdout, including thread/turn/item lifecycle events; the final
agent message is available as an output event or through `-o/--output-last-message`.
`--ephemeral` avoids persisted rollout state.

The documented resume forms are:

```sh
codex exec resume --last "fix the race conditions you found"
codex exec resume SESSION_ID "continue from the saved session"
```

The CLI help probe on `codex-cli 0.153.4` matches that surface:

```text
codex exec [OPTIONS] [PROMPT]
codex exec resume [OPTIONS] [SESSION_ID] [PROMPT]
  --json
  --output-schema <PATH>
  --ephemeral
  --profile <NAME>
  --sandbox <read-only|workspace-write|danger-full-access>
```

The CLI documentation exposes execution, resume, fork and review commands; it
does not expose a direct “send a message to another running session” command.
That absence is a limitation of the documented CLI surface, not proof that an
undocumented internal endpoint cannot exist. Tusker should therefore model CLI
resume as a new provider turn with ordinary cost/latency and retain its own
outbox, idempotency key and receipt state.

Sources: [non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode),
[developer command reference](https://learn.chatgpt.com/docs/developer-commands?surface=cli).

## Muse as configured and routed in this repository

### Execution route

Tusker's canonical runner definition is in `cmd/tusker/runner_conformance.go`:

```text
provider:   muse
dialect:    codex
executable: codex
command:    codex --profile muse exec --json --skip-git-repo-check -
transport:  cli
```

The same route is documented in `docs/system/runners-and-acp.md`: Muse uses
the same CLI compiler as Codex, the operator owns the profile/authentication,
and it is not ACP. `internal/runner/prepare.go` deliberately refuses to claim
non-live Muse work because the profile authentication has no noninteractive
probe; it returns `auth_missing` with the instruction to run live conformance.
That is a fail-closed admission rule, not evidence that the configured token is
invalid.

The installed profile file (secret-free fields) is:

```toml
model = "muse-spark-1.3"
model_provider = "meta"
model_reasoning_effort = "high"
model_context_window = 1048576

[model_providers.meta]
name = "Meta Model API"
base_url = "https://api.meta.ai/v1"
wire_api = "responses"

[model_providers.meta.auth]
command = "/Users/sarav/.local/bin/muse-oauth-token"
timeout_ms = 30000
refresh_interval_ms = 300000
```

The auth command is executable and returns an opaque bearer token; the token
was not printed or recorded here.

### Why Muse is not app-server-callable here

The installed CLI accepts `codex --profile muse exec --help`, but
`codex --profile muse app-server --stdio` is rejected before the server starts:

```text
Error: --profile only applies to runtime commands and `codex app-server` ...
```

Consequently, Tusker cannot claim that Muse inherits app-server
`thread/resume`, `turn/steer`, or `thread/inject_items`. The only presently
configured Muse execution interface is Codex CLI `exec`; any coordination
delivery through Muse must use the durable Tusker mailbox plus the CLI resume
route, unless a future Muse-native adapter is separately implemented and
qualified.

### Muse model discovery is not execution

`cmd/tusker/runner_catalog.go` performs two metadata checks:

1. It reads the Muse profile, runs its auth command, and lists the configured
   Responses API `/models` endpoint.
2. It starts `muse serve --no-session-log` and calls its JSON-RPC
   `initialize`/`initialized`/`model/list` interface, then intersects the two
   catalogs.

This powers the model picker only. It does not prove a model turn, session
resume, cross-session message delivery, or retry behavior. The current catalog
probe reported:

```text
Muse Code 1.1.1 (1.1.1-R2514.1)
default model: muse-spark-1.3
models: muse-spark-1.3, muse-spark-1.2, contributor
authentication: authenticated (discovery path)
discovery: muse_server:model/list + muse_profile:provider/models
```

The discovery result is useful evidence that the installed route and catalog
are present, but it is intentionally weaker than live execution conformance.

## Tusker probe ledger

Metadata probes were run on 2026-09-10. The two rows explicitly marked live
were run on 2026-09-11 and did start one bounded model turn each.

| Probe | Result | Meaning |
| --- | --- | --- |
| `command -v codex; codex --version` | `/Users/sarav/.bun/bin/codex`; `codex-cli 0.153.4` | Codex executable/version pinned |
| `codex login status` | `Logged in using ChatGPT`; exit 0 | Codex CLI auth status is available |
| `codex app-server --help` | experimental JSON-RPC server; stdio/unix/ws/off transports | App-server binary surface is installed |
| delayed `initialize` + `model/list` over stdio | success; live model catalog returned | Direct app-server callability proven without a turn |
| `codex app-server generate-json-schema` | v2 schema bundle generated | Exact installed request/field contract is inspectable |
| `codex --profile muse exec --help` | success | Muse profile is accepted by CLI runtime |
| `codex --profile muse app-server --stdio` | rejected before start | Muse profile cannot be attached to app-server |
| `tusker runner catalog --json` | Codex and Muse available; Codex 0.153.4; Muse 1.1.1 | Discovery only; conformance not checked |
| `tusker runner conformance --harness codex_exec --preset read-only --json` | admission/auth/configuration pass; `live_canary: not_run`; `ready: false` | Correctly separates local readiness from live proof |
| `tusker runner conformance --harness muse --preset read-only --json` | blocked at `auth_missing`; “run live conformance” | Muse non-live admission is intentionally fail-closed |
| `tusker runner conformance --harness codex_exec --preset read-only --live --exercise print --json` | PASS; ready; all cases pass; executable identity `sha256:65a9...`; valid until 2026-09-11T18:32:23Z | Installed Codex CLI execution is live-qualified |
| `tusker runner conformance --harness muse --preset read-only --live --exercise print --json` | PASS; ready; all cases pass; provider `muse`, dialect `codex`, transport `cli`; valid until 2026-09-11T18:32:48Z | Installed Muse profile execution and read-only enforcement are live-qualified |

The conformance command may return a non-zero process status when its report is
blocked; the report itself is the authority for the admission reason. Do not
reinterpret the Muse `auth_missing` admission as a failed credential check.

## What the architect should build against

Persist provider-neutral coordination first, then select the strongest
qualified delivery primitive:

| Tusker operation | Codex app-server | Codex CLI | Muse route today |
| --- | --- | --- | --- |
| Wake/reopen an owner | `thread/resume` | `exec resume SESSION_ID` | same CLI resume |
| Deliver to an active owner | `turn/steer` with expected turn ID | no documented direct equivalent | no documented direct equivalent |
| Record a reply without another model turn | `thread/inject_items` (experimental) | no documented equivalent | no documented equivalent |
| Correlate after replacement/retry | Tusker durable contact/session mapping | Tusker durable mapping + CLI session ID | Tusker durable mapping + CLI session ID |

The provider adapter should report capability bits only after protocol
negotiation and, for Muse, a live canary. In particular:

- `thread/read` is observation, not resume and not delivery.
- `turn/steer` is only valid while the expected turn is active.
- `thread/inject_items` changes provider history but does not wake an owner;
  Tusker still needs its own wake/notification state machine.
- A model catalog, authenticated discovery helper, or successful `--help`
  probe must never be promoted to “resume proven.”
- CLI resume and any future provider delivery must be idempotent at the Tusker
  outbox/receipt layer, because provider retries can create duplicate turns even
  when the logical message is unchanged.

## Remaining app-server qualification

The following are intentionally open and should be tracked as qualification,
not silently assumed from the metadata probes:

1. For Codex app-server, qualify `thread/resume` after process restart and
   retry, including a stale/missing thread response.
2. Qualify `turn/steer` during an active turn and verify stale
   `expectedTurnId` rejection.
3. If zero-turn provider history injection is adopted, qualify
   `thread/inject_items` with duplicate delivery and reconnects; otherwise keep
   it disabled and use the Tusker mailbox as the canonical record.
4. Verify that all provider events are correlated by provider thread/turn/item
   IDs and remain deduplicable across reconnects.

The honest boundary is: **Codex and Muse live CLI execution are qualified;
Codex active-turn steering is protocol-tested against the official request
shape but not yet live-certified; Muse remains CLI-only.**
