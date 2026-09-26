# Harness sessions, global profiles, and cleanup — review brief

Commit under review: `02fd45d1` ("Restore Claude Code harness, enforce global-only
profiles, and wire run steering"), on top of `fad658aa`. One commit, 547 files,
+7,270 / −12,913. Most of the file count is the rebuilt `internal/serve/ui/dist`.

| Area | Files | + | − |
| --- | --- | --- | --- |
| `cmd/tusker` production Go | 126 | 1,161 | 6,308 |
| `cmd/tusker` tests | 100 | 4,864 | 5,848 |
| Serve UI source and tests | 22 | 510 | 136 |

Governing decisions: `.tusker/specs/decisions/2026-09-23-harness-sessions-grill.md`
(D1–D6). Waves W-0042 (run session contract), W-0043 (agent mailbox) and W-0044
(session recovery hardening) landed before this commit; this commit fixes gaps
found when auditing them against the intended product.

## Intended product (what the reviewer should hold the code to)

- Tusker is a meta-harness over four agent CLIs: Claude Code, Codex, Muse and
  Devin (D1). Headless exec is the route for each (D2): `claude -p` stream-json,
  `codex exec --json`, `muse exec --json`, `devin acp`.
- Tusker records each run's native thread/session ID. The operator can Say
  (interject), Interrupt, or Continue a run; interrupt-plus-resume is the
  universal steering primitive, with soft Say where verified (D3).
- Workers and architects talk through one mailbox (`tusker message`), an MCP
  server injected into workers, and a hook inbox for architect sessions (D4).
- Runner profiles are defined once, in the user-global config, each tagged with
  eligible tiers (light / standard / demanding). Projects only select or map
  global profiles; they never define them.
- Tusker launches only the user's own installed binaries on the user's own login
  (D5). Live-provider qualification is a separate manual step (D6).

## 1. Claude Code missing from the harness picker

**Root cause.** Commit `8442cec5` replaced Claude Code's runner-catalog entry with
a `futureCatalogHarness` stub, so the Serve "Coding agent" picker (which lists
harnesses with `manual_entry: true`) showed only Codex, Muse and Devin.

**Fix.** `cmd/tusker/runner_catalog.go` — `discoverClaudeCatalog()`:
- Declares the aliases `fable`, `opus`, `sonnet` with efforts
  `low|medium|high|xhigh|max` (default `high`). Claude Code has no model-list
  endpoint, so the aliases are declared (`Source: declared`, `Confidence: lower`).
- Availability comes from `claude --version` (executable) and
  `claude auth status --json` (login). No credentials are read or brokered (D5).
- Tests: `runner_catalog_test.go` asserts Claude is selectable when both probes
  succeed and `unsupported` when the binary is missing; the probes are stubbed
  so the test does not depend on the host install. The "future adapters stay
  unselectable" check now uses a genuinely future adapter (`opencode`).

**Review focus.** Alias list and default effort; whether a failed
`auth status` should keep the harness listed-but-unavailable (current behavior).

## 2. Say / Interrupt / Continue for daemon-dispatched runs

**Root cause (production bug).** Say, hard Say and Continue-with-message
resolved the worker's identity through a run-history row carrying the lease
generation. The daemon never wrote that row, so every daemon-dispatched run
refused steering.

**Fix.**
- `daemon.go` — `ensureManagedRunExecution(run, note)` runs right after
  `SaveRunIdentity` at claim and creates the managed execution record. It reuses
  the task's existing root execution (query on `execution_records` by
  project/task, `node_kind = 'managed_attempt'`) before creating a new one. If
  `CreateManagedExecution` fails because another process already wrote a record
  for the attempt, it re-reads and adopts that record.
- `daemon.go` — `attachManagedRunSession(run)` runs on each tick and after
  resume. It attaches the native session ID. If the session is already attached
  (to this attempt, or to a predecessor attempt that a resumed attempt shares),
  it returns nil rather than failing every tick.
- `worker_coordination.go` — `WorkerIdentityForRun` resolves resumed attempts
  that share a predecessor's session via `attemptDescendsFrom`.
- `run_say.go`, `run_say_claude.go` — defense-in-depth fallback that reads the
  identity from the run record (`runSayWorkerIdentity`,
  `runContinuationIdentity`).
- `agent_coordination.go` — soft delivery keys off the harness `SoftSay`
  capability flag; the old `workerSoftDelivery` switch is removed.
- `runner_codex.go`, `runner_muse.go` — Resume now carries Start's
  `PrivateFolders` and `Actor` (previously dropped on resume).
- `runner_exec.go` — workers receive `TUSKER_BIN`; Muse Start/Resume prompts
  carry the CLI ask route, since Muse has no injected MCP server.

Wiring matrix after the fix:

| Harness | Native ID captured | Hard Say / Continue | Soft Say | Mailbox route |
| --- | --- | --- | --- | --- |
| Claude Code | yes | yes | yes (stdin stream-json) | injected MCP |
| Codex | yes | yes | no | injected MCP |
| Muse | yes | yes | no | CLI `tusker message ask` |
| Devin | yes | yes | no | injected MCP |

Tests: `TestDaemonClaimManagedWorkerIdentityForHarnesses`,
`TestRunSayIdentityFromRunRowForAllHarnesses`,
`TestWorkerIdentityForResumedNativeSession`,
`TestRunnerResumeCarriesStartPolicy`, `TestMuseCLIAskRoutePromptAndBinary`,
`TestEnsureManagedRunExecutionAdoptsConcurrentAttemptRecord`.

**Open, needs the live runbook.** Whether Muse's sandbox allows
`tusker message ask` to write Tusker state.

## 3. Mailbox hook for architect sessions

- `tusker message hook print --harness claude|codex [--project <id>] [--json]`
  (`agent_message_commands.go`, `agent_message_inbox.go`, `cli.go`) prints the
  hook JSON with the absolute binary path. It writes nothing; the user installs
  it. Tusker never edits `~/.claude` or `~/.codex`.
- `printMessageHelp` added; `message hook` listed in the capabilities inventory
  (`capabilities_cmd.go`).
- Tests: `TestMessageHookPrint`, `TestCapabilityInventoryCoversDispatcher`.

## 4. Global-only runner profiles

**Root cause of the "many Codex ACP profiles".** A July bootstrap wrote runner
profiles into each project's `.tusker/config.yaml`. Every repo therefore carried
its own copy of ACP-era profiles.

**Code changes.**
- Project layers may not define `automation.profiles` or `removed_profiles`
  (`runner_profiles.go`, `model_levels.go`, Serve handlers, settings UI). A
  project that sets `removed_profiles` gets its own message pointing at project
  tier mappings or `tusker models profile-remove --scope global`.
- `runner profiles --write` writes the global config only, using `codex_exec`;
  it never selects Terra or gpt-5.6 models. It prints "no new profiles for
  <path>" when nothing was added.
- Removing a global profile is refused while any registered project's config or
  unfinished task references it (`modelProfileReferencesForScope(vault,
  "global")`). References from built-in defaults do not block removal, or the
  seeded profiles could never be removed. If some project cannot be checked,
  removal is refused with a hint to disable instead.
- Global-scope saves take a lock on the global config file, acquired after the
  project lock (fixed order). `setUserGlobalConfigWithReadback` is serialized
  by `userGlobalConfigWriteMu`. New helper `acquireV7LockForIdentity` because
  the document lock requires the file to exist.
- Tier coverage ignores read-only and `review_only` profiles.
- After `profile-*` actions or any global-scope change, every project's Serve
  snapshot is invalidated. `api.ts` defaults profile actions to scope `global`.
- Completion authority (`completion_worker_safety.go`) accepts profiles from the
  global layer via `explicitProfileSource`; built-in defaults are still refused.
- **Config load no longer fails on an unknown profile reference.** A
  `default_profile`, `lane_profiles`, `model_levels` or `routing` entry naming a
  profile the global config lacks becomes a warning on
  `resolvedTuskerConfig.Warnings` (shown in the `models` report JSON). A fresh
  clone on a machine without that profile can still run `init`, `show`, `help`.
  Route selection still refuses it, and wave review marks the task
  "execute route blocked", so no directive is queued.
- Task create/update accept `--execute-profile` / `--review-profile`, and update
  accepts `--clear-*` (`direct_authoring_cmd.go`, `commands_v7.go`).
- `demo seed` no longer writes project profiles.

Tests: `TestProjectLayerCannotDefineProfilesButSelectsGlobalOnes`,
`TestModelLevelsProfileRemoveRefusesOtherProjectReference`,
`TestModelSettingsGlobalLockSpansProjects`,
`TestGlobalProfilesCoverAllTiersIgnoresReadOnlyProfiles`,
`TestRunnerProfileBootstrapFreshInitWritesNoProjectProfiles`,
`TestRunnerProfileBootstrapSkipsWhenGlobalProfilesCoverTiers`,
`TestProfileReconcileWithoutUsableHarnessWritesNothing`,
`TestTaskProfilePinsCreateAndUpdate`,
`TestDirectWaveAutonomousLateRouteRemovalDoesNotQueueNextFrontier` (assertion
moved from a config-load error to the per-member route blocker).

Test isolation: `TestMain` unsets `TUSKER_CONFIG` and sets
`XDG_CONFIG_HOME=<stateRoot>/xdg`. Subprocess helpers receive the parent's
global config through `TUSKER_PROC_CONFIG`.

**Review focus.** The warning-vs-error split at config load; the removal
reference scan across registered projects; lock ordering.

## 5. Wave start refuses profiles that cannot run unattended

Commit `c94ff806` removed `wave preflight`/`arm`, which refused an approval
policy that would stop an unattended run. The spec
(`.tusker/specs/direct-wave-authoring.md:242-244`) still requires Start to refuse
incompatible setup.

- `runner.go` — shared `approvalPolicyHumanOnlyReason`. The Codex and Claude
  live runners' `policyDenialReason` now use it.
- `runner_route_preview.go` — `routePreviewForNote` adds a blocker when the
  effective policy (computed as dispatch computes it) is `untrusted`. That covers
  wave review, wave start, `tusker run` start, `runner route` and the Serve route
  display. `on-request` is allowed: it is the workflow default and drives the
  approval-card flow.
- Test: `TestDirectWaveStartRefusesHumanApprovalProfile`.

## 6. Runtime store

- **Schema completeness.** `runtimeSchemaComplete` required
  `review_results.record_id` and `gate_ledger.record_id`, which do not exist, so
  every open ran the full migration. The expected schema is now derived once per
  process by running the real `Migrate()` against in-memory SQLite and recording
  tables, columns, indexes, triggers and ledger marker versions. A store is
  complete only if it has all of them; `executionLedgerConstraintsCurrent` still
  repairs drifted constraint SQL. If building the reference fails, the full
  migration runs.
- **Backfill.** `backfillExecutionLedger` runs once, recorded by a marker row
  (`component='legacy_backfill'`, version 1). Running it on every open raced
  with the daemon (attempt saved, then execution record created in a separate
  step) and produced `UNIQUE constraint failed: execution_records.attempt_id`,
  which made the runner wrapper exit 1. The backfill also skips attempts that
  already have a non-legacy record.
- **Work sessions.** `claimRunLeaseWithWorkSessionAttempt` now writes the
  execution record in the claim transaction (`insertWorkSessionExecutionTx`,
  `source='work_session'`, `lease_generation=0`, child of the implementation
  attempt's record when one exists). Generation 0 keeps work sessions out of
  daemon Say/Continue targeting.
- **Project-scoped run lookups.** Bare `FindRun(taskID)` matched any project with
  the same task ID. Scoped now: `findRunForVault` (an unregistered vault returns
  no run), `ReclaimExpiredRunLease`, integrator dependency reports, event-log
  sinks and supervisor-decision replay (failure records now carry `ProjectID`;
  a legacy record without one still gets the typed `RUN_IDENTITY_AMBIGUOUS`
  refusal).
- `DefaultStateRoot` accepts only an absolute `TUSKER_STATE_ROOT` (a relative
  value such as the string `undefined` had scattered `daemon.db` files).

Tests: `TestRuntimeSchemaCompleteTakesFastPathAndDetectsMissingLateSchema`,
`TestWorkSessionClaimRecordsExecutionInClaimTransaction`,
`TestEventLogPersistenceFailureSinkResolvesSharedTaskIDByProject`,
`TestExecutionRegisterResolvesRuntimeProject`.

**Review focus.** The derived-schema comparison (correctness for old DBs); the
one-time backfill marker; attempts saved outside the three claim paths.

## 7. Other CLI gaps

- `execution register` resolves the runtime ULID project via
  `digestRuntimeProjectID` and accepts `--project`; `agent_contacts.go` accepts
  either the config `project_id` or the ULID.
- `global uninstall --json` no longer prints a second JSON error document after
  a failed apply (`afterResultEmitted`).

## 8. Dead-code removal

About 240 unreachable functions were removed (found with
`golang.org/x/tools/cmd/deadcode`), including the retired Codex ACP setup and
adapter installer (`acp_setup.go`, `acp_adapter_native_darwin.go`,
`acp_adapter_publish_darwin.go`, most of `acp_adapter_install.go` and
`acp_adapter_npm.go`) and `demo_session_permission.go`.

A first pass removed 99 tests along with the functions. An independent pass
reviewed each one: 72 exercised live code through thin wrappers and were
restored, rewired to the live function or given a test-only helper. The
remaining removals test code that no longer exists (ACP installer and setup,
scratch reaping, demo session permissions, readiness contract, close-policy and
legacy-finding migrations, file/markdown V7 stores). The full list of removed
test names is in the diff (`git diff fad658aa 02fd45d1 -- 'cmd/tusker/*_test.go'`).

**Review focus.** That each removed test covered only deleted code.

## 9. Serve UI

- `src/components/MobileNav.tsx` — bottom bar below 1024px; `__root.tsx` adds
  the project drawer and backdrop; `ProjectStrip.tsx` exports its section list.
- `Sidebar.tsx` renamed to `AddProjectForm.tsx` (it only held that form).
- Profile actions default to global scope in `api.ts`.
- Screenshots: `docs/reports/mobile-nav/`. `dist` rebuilt with `bun run build`.

## 10. Specs, docs, config

- `agent-access.md`, `runner-execution-boundary.md` describe direct `muse exec`
  (D2). `docs/system/orchestration.md` points to `message hook print`.
- `docs/qualification/live-harness/README.md` + `seed.sh` — the D6 manual
  runbook over a `demo seed --scenario parallel-waves` fixture.
- `skills/tusker/references/MIGRATION.md` and small skill reference edits were
  in the working tree and are included. The same applies to a few smaller
  working-tree changes this brief does not describe (for example the tests
  `TestWaveCreateHelpShowsAuthoringSchema`,
  `TestNewEpicRejectsUnresolvableSpecRefWithReason`,
  `TestBareStatusPrintsVaultAndEmptyEpicSummary`,
  `TestDirectWaveReviewCheckJSONEmitsSingleDocument` and their code); review
  them from the diff.
- `.gitignore`: `say-*.wav`, stray `daemon.db`, `.human-kinds-*`, the
  `cmd/tusker/tusker` binary, `*.bak-*`.

Outside the repo (not in the commit): the user-global
`~/.config/tusker/config.yaml` now holds the profiles
`codex_exec-gpt-6-luna` (light), `codex_exec-gpt-6-sol-low` and
`devin-swe-2-high` (standard), `codex_exec-gpt-6-sol-medium` and
`claude-opus-high` (demanding), and `muse-spark-1.3-high`. Ten other repos had
their project profile blocks removed and now select these. Every edited file
has a `*.bak-2026-09-25` backup. Astra is deliberately not configured.

## Verification

- `go build ./...`, `go vet ./cmd/... ./internal/...`, `gofmt -l` clean.
- `rtk proxy go test -count=1 ./cmd/... ./internal/...`: all packages pass
  except one flake in the final run, `TestSearchUsesAllPositionalQueryTerms`.
  Cause: parallel tests overlapping in the process-wide `captureStdout`. Fixed
  with a mutex; the test passes on rerun.
- `waitForWrapperDone` timeout raised from 3s to 15s; it only detects hangs.
- UI: `bun run typecheck` clean, `bun test` 385/385.
- Not done: live-provider qualification (D6 runbook, manual).

## Known limitations

- If `ensureManagedRunExecution` fails for any reason other than the adopted
  race, the lease stays claimed and the attempt has no execution record until
  the lease expires; nothing repairs it now that the per-open backfill is gone.
- Unknown-profile warnings show in the `models` report only; `doctor` does not
  read the resolved config yet.
- The 12 terminal `ORC-T-0077..0088` tasks still name `implementation-terra`;
  left unchanged on purpose.

## Process note

The work was done by the interactive session plus parallel subagents (Opus,
Devin SWE-2, gpt-6-sol low/medium) with disjoint file ownership and a central
test gate. Sol's dead-code pass over-deleted tests; that was caught and
repaired as described in section 8. Reviewers should weigh the dead-code and
test-removal portion accordingly.
