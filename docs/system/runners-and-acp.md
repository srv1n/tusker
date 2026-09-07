---
title: "Runners and ACP"
subject: runners-and-acp
part_of: overview
status: canonical
---

# Runners and ACP

Tusker executes coding agents installed and authenticated by the operator. It does
not bundle, install, update, or silently substitute an agent or ACP adapter. The
canonical contract is [[runner-execution-boundary]].

## Supported installed routes

| Harness | Structured launch | Permission support |
| --- | --- | --- |
| `codex_exec` | `codex exec --json` | read-only, workspace write with explicit network off/on, and externally contained full access |
| `muse` | `codex --profile muse exec --json` | the same compiler as Codex; the operator owns the Muse profile and authentication |
| `claude-code` | `claude -p --output-format stream-json` | read-only and externally contained full access; workspace-write presets are unavailable because Claude does not expose a verified workspace boundary |
| `acp_v1` | exact installed executable and argument array | bounded presets only when the endpoint is wrapped by separately verified native containment and passes a live ACP handshake; full access is unavailable |

Transport is selected before claim and never falls back silently. Preparation
resolves every executable candidate, probes version and authentication, compiles the
requested preset into provider-native arguments, and pins the physical executable
identity. Admission failure leaves work unclaimed and records an actionable
infrastructure block.

## Test a harness

Run local admission checks without starting a model:

```sh
tusker runner test codex_exec --json
```

Run one disposable model turn only when that spend is intended:

```sh
tusker runner test codex_exec --live --json
tusker runner test codex_exec --live --exercise print --json
tusker runner test codex_exec --live --exercise timer --json
tusker runner test codex_exec --preset workspace-write-offline --live --script ./scripts/my-canary --json
```

The Settings > Runner profiles screen calls the same service through **Run local
checks** and **Run live canary**. Reports use `tusker.runner-conformance/v1` and keep
`ready: false` until a live canary succeeds. A live pass expires after 24 hours.

The catalog is a machine-local observation:

```sh
tusker runner catalog --json
```

It reports Codex, Muse, or Claude only after their installed commands respond to the
required probes. ACP is profile-specific and becomes eligible through conformance,
not by the existence of a bundled adapter.

## Configuration-only onboarding

A compatible CLI uses an existing dialect:

```yaml
automation:
  profiles:
    review-local:
      harness: codex_exec
      model: gpt-6-astra
      effort: high
      permission_preset: read-only
      command: codex exec --json -
      sandbox: {mode: read-only, network: false}
      subagents: {allowed: false, max_concurrent: 0}
```

A compatible installed ACP endpoint uses the same profile store. The executable
must already be installed. `native_containment: true` is reserved for an endpoint
whose wrapper has separately passed the containment suite; it is not inferred from
ACP itself.

```yaml
automation:
  profiles:
    review-acp:
      harness: acp_v1
      model: provider-model
      effort: medium
      permission_preset: read-only
      command: /opt/local/bin/contained-agent-acp --stdio
      native_containment: true
      sandbox: {mode: read-only, network: false}
      subagents: {allowed: false, max_concurrent: 0}
```

Unknown CLI event or permission semantics require a small adapter in
`internal/runner`; arbitrary shell commands are not a harness API.

## Work levels

`automation.model_levels` maps Light, Standard and Demanding to ordered execute
and review profile lists. The first entry is primary and later entries are the
only permitted fallbacks. Explicit task `runner_profile`, routing rules and lane
profiles keep their existing precedence. An authored `work_level` or
`review_level` selects the new mapping; existing routine/standard/complex/frontier
complexity routes remain compatible, including frontier-specific profiles.

Tusker records profile, harness, model and effort on the run before claim. A retry
in the same lane preserves that recorded cycle even if configuration changes.
Unstarted work observes the newest valid configuration. Missing mappings and
unknown profiles block before claim.

`automation.profiles` admits only operator-installed harnesses, so a
simulated executor cannot be registered there: the repeatable demo declares
its `demo-timer` implement/review/plan profiles in demo-scoped config
(`.tusker/demo/profiles.yaml`, also mapped in the demo manifest) and executes
through the `demo-timer` driver, which performs fixed delays, exact fixture
writes, and bounded progress instead of launching a model. A demo run proves
orchestration, never provider conformance. `demo run --require-harness`
refuses when the named harness is unavailable rather than substituting
another one silently.

## Lifecycle ownership

`internal/runner` owns discovery, probes, exact policy projection, structured process
launch, six normalized lifecycle events, output bounds, cancellation, process-tree
supervision, and execution receipts. `cmd/tusker` owns claims, workspaces, task
verification, review, retries, recovery, and canonical closure. Workers never update
Tusker task state.

The public `acp install` and `acp setup` routes are retired. Existing saved
`codex_acp` configuration is never selected as a fallback and must be migrated to an
operator-installed `acp_v1` endpoint or to an explicit CLI profile.

## Code sources

- `internal/runner/`
- `internal/acp/`
- `cmd/tusker/runner_conformance.go`
- `cmd/tusker/runner_claude_live.go`
- `cmd/tusker/daemon.go`
- `internal/serve/ui/src/features/settings/app/ProfilesSection.tsx`
