---
subject: agent-profiles-acceptance
status: partial
candidate: 4b5d2076-dirty
---

# Agent Profiles acceptance report

## Verdict

Candidate-source proof passes for catalog provenance, profile/tier persistence,
revision-bound profile tests, unavailable legacy routing, and the rendered
Agents UI. Installed-runtime proof is intentionally **blocked**: the resident
daemon is idle but serves the installed `tusker` binary rather than this dirty
candidate. No install or daemon replacement was performed.

## Baseline and runtime

| Item | Observed |
| --- | --- |
| Candidate | `v0.0.0-20260909115049-4b5d20769b6c+dirty`, SHA-256 `2c08d6f49e8ff89f35c16d7df082707bef2769114e5c01fb344551d44bae2216`; source candidate served on `127.0.0.1:7421` without installation |
| Installed CLI | `/Users/sarav/.local/bin/tusker`, `archive/pre-convergence-main-20260727-420-g4b5d2076-dirty` |
| Resident daemon | alive at `127.0.0.1:7420`, zero active runs |
| WUX reconciliation | `WUX-T-0010`, `WUX-T-0011`, and `WUX-T-0012` dry-runs all returned `changed=false`; all remain held/disarmed |

## Discovery and route truth

| Agent | Transport | Discovery | Execution claim |
| --- | --- | --- | --- |
| Codex | CLI | current installed catalog, live provenance; list is metadata only | explicit conformance required |
| Muse profile | Codex CLI with `--profile muse` | unsupported; exact manual model/effort remains unverified | real route is the configured Codex profile, not ACP/MSP |
| Claude Code, OpenCode, Cursor, Devin | future | unsupported | not selectable; retained legacy Claude assignments are visibly unavailable |

Profile tests persist the exact saved configuration revision in the existing
conformance cache. A profile edit invalidates that observation, so reloads no
longer claim an old test is current. The rendered list also labels legacy
`gpt-5.x` values as needing verification without rewriting their stored value.

## Evidence

| Check | Result |
| --- | --- |
| `go test ./cmd/tusker -run '^(TestModelLevels|TestAgentProfileMigration|TestAgentProfileTestIdentity|TestServeModelLevels)' -count=1` | PASS: 11 tests |
| `go test ./internal/runner/... -count=1` | PASS: 7 tests |
| `bun test tests/profiles-interaction.test.ts` | PASS: add/save Codex profile with Light and Standard eligibility |
| `bun run typecheck && bun run build` | PASS |
| candidate `runner conformance --harness codex_exec --preset read-only --json` | PASS: structured Codex CLI configuration/auth/policy projection; `live=false`, no model turn |
| source candidate visual review | Profiles: 7.5/10 before corrections; fresh final desktop/390px review: 8.5/10, with no material remaining fixes. |

The source candidate was rendered at `http://127.0.0.1:7421/settings` at
desktop width; that temporary preview has been stopped. The installed UI remains at
[Settings](http://127.0.0.1:7420/settings); it is not proof of this candidate.
The candidate screenshot before the corrective pass showed ambiguous duplicate
profile labels, raw access presets, and a future Claude profile presented as
ordinary. The after view adds effort/legacy disambiguators, human-readable
access labels, explicit verification state, and an `Unavailable` Claude route.

## Repeatable handoff

Run the offline, no-provider gate:

```sh
scripts/test-installed-runner-conformance.sh
```

Build a candidate without installing it, then reseed an idle manual fixture:

```sh
mkdir -p /private/tmp/tusker-agent-profile-candidate
go build -o /private/tmp/tusker-agent-profile-candidate/tusker ./cmd/tusker
TUSKER_CANDIDATE=/private/tmp/tusker-agent-profile-candidate/tusker \
scripts/test-real-work-project.sh \
  --repo /private/tmp/tusker-agent-profiles-manual \
  --mode offline --stage seed-only \
  --proof-dir /private/tmp/tusker-agent-profiles-proof
```

Verified seed-only fixture: `/private/tmp/tusker-agent-profiles-manual.VWGKbC`
with proof at `/private/tmp/tusker-agent-profiles-proof.sAGBOS` and state at
`/private/tmp/tusker-agent-profiles-state.RFVnwZ`. It contains thirteen
unstarted tasks and no leases. To idempotently reseed that exact fixture:

```sh
TUSKER_STATE_ROOT=/private/tmp/tusker-agent-profiles-state.RFVnwZ \
TUSKER_CANDIDATE=/private/tmp/tusker-agent-profile-candidate/tusker \
scripts/test-real-work-project.sh \
  --repo /private/tmp/tusker-agent-profiles-manual.VWGKbC \
  --mode offline --stage seed-only \
  --proof-dir /private/tmp/tusker-agent-profiles-proof.sAGBOS
```

The live conformance button sends its selected profile through the resident
daemon's `/api/runner/conformance` endpoint. Run it only after the candidate
has been installed by its runtime owner and a paid/provider-backed check is
explicitly authorized. It must report the selected profile revision, transport,
model, effort, and access; a blocked Muse result is a blocker, not a pass.

## Remaining blockers

- No authorized candidate install/restart means no installed daemon-backed
  Codex or Muse live turn was run.
- The seeded fixture is offline and proves lifecycle identity/review mechanics,
  not a provider invocation or authentication entitlement.

## 2026-09-09 Codex installed App Server discovery acceptance

The profile form was rebuilt and served from a temporary candidate at
`http://127.0.0.1:7422/settings`, while its Codex catalog came from the
installed `codex app-server --stdio` in the same default Codex configuration
and credential context as `codex_exec`. The resident installed Tusker daemon
on port 7420 was not restarted or replaced.

- Captured live `model/list` result: `docs/reports/agent-profiles/discovery/model-list-response.json`.
- Fresh browser recording: `/private/tmp/tusker-codex-discovery.xvkked/recording/page@bbf9f1b449b601e1b3730962fa2aaa86.webm`.
- Populated-form screenshot: `/private/tmp/tusker-codex-discovery.xvkked/recording/add-profile-codex-populated-final.png`.
- Saved-profile screenshot: `/private/tmp/tusker-codex-discovery.xvkked/recording/add-profile-codex-saved-final.png`.
- Capture sidecar: `/private/tmp/tusker-codex-discovery.xvkked/recording/acceptance-flow-final.json` records default `gpt-6-astra`/`medium`, selected `gpt-5.6-luna`/`max`, and `typedModelOrEffort: false`.

The capture uses actual `<select>` controls backed by the installed response:
Add profile -> Codex -> automatically populated model -> model-specific
reasoning -> save. It is evidence of installed-Codex discovery and the
candidate UI, not a claimed provider model turn.

## 2026-09-10 Muse installed-profile discovery acceptance

The installed Codex CLI does not allow `--profile muse` for App Server, and
its App Server `model/list` therefore exposes the default Codex account rather
than the Muse profile. Muse discovery instead reads the exact installed
`muse.config.toml` that `codex --profile muse exec` uses, invokes its existing
command-backed auth helper without persisting the bearer, and validates the
configured `muse-spark-1.3` model against that profile's authenticated Meta
Responses API `/models` endpoint. The provider response does not declare a
reasoning-effort matrix, so the selector exposes only the profile's configured
and executable `high` value; no unsupported models or efforts are invented.

- Normalized live discovery response: `docs/reports/agent-profiles/discovery/muse-profile-provider-catalog.json`.
- Fresh browser recording: `/private/tmp/tusker-muse-discovery-candidate/recording/page@2e5ecaf881ff13383fcc7fb9a6a0cc50.webm`.
- Populated-form screenshot: `/private/tmp/tusker-muse-discovery-candidate/recording/add-profile-muse-acceptance-final.png`.
- Saved-profile screenshot: `/private/tmp/tusker-muse-discovery-candidate/recording/add-profile-muse-accepted-final.png`.
- Capture sidecar: `/private/tmp/tusker-muse-discovery-candidate/recording/acceptance-flow-final.json` records `muse-spark-1.3`, `high`, and `typedModelOrEffort: false`.

The flow is Add profile -> Muse -> populated model selector -> populated
reasoning selector -> save. It is a real provider-catalog observation and
candidate UI recording, not a provider model turn. Tusker's resident daemon
was not restarted or replaced.
