---
title: "Agent access acceptance"
subject: agent-access-acceptance
part_of: system
status: canonical
---

# Agent access acceptance

This report records the backend acceptance evidence for the versioned agent-access
contract. The local checks below use disposable workspaces, a provider-free direct
Muse JSONL fixture, and the existing in-process approval store. They prove route,
policy, preparation, execution-envelope, and recovery behavior; they do not prove
provider enforcement or a paid model turn.

## Acceptance matrix

| Concern | Evidence | Result |
| --- | --- | --- |
| New profile defaults and shared resolution | `TestAgentAccessIntegrated`, `TestAgentAccessContract` | PASS: `work_in_projects`, workspace write, network on, destructive actions ask, and empty private-folder list resolve before launch. |
| Legacy profile preservation | `TestAgentAccessIntegrated`, `TestAgentAccessMigration` | PASS: an absent `access` object continues to use the authored legacy permission preset; no automatic migration is applied. |
| Explicit legacy upgrade | `TestAgentAccessProviderFreeJourney`, `agent-access-journey.test.ts` | PASS: Full access stays unchanged until `Use project access` is saved; Cancel is inert and profile identity, model, reasoning and tier references are preserved. |
| Shared command behavior | `TestCommandPolicyPrecedence`, `TestAccessProfilePublishesFixedCommandPolicy`, callback fixtures | PASS: resolver, preview and runtime share fixed Automatic, Ask each time and Block behavior; Review only blocks writes and mandatory blocks outrank approval. Historical `automation.denylist` remains compatibility-only and is explicitly non-authoritative. |
| Direct Muse execution | `TestAgentAccessIntegrated`, `TestMuseNativeRoute` | PASS: `muse_cli` prepares `muse exec --json`, records the resolved access fingerprint, and consumes the structured terminal/session fixture. It is distinct from the legacy `muse` Codex-profile route. |
| Required native gaps | `TestAgentAccessIntegrated`, native-control fixtures | PASS: unsupported private-folder exclusion is surfaced as `access_control_unsupported`; the run does not widen scope or mutate the private sentinel. |
| Policy revision binding | `TestAgentAccessIntegrated` | PASS: changing network policy changes the resolved fingerprint and effective policy. |
| No-callback denial | `TestAgentAccessIntegrated` | PASS: destructive deny compiles Muse's native `--approval-mode never`; no synthetic approval response is created. |
| Approval duplicate, exact binding, and stale response | `TestAgentAccessApproval`, `TestAgentAccessApprovalNativeOptionAndCancellation` | PASS: duplicate delivery is idempotent, changed identity/arguments/policy is rejected, and stale/conflicting decisions fail closed. |
| Restart recovery and fresh attempt | `TestDaemonStartupReconcilesAgentAccessApprovals`, `TestAgentAccessApprovalRecovery` | PASS: secondary store opens preserve pending callbacks; the authoritative `daemonRunCmd` startup expires an unverifiable callback, after which the old revision cannot settle it and a new attempt receives a fresh request. |
| Profile and approval UI | `agent-access-profiles.test.ts`, `agent-access-approval.test.ts`, browser fixtures | PASS: 5 source tests/49 assertions and 2 browser scenarios/38 assertions cover defaults, explicit setup/test/save, styled desktop and 390px layouts, 200% zoom overflow, 44px narrow controls, visible keyboard focus, immutable request states, keyboard-safe allow-once/block, and deduplication. |
| Private-folder enforcement | `TestAgentAccess*`, `TestClaudePrivateFolderToolBoundary` | PASS: required controls need affirmative native support, private descendants remain valid exclusions, and Claude rejects supported tool and shell paths inside configured private folders. |
| Policy override resistance | `TestMusePolicyArgsCannotOverrideRequestedPolicy`, `TestMuseOfflinePolicyCannotEnableNetwork`, `TestMuseCLIRejectsConfiguredPolicyOverrides` | PASS: configured `--yolo`, `--workspace`, `--approval-mode`, and `--sandbox-network` forms cannot override resolved Muse policy. |
| Mandatory denial before approval | `TestAgentAccessApprovalPreflightDeniesBeforePersistence`, `TestDestructiveGitCallbackClassification`, `TestCodexLiveRunnerReviewerLaneForcesReadOnlyAndRejectsMutatingApprovals` | PASS: complete Codex, Claude, and ACP callback paths deny root, home, project-root, private, outside-workspace, raw-device, `diskutil eraseDisk`, `mkfs`, ambiguous, chained, and alternate-repository (`-C`, `--git-dir`, `--work-tree`) requests before persistence. Bounded destructive Git operations can reach allow-once; ordinary `git add` and `git commit` run automatically in project-write mode but remain denied in Review only. |
| Approval store liveness | `TestAgentAccessApprovalStoreOpenPreservesLivePendingRequest`, `TestDaemonStartupReconcilesAgentAccessApprovals`, `TestDaemonStartupReconciliationPreservesRegisteredLiveAttempt` | PASS: a second store open preserves pending callbacks; authoritative daemon startup reconciles them against the in-process live-handle registry and expires only unverifiable requests. |
| Draft validation fidelity | `TestServeRunnerConformanceSetupResolvesDraftAccessWithoutModel`, browser profile fixture | PASS: setup resolves the full unsaved access object without a model call; Run test carries the same private folders, references, network, and destructive policy. |

## Local route evidence

Installed executable metadata observed on this host:

| Route | Executable | Version |
| --- | --- | --- |
| Codex CLI | `/Users/sarav/.bun/bin/codex` | `codex-cli 0.153.4` |
| Claude CLI | `/Users/sarav/.local/bin/claude` | `2.1.261 (Claude Code)` |
| Direct Muse CLI | `/Users/sarav/.local/bin/muse` | `Muse Code 1.1.1 (1.1.1-R2514.1)` |

Native mappings are documented in `native-controls.md` and `muse-native.md`.
Installed executable discovery and provider-free echo checks are local setup
evidence only. Credentials, provider authorization, and live model identity remain
separate facts.

## Reproducible checks

| Check | Result |
| --- | --- |
| `go test ./internal/runner ./cmd/tusker -run 'TestAgentAccess' -count=1 -timeout=120s` | PASS (`internal/runner` 0.611s; `cmd/tusker` 5.089s) |
| `go test -race ./cmd/tusker ./internal/runner -run 'TestAgentAccessApproval' -count=1 -timeout=120s` | PASS (`cmd/tusker` 4.029s; `internal/runner` no tests to run) |
| `go build -o /tmp/tusker-agent-access-check ./cmd/tusker` | PASS |
| `bun test --cwd internal/serve/ui test/agent-access-profiles.test.ts test/agent-access-approval.test.ts` | PASS (5 tests, 49 assertions) |
| `bun test --cwd internal/serve/ui tests/profiles-interaction.test.ts tests/agent-access-approval.test.ts` | PASS (2 browser scenarios, 38 assertions) |
| `bun run --cwd internal/serve/ui typecheck && bun run --cwd internal/serve/ui build` | PASS |
| `tusker docs check --json` | BLOCKED by pre-existing `.tusker/specs/decisions/2026-09-11-project-registration-and-visibility-grill.md` missing required `decides_for`; 44 documents scanned |

## Onboarding recipe

1. Confirm the operator-installed executable and version for the selected route.
   Use `muse_cli` for direct `muse exec --json`; use `muse` only for the legacy
   `codex --profile muse exec --json` route.
2. Author the profile's `tusker.agent-access/v1` object. The minimal bounded
   profile is `mode: work_in_projects`, `network: true`,
   `destructive_actions: ask`, `folders: []`, and `private_folders: []`.
3. Run `tusker runner test <profile> --json` for setup evidence. Review the
   resolved report, native controls, policy fingerprint, and any unsupported
   required control before dispatch.
4. A required control that the selected route cannot express blocks execution;
   do not widen the profile or rely on an implicit fallback. Approval decisions
   are human, request-bound, revision-checked, and allow-once only.
5. Run `tusker runner test <profile> --live --json` only as a separately authorized
   disposable model turn. Record its provider/model/authentication result apart
   from this provider-free acceptance evidence.

## Explicit gaps

Paid or live model qualification, authenticated provider turns, installed native
callback liveness, and hostile-client containment were not run. UI acceptance is
fixture-browser evidence with captured desktop, 390px, and 200% zoom states, not an
installed-app or live provider claim. Muse `serve`/MSP discovery is not execution
evidence. No fallback route is claimed when a required native control is
unsupported.
