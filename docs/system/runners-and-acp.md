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
| `muse` | `muse exec --json` | native Muse workspace, approval, network, write and shell flags; private-folder exclusions and read-only external roots remain unavailable |
| `claude-code` | `claude -p --output-format stream-json` | read-only and externally contained full access; workspace-write presets are unavailable because Claude does not expose a verified workspace boundary |
| `devin` | installed `devin acp` with Tusker-compiled `--sandbox`, exact ACP model, and `smart` mode | workspace write with internet on and destructive actions blocked; offline, review-only, private-folder, external-root, operator-approval, and full-access profiles fail closed |
| `acp_v1` | exact installed executable and argument array | bounded presets only when the endpoint is wrapped by separately verified native containment and passes a live ACP handshake; full access is unavailable |

Transport is selected before claim and never falls back silently. Preparation
resolves every executable candidate, probes version and authentication, compiles the
requested preset into provider-native arguments, and pins the physical executable
identity. Admission failure leaves work unclaimed and records an actionable
infrastructure block.

For profiles with the `tusker.agent-access/v1` contract, preparation also pins the
resolved access report and fingerprint. Required controls that are only advisory
or unsupported make that route unavailable. Muse `serve`/MSP is used for catalog
discovery, never as an execution transport or mislabeled as ACP.

## Test a harness

**Check setup** runs local admission checks without starting a model:

```sh
tusker runner test codex_exec --json
```

**Run test** starts one disposable model turn only when that spend is intended:

```sh
tusker runner test codex_exec --live --json
tusker runner test codex_exec --live --exercise print --json
tusker runner test codex_exec --live --exercise timer --json
tusker runner test codex_exec --preset workspace-write-offline --live --script ./scripts/my-canary --json
```

The Settings service calls the same resident conformance path through **Check setup**
and **Run test**. Reports use `tusker.runner-conformance/v1`, identify the selected
profile/model/effort when one was supplied, and keep `ready: false` until a live
canary succeeds. A live pass expires after 24 hours. The disposable turn is bounded
to two minutes and its temporary workspace is removed afterwards.

The catalog is a machine-local observation:

```sh
tusker runner catalog --json
tusker runner catalog --refresh --json
```

It reports executable detection, authentication state, discovery source/freshness and
conformance as separate facts. Codex can return its installed model inventory. Muse
CLI discovers its installed account catalog through MSP `model/list`; the selected
profile still executes through `muse exec --json`, and authentication remains unknown
until an explicit profile test passes. Claude Code is supported; OpenCode and Cursor remain non-selectable future entries. Devin's
catalog intersects `devin models list --format json` metadata with the exact model
choices advertised by ACP `session/new`; unsupported account models are not offered.
ACP is profile-specific and becomes eligible through conformance, not merely by the
existence of an installed executable.

For Settings integration, the stable read/test examples are:

```sh
tusker models catalog --json
tusker models profile-set --scope project --name muse-review --harness muse --model <exact-id> --effort high --preset read-only
tusker runner test muse-review --json       # Check setup: no model turn
tusker runner test muse-review --live --json # Run test: one explicit turn
```

`models catalog --json` includes `schema`, `version`, each preset's CLI transport,
setup text, executable/authentication/discovery/conformance states, and its
`last_checked` timestamp. The profile test report includes `profile_id`, `model`,
`effort`, `transport`, case-level outcome, timestamps, and a safe `next_step` on
admission failure. Neither output includes credential values.

Successful model discovery is cached for 24 hours by adapter version, transport,
and non-secret configuration context. `--refresh` bypasses a fresh entry; if it
fails, the last successful models remain with `discovery_state: stale`. Bundled
discovery failure returns no invented fallback IDs.

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

The direct Muse route can be authored with the shared access contract:

```yaml
automation:
  profiles:
    muse-project:
      harness: muse
      model: <installed-model-id>
      effort: medium
      access:
        schema: tusker.agent-access/v1
        mode: work_in_projects
        network: true
        destructive_actions: ask
        folders: []
        private_folders: []
```

Installed authentication and paid model turns remain separate setup/live
evidence; missing credentials do not create a fallback route.

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

Devin uses its installed ACP endpoint directly:

```yaml
automation:
  profiles:
    devin-project:
      harness: devin
      model: <exact-discovered-acp-model>
      effort: medium
      command: devin acp
      access:
        schema: tusker.agent-access/v1
        mode: work_in_projects
        network: true
        destructive_actions: deny
        folders: []
        private_folders: []
      subagents: {allowed: false, max_concurrent: 0}
```

Tusker owns the Devin launch controls: the authored command stays exactly `devin acp`,
then preparation adds `--sandbox` and the selected model, while session setup applies
ACP `mode=smart`. A profile asking for an unsupported access shape is rejected before
claim instead of weakening to a broader mode.

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

Profiles can be disabled without erasing their definition or recorded run identity.
A disabled profile is unavailable for new resolution; only an already-authored
ordered fallback may replace it. Removal is revision-guarded and refuses references
from level/lane/default/routing configuration or non-terminal task overrides.

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
