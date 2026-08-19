# ACP and live-observability gate packet — 2026-08-18

This packet is the truthful handoff for `ACP-G-0001` through `ACP-G-0003` and
the execution-observability gap. It records source and offline evidence only.
No provider was contacted, no credential was read or printed, no gate was
satisfied or waived, and the runtime database was not mutated.

## Gate state

| Gate | Owner | Current state | Why it remains open |
|---|---|---|---|
| `ACP-G-0001` | `human:sarav` | open | Authenticated Codex ACP smoke is an account/spend decision and needs a durable receipt from the exact pinned bundle. |
| `ACP-G-0002` | `human:sarav` | open | The source tree has no maintained Claude ACP runner or live-smoke path. Generic ACP and legacy Claude observation tests are not Claude ACP parity proof. |
| `ACP-G-0003` | `human:sarav` | open | Default cutover and direct-runner deletion require both provider parity gates, soak, rollback, deletion, and an explicit human release decision. |

The tracker confirms all three gates are blocking human gates. `ACP-T-0005`,
`ACP-T-0006`, and `ACP-T-0008` remain held/backlog with pending verification;
their task contracts already own the missing work, so no replacement task was
created.

## Source facts

- Codex ACP is implemented and sealed-bundle admission is present.
- The installer and npm packager are Codex-only (`codex/codex-acp`). The bundle
  validator knows the future `claude/claude-agent-acp` descriptor shape, but
  that is validation policy, not an installed adapter.
- There is no `cmd/tusker/runner_acp_claude.go`, no Claude ACP runner factory
  route, no Claude ACP live-smoke test, and no Claude ACP receipt producer.
- Direct `claude-code` and `ClaudeExecutionAdapter` remain explicit fallback /
  observation paths. They must not be relabeled as Claude ACP parity.

## Offline evidence

These commands contact no provider. The first two passed on the final source
available to this lane; the broad command also passed. Results are focused
proof, not live-provider or release proof.

```text
go test ./cmd/tusker -run 'CodexACP|RunnerACP|ACPConformance' -count=1
PASS (4.646s)

go test ./cmd/tusker -run 'ClaudeExecutionAdapter|ExecutionObservability|RunnerACP|ACPConformance' -count=1
PASS (1.581s)

go test ./cmd/tusker -run 'ACP|Runner|CodexCloud|Authority' -count=1 -timeout 20m
PASS (102.376s)
```

The deterministic observability fixture is useful for schema/replay proof but
does not establish live provider visibility:

```text
go test ./cmd/tusker -run '^TestExecutionObservabilityDogfoodFixture$' -count=1
```

The current default runtime store is also empty at inspection time:

```json
{"graph":{"schema":"tusker.execution-graph/v1","nodes":[],"edges":[],"partial_visibility":false,"topology_partial":false},"ok":true}
{"executions":null,"ok":true,"project_id":"tusker"}
```

That is an observed absence of live execution/provider receipts, not a green
observability result.

## Codex human-run receipt path (`ACP-G-0001`)

Run only after the operator has selected the exact sealed bundle and accepts
the local authentication/account boundary. The test forwards the selected
auth source to the adapter; do not add credentials to the command or logs.

```bash
set -eu
repo=/Users/sarav/Downloads/side/tusker
vault="$repo/.tusker"
candidate=/private/tmp/tusker-prime-candidate
receipt=/private/tmp/tusker-codex-acp-live-smoke.json
test -x "$candidate"

TUSKER_LIVE_CODEX_ACP=1 \
TUSKER_LIVE_CODEX_ACP_VAULT="$vault" \
TUSKER_LIVE_CODEX_ACP_WRAPPER="$candidate" \
TUSKER_LIVE_CODEX_ACP_RECEIPT="$receipt" \
go test ./cmd/tusker -run '^TestLiveCodexACPPrimarySmoke$' -count=1 -timeout 10m

# Inspect only non-secret, gate-relevant fields.
jq '{schema,ok,canonical_revision,adapter_version,adapter_fingerprint,adapter_executable_fingerprint,bundle_receipt_digest,protocol,agent_name,agent_version,capabilities,profile,exit_code,outcome,duration_ms,log_path}' "$receipt"
```

The receipt producer records the source revision, manifest and executable
fingerprints, sealed-bundle receipt digest, negotiated protocol/agent and
capabilities, typed outcome, duration, and redacted log/status paths. A green
focused test without this receipt is not enough for the gate.

After independently reviewing the redacted receipt, the human owner may
record the gate (the command below is intentionally not run by an agent):

```bash
tusker gate satisfy ACP-G-0001 --by human:sarav \
  --evidence "Codex ACP receipt: $(jq -c '{canonical_revision,adapter_version,adapter_fingerprint,bundle_receipt_digest,protocol,agent_name,agent_version,capabilities,outcome,duration_ms,log_path}' "$receipt")"
```

## Claude gate (`ACP-G-0002`)

There is no honest live command to run against this source tree. Do not point
the Codex smoke at a Claude executable or treat `claude-code` output as ACP.
The gate requires a maintained, pinned `claude-agent-acp` bundle, a runner
route using the common ACP client, a Claude-specific conformance suite, and an
authenticated opt-in smoke that emits the same receipt fields. `ACP-T-0006`
already owns that implementation and proof contract.

The exact blocker can be rechecked without touching runtime state:

```bash
test ! -e cmd/tusker/runner_acp_claude.go
test ! -e cmd/tusker/runner_acp_claude_test.go
test ! -e cmd/tusker/runner_acp_claude_live_smoke_test.go
```

## Cutover gate (`ACP-G-0003`)

Do not satisfy this gate from the current evidence. It remains blocked until
both provider receipts exist and a human has reviewed default-on soak,
new-attempt-only rollback, direct-runner deletion, `codex_cloud` separation,
and the final clean validation. A gate record, if later authorized, must be
created by the owner:

```bash
tusker gate satisfy ACP-G-0003 --by human:sarav \
  --evidence "Exact revision, accepted Codex and Claude receipts, soak/rollback/deletion report, and final validation reviewed: <human-supplied packet>"
```

## Live execution-observability handoff

The graph query surface is already deterministic and read-only:

```bash
tusker execution list --json
tusker execution inbox --json
tusker execution show --id exec_<redacted> --json
```

For a real operator-directed run, register and attach the provider session
before launch, then inspect the graph and timeline through Serve. Provider
session IDs and credentials must remain out of reviewer prose:

```bash
tusker execution register --source direct_codex --provider codex --name "ACP observability smoke" --json
tusker execution attach --id exec_<redacted> --provider codex --provider-session-id <redacted> --json
tusker execution list --provider codex --json
```

These commands create runtime records and therefore require explicit operator
authorization; they were not run for this packet. Empty graph output remains
the current evidence boundary.

## Reviewer conclusion

The offline ACP safety boundary and Codex implementation are reviewable. The
open items are not cleanup tasks an agent can truthfully close: two are
human-authenticated provider/account gates, one is a human default-cutover
decision, Claude ACP itself is not implemented, and the live observability
store has no provider observations. The next safe move is either (a) the
human runs the Codex receipt path and retains the result, or (b) the owner
defers the ACP gates while `ACP-T-0006` implements Claude ACP. No historical
event, gate, or runtime row should be edited to make these appear complete.
