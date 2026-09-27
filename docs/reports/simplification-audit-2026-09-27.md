---
title: "Simplification audit of cmd/tusker, judged by decision D2"
subject: simplification-audit-2026-09-27
status: canonical
read_when: "Planning which Tusker guards, adapters and commands to cut, or dispatching a simplification slice."
skip_when: "Looking up how a kept subsystem works today; read the system chapters instead."
---

# Simplification audit, 2026-09-27

Scope: `cmd/tusker` at `d3f0012b`. That is 156,060 lines of non-test Go in 271
files, plus 110,460 lines of tests in 356 files. The rule is the owner's
decision D2 in `docs/system/proposals/real-testing-campaign.md:248`: trust
honest agents, catch the rest with review, proof checks and the UI, and keep
only the cheap guards that real incidents earned.

Evidence comes from reading the code, from `tusker capabilities --json`, and
from read-only queries of the live runtime database
(`~/Library/Application Support/tusker/daemon.db`). Line counts are `wc -l`.

## 1. Verdict

Tusker is over-built against the wrong threat. Every blocker in today's live
test came from a guard that assumes a lying worker. The biggest offenders are
the completion-authority chain (4,286 lines), the global invariant circuit
latch, the wakeup queue, and the wave "drift" refusals. None of these has ever
caught a dishonest agent. They have repeatedly stopped honest work. Next to
them sit about 16,000 lines of adapters and pipelines that no configuration
selects and the database shows were never, or no longer, used: `codex_cloud`,
`codex_app_server`, the Codex ACP layer, the ChatGPT external loop, scheduled
promotion and departures, and the feedback-signal pipeline. Cut the adversarial
guards and the dead paths. Keep the guards that stopped the July 2026 runaway
loop: retry and continuation caps, token budgets, one lease per task, the
crash-loop circuit, worktrees and wrapper supervision. That removes roughly
40,000 to 50,000 lines (source plus tests) without losing a single protection
an incident earned. It also fixes F7, F14, F15, F16 and F17 on the way.

## 2. Guard and protocol subsystems

"Blocked?" cites a campaign finding (F-number), a commit, or the database. "DB"
means a row count in the live `daemon.db`.

| # | Subsystem | Main files | Src lines (tests) | Defends against | Blocked legit work? | Verdict | Replacement |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | Completion authority chain | `completion_reactor.go`, `completion_authority.go`, `completion_worker_safety.go`, `completion_reactor_mode.go`, `v7_completion_receipt.go`, `v7_close_authority.go`, `landing_authority.go` | 4,286 (~4,485) | Forged or replayed review verdicts, post-review edits, swapped runner binaries | Yes. F14: refuses every non-Codex harness and every full-access profile (`completion_worker_safety.go:24-37`). F15: `automation explain` cannot predict it. About 25 "fix/harden completion authority" commits (e.g. `cbd51d95`, `85e8ac31`, `95f53c38`). DB: 80 transactions, all in temp demo or walkthrough projects, none in a real repo | CUT | ~150-line pass handler (section 3, cut 1) |
| 2 | Global invariant circuit | `sentinel.go`, `daemon_event_log_failure.go` | 988 (851) | Held leases on ineligible tasks, attempt caps exceeded, dead PIDs, duplicate leases, stalled poll | Yes. F7: froze all dispatch for 5 days. The latch stays open with zero violations (`sentinel.go:156-163`). Earlier freezes: RUN-T-0033 (review flip opened the circuit), RUN-T-0039 (stale rows after close) | SIMPLIFY | Same checks, but park the offending run, scope any block to its project, and auto-close when clean |
| 3 | Crash-loop circuit | `daemon_launchd.go` | 346 (594) | Daemon restart loops | No | KEEP | — |
| 4 | Single-daemon pidfile | `daemon_guard.go` | 204 (178) | Two daemons | No | KEEP | — |
| 5 | One lease per task (CAS claim) | `run_ownership.go`, `runtime_store.go:3534-3778`, `:4189` | ~850 core | Double dispatch after a crash or reclaim | No. Earned by RUN-T-0043 (lease race) | KEEP | — |
| 6 | Per-generation run authorization lineage | `runtime_store.go:1043-1053`, `:2810`, `:2851`, `execution_ledger.go:142` | ~300 | Stale authority reused by a newer attempt | Indirectly, through 7 | CUT | Directive plus armed wave |
| 7 | Wave contract, authorization and material drift | `direct_wave_authority.go:158,389,765,1089`; `direct_run_authority.go:79-190`; `v7_wave_authorization.go:36-243,272` | ~1,300 of 3,377 (~1,500 of 3,217) | A worker or author editing a task or spec after arming | Yes, in history: `60faa7f2` "Repair interactive-claim and armed-wave authority regressions", `cf3f96c0` "Fix tracked delivery plan context drift", `519e14ac`, `89a069af` | CUT | Directive active, task in wave, wave armed |
| 8 | Wave arming and dispatch scope | `dispatch_scope.go`, `directWaveStart`/`Pause`/`Resume` in `direct_wave_authority.go:1323,1781,1847`, `armed_wave.go` | ~1,000 | Drafts starting by accident | No. It is the owner's vision ("Arming") | KEEP | — |
| 9 | Upstream hold | `upstream_hold.go` | 81 (164) | Dispatching on a red upstream | No | KEEP | — |
| 10 | Self-service admission | `self_service_admission.go` | 183 (310) | Stale or changed authority | Through 7 | SIMPLIFY | Drop `AuthorityStale` and `MaterialChanged` (`:113-115`) |
| 11 | Retry, continuation and no-progress caps | `workflow.go` retry, `retry_failed_task.go`, sentinel attempt check | ~400 | Runaway loops. July 2026: 1.63B tokens, 88 attempts on FBK-T-0002 | No | KEEP | — |
| 12 | Token budget governor | `budget.go` | 152 | Unknown failures burning tokens | No. Proven live on 2026-07-07 (killed at 14,361 > 5,000) | KEEP | — |
| 13 | Wrapper supervision and adoption | `runner_wrapper*.go`, `runner_preclaim_health.go` | 1,018 (796) | Orphans, ghosts, daemon death | No | KEEP | — |
| 14 | Per-task worktrees | `workspace_manager.go`, `worktree_*` | ~930 | Agents trampling each other | No | KEEP | — |
| 15 | Agent wakeup queue | `agent_coordination.go:14-308,565-595`, `agent_messages.go:219` | ~400 (~300) | Double delivery, stale routes, impersonated replies | Yes. F16: the answer sat queued until the 30-minute cold poll. Serve (`serve_agent_messages.go:55-66`) and CLI (`cli.go:170-176` lacks `message reply`) never nudge the daemon. About 20 park branches (`agent_coordination.go:67-252`) | CUT | Answer handler resumes the run directly (cut 3) |
| 16 | Yield marks session not resumable | `daemon.go:2903`, `:2959` | 2 tokens | Nothing | Yes. F17 | CUT (fix) | Pass `sessionResumable` (`daemon.go:2781`) |
| 17 | Access resolution and destructive-command guard | `agent_access.go:242`, `agent_access_approval.go:282-476`, `agent_access_policy.go` | ~750 | Honest mistakes (`rm -rf`, force-push) | No | KEEP | — |
| 18 | Access, approval and ACP permission fingerprints | `agent_access.go:506`, `agent_access_approval.go:176,212,221`, `acp_permission.go:176,257` | ~150 | Approval replay by a worker | No | CUT | Nothing |
| 19 | Symlink-safe private paths | `private_path.go` | 225 | A hostile worker planting symlinks under log paths | No | SIMPLIFY | Temp file plus `os.Rename`, mode 0600 |
| 20 | Landing receipts and authority | `v7_land_cmd.go:1993,2097`, `scheduled_promotion.go:656`, `landing_authority.go` | ~500 | Forged landing records | DB: `landing_authority_issuances` = 0 | CUT the fingerprint chain | Plain "landed" record in git |
| 21 | Full-gate isolation provider | `v7_full_gate_provider.go`, `v7_full_gate_state_root.go` | 2,858 (1,710) | A gate daemonizing out of its container | Never ran. `isolation_provider` is set in no config. `v7_land_cmd.go:3111` checks 20 digests in one boolean | CUT | Run the gate command in the worktree |
| 22 | Verification receipts | `v7_verification_execution.go:823` | ~200 of 1,097 | Stale proof after code changes | No | SIMPLIFY | One material hash |
| 23 | Hidden demo projects | `serve_command.go:1367-1444`, `ui/src/types/domain.ts:515` | ~40 | Demo clutter in the sidebar | Yes. F13 | SIMPLIFY | Show every project, with a "demo" badge |
| 24 | ACP adapter bundle receipts | `acp_adapter_bundle.go`, `acp_adapter_install.go`, `acp_adapter_npm.go` | 1,229 | Supply-chain tamper of `codex-acp` | No, but unused (see 4) | CUT with codex_acp | — |

Across non-test files, `Fingerprint` appears 905 times in 56 files, and 157
lines mention "drift". Most of them belong to rows 1, 7, 18, 20 and 21.

## 3. Ranked cut list

Order: (blocks legitimate work) × (size). Line estimates include tests.

### Cut 1. Completion authority chain → a small pass handler (~8,300 lines out, ~150 in)

- **Files and functions:** all of `completion_authority.go`, `completion_worker_safety.go`,
  `v7_completion_receipt.go`, `v7_close_authority.go` (except the
  `v7CloseAuthorityDigest` format helper, which moves) and `landing_authority.go`. In
  `completion_reactor.go`, keep only `reconcileReviewCompletion` (:277) and
  `returnCompletionFindingToImplementer` (:1671). Delete the phase machine
  `completePassingReview` (:1011-1280), frozen authority (:659-1000),
  staged-object and ref authentication (:1324-1428), and exact staging
  (:1878-2450). Replace `completion_reactor_mode.go` with an on/off bool.
- **New pass handler:** `v7ClosePreflight` (`v7_close_ceremony.go:103`), then land
  through the existing path (`daemon.go:3993`), then `applyV7TaskCloseProjection`
  (`v7_close_ceremony.go:519`), then commit. Make it crash-safe with "task done, or
  branch already an ancestor of integration", as `daemon.go:3990` does.
- **Callers to edit:**
  - `daemon.go`: 1691, 3146, 3886, 3965, 4477-4589
  - `review_result.go:71`, `:430-459`, `:574` (drop the v3 policy-fingerprint requirement)
  - `v7_validation.go:207`, `:1739-1749` (stop requiring `close_authority` on done tasks and closed events)
  - `factory_operations.go:304,323,622,687`
  - `v7_close_ceremony.go:270-398`
  - `demo_check.go:405`, `v7_control_cmd.go:486`, `work_recovery.go:784`
  - `departure_execution.go:403`, `v7_land_cmd.go:2110`
  - `runner_claude_live.go:172` and `runner_exec.go:129` (binary identity)
  - Mode plumbing: `workflow.go:316,635-656`, `automation_commands.go:1105`,
    `runner_profiles.go:564`, `setup_doctor.go:186`, `demo_cmd.go:140`
- **Tests that go:** `completion_reactor_test.go` (2,845) and siblings (~150),
  `worker_authority_boundary_test.go` (671), `landing_authority_test.go` (494),
  `completion_authority_test.go` (179), `completion_reactor_mode_test.go` (148).
  About 4 other test files set authoritative mode and need that line dropped.
- **Risk:** medium. Existing done tasks carry `close_authority` facts, so the
  validator must accept them and stop requiring them. A worker could in theory
  write its own "pass" verdict; D2 accepts that, and review plus the owner's
  UI look catches it. D1's `sandbox-exec` proposal is the cheap follow-up if
  that ever happens. This cut also fixes F14 and F15.

### Cut 2. Global circuit latch → per-run park, self-closing (~600 lines out)

- **Files:**
  - `sentinel.go:131-177`: delete the latch branch at :156-163. When there are no
    violations, close the circuit.
  - Scope `invariantDispatchBlocker` to the project that owns the violation.
    Callers: `daemon.go:1447,1654`, `automation_commands.go:661`.
  - For `held_lease_dispatch_eligible` violations (the F7 class), retire or park
    the run instead of opening any circuit.
  - `daemon_event_log_failure.go`: drop the failure registry, probe and CAS
    (:168, :257-350). Log and retry instead.
  - `ResumeInvariantCircuit` (`sentinel.go:179-284`) shrinks to about 10 lines.
- **Tests:** trim `daemon_sentinel_test.go` (422) and `daemon_event_log_failure_test.go` (429).
- **Risk:** low. The checks stay. Only the blast radius shrinks. A note on F7:
  the sentinel already skips disabled projects (`daemon.go:987,1069`,
  `project_loader.go:53`). The project was most likely enabled when the circuit
  tripped on 2026-09-22. The latch kept the stale violation after that, and
  nothing ever told the owner.

### Cut 3. Wakeup queue → direct resume on answer (~700 lines out, ~20 in)

- **Fix F17 first:** `daemon.go:2903` and `:2959` pass `false`. Pass
  `sessionResumable` instead.
- **Fix F16:** in `putAgentAnswer` (`agent_messages.go:194`), after storing the answer:
  - If the run is live, call `runSayHard` (`run_say.go:207`).
  - Otherwise call `QueueAgentContinuation` (`agent_coordination.go:548`) and send
    `reconcile_project` the way `serve_command.go:1712` does.
  - Add `message reply` to `cliCommandMutatesVault` (`cli.go:170-176`).
- **Then delete:**
  - the `agent_wakeups` table and `ListQueuedAgentWakeups`,
    `ClaimAgentWakeup`, `SetAgentWakeup*` and `processAgentWakeups`
    (`agent_coordination.go:14-308,565-595`)
  - `queueAgentMessageWakeup` (`agent_messages.go:219`)
  - the store migrations (`runtime_store.go:1239,1640-1643`) and the call sites
    `daemon.go:782,797`
  - the recipient-generation stale branches (:94, :111, :184) and the redundant
    sender recheck (:160)
- **Keep:** the stop-intent check (:188), "don't interrupt a non-yield turn"
  (:205), and the CAS inside `QueueAgentContinuation`.
- **Tests:** `agent_coordination_e2e_test.go` (193) is rewritten. Trim `agent_message_inbox_test.go`.
- **Risk:** low to medium. The `return err` exits at `agent_coordination.go:170-250`
  can currently abort a whole poll. Deleting them removes that hazard as well.

### Cut 4. Wave and run drift refusals (~2,800 lines out)

- **Delete:**
  - `directWaveTaskContractStaleReason` (`direct_wave_authority.go:158`) and its uses at
    :389, :765, :1089
  - the five `*MatchesTaskAuthority` functions (`direct_run_authority.go:79-190`).
    Replace them with one check: directive active, task in wave, wave armed.
  - the material fingerprint (`v7_wave_authorization.go:36-243`) and the stale branch
    of `waveAuthorizationProjection` (:272)
  - per-generation `run_authorizations` matching (row 6)
- **Call sites:**
  - `daemon.go:2200,2517-2524,4236,4312-4319,4683`
  - `serve_runs.go:806`, `work_recovery.go:587,910-916`
  - `armed_wave.go:370`, `commands_v7.go:4085`, `work_session_readiness.go:23`
  - `self_service_admission.go:113-115`
- **Tests:** about 1,500 lines of `direct_wave_authority_test.go` (1,993) and
  `v7_wave_authorization_test.go` (467).
- **Risk:** medium. Arming, pause and resume must still work. Keep
  `directWaveFrontierOwnedPathConflict` (:328) and `waveIntegrationBaseClean` (:304).

### Cut 5. Non-exec Codex adapters and the ChatGPT external loop (~13,500 lines out)

None of these is selected in `~/.config/tusker/config.yaml` (harnesses used:
`claude-code`, `codex_exec`, `devin`, `muse`) or in `.tusker/config.yaml:27-42`.

- **codex_cloud:** `runner_codex_cloud.go` 746 (445). DB: 0 runs with a `cloud_task_id`.
- **ChatGPT external loop:**
  - Files: `daemon_external_loop.go` 899, `automation_external_loop.go` 898,
    `automation_external_collect.go` 942 (tests ~2,100).
  - DB: `external_loop_events` = 0, `architect_continuations` = 0.
  - Hooks: `daemon.go:1019,1203,1216,4725,4880-4934,7058,7298,7627-7660`, `runner.go:129-175`.
  - Cut it together with codex_cloud, because the loop's collect step needs codex_cloud (`daemon_external_loop.go:703-720`).
- **Codex ACP layer:**
  - Files: `runner_acp_codex.go` 967, `runner_acp_codex_live.go` 544, `acp_adapter_*` 1,345
    (tests ~1,100). DB: 8 runs, the last on 2026-09-23. That is the day decision
    D2 moved Codex to `codex exec`.
  - Keep `runner_acp.go`, `acp_permission.go` and `internal/acp`, because Devin runs through ACP
    (`daemon.go:6900`). Strip their `codexPlan` branches (`runner_acp.go:22,233-505`).
  - Drop the `tusker acp` command (`cli.go:207-214`).
- **codex_app_server:**
  - Files: `runner_codex_live.go` 1,843, plus the `CodexRunner`/`CodexAppServerRunner` half of `runner_codex.go`.
  - First move about 30 shared helpers (`appendRawLogLine`, `waitForExit`,
    `writeRunnerStatusFile`, the git-mutation checks and others) to a neutral file.
  - Delete the silent routing at `runner_codex.go:20-21,88,107`: an exec profile whose command contains `app-server` is quietly re-routed.
  - Tests: `runner_live_test.go` (1,604). **17 test files use the live runner as their fake.** They need a new exec-based fake. This is the costliest part.
- **Shared registries to edit:**
  - `runner.go:18,30`, `daemon.go:6884-6910`, `runner_wrapper.go:18,209-260`
  - `workflow.go:98,845,870`, `workflow_validate.go:196-206,301`
  - `capabilities_cmd.go:118-119`, `runner_preflight.go:460`, `runner_profiles.go:666,1148`
  - `agent_access_approval.go:507,743`, `execution_commands.go:159,277`, `codex_execution_adapter.go:54`
- **Risk:** medium. The store tables for the external loop need tolerant reads or a
  migration. For the app server, rewrite the test fakes before deleting the runner.

### Cut 6. Scheduled promotion, departures, full-gate provider, batch gate (~14,000 lines out)

- **Files:**
  - `departure_*` 2,114 (2,480)
  - `scheduled_promotion.go` 1,663 (1,430)
  - `promotion_failure.go` 185 (183)
  - `v7_full_gate_provider.go` and `v7_full_gate_state_root.go` 2,858 (1,710)
  - `batch_gate.go` 510
  - `morning_brief.go` 627 (410)
- **Usage:** DB: `departure_runs` = 0, `batch_gate_runs` = 0. `batch_gate.enabled: false` in
  `.tusker/WORKFLOW.md:167`, and no config sets `isolation_provider`.
- **Callers to cut:**
  - `daemon.go:1113-1116` (departure scheduling)
  - `v7_land_cmd.go:3100-3260` (provider receipts inside land)
  - `gate_tier.go`, `gate_ledger.go`, `work_recovery.go`, `serve_runs.go`
  - `resource_lease.go` promotion paths
  - `v7_logbook_cmd.go:71-72` flags
  - `/api/morning-brief` (`serve_command.go:462,741`)
- **Risk:** high coupling, zero use. Land must keep working from the UI
  (`serve_actions.go:584-586`). Do this after cut 1 so the land path is already simpler.

### Cut 7. Feedback-signal pipeline, improve, xcode, trace replay (~8,900 lines out)

- **Files:**
  - `v7_feedback_ingest|signal|signals_cmd|review|promote_plan.go` 4,179 (1,251)
  - `v7_feedback_canon.go` 685 (219). Move `renderV7DomainCanonProhibitions` first.
  - `v7_improve_cmd.go` 942. Move `firstMarkdownHeading` to `serve_docs.go` first.
  - `xcode_doctor.go` 607 (218)
  - the CLI half of `trace_replay.go` 699. Keep `evaluateV7ReplayVerificationRow`
    for `v7_proof_cmd.go:1061`.
- **Keep:** `feedback add` and `feedback digest` (`v7_feedback_cmd.go`), because Serve uses them
  (`serve_actions.go:1504,1810`).
- **Usage:** there are four feedback notes in this repository in total.
- **Risk:** low. Drop the hook at `commands_index.go:619` and the xcode references in
  `skills/tusker/references/XCODE_BUILD_STATE.md`.

### Cut 8. Small adversarial leftovers (~800 lines out)

- Rows 18, 19 and 22 of the table.
- Row 23: remove the auxiliary hiding (F13).
- Drop the orphan `human_control_challenges` table: no Go code references it.

## 4. Dead or unused code and the CLI surface

### Adapters

`tusker capabilities --json` lists eight runner adapters: `claude-code`, `codex`,
`codex_acp`, `codex_app_server`, `codex_cloud`, `codex_exec`, `devin`, `muse`.

Live DB run counts by runner: `codex_exec` 298, `codex_acp` 8 (retired
2026-09-23), `devin` 7, `claude-code` 5, `muse` 1.

Keep `claude-code`, `codex_exec`, `devin` and `muse`. Make `codex` an alias for
`codex_exec`, or drop it. Cut the other three (cut 5).

### Pipelines with zero rows or zero configuration

| Surface | Evidence | Verdict |
| --- | --- | --- |
| External ChatGPT loop | 0 `external_loop_events`, 0 `architect_continuations`, no caller in skills, scripts or docs | CUT |
| Departures and scheduled promotion | 0 `departure_runs`, 0 `batch_gate_runs`, `batch_gate.enabled: false` | CUT |
| Full-gate isolation provider | `isolation_provider` unset everywhere | CUT |
| Landing authority issuance | 0 `landing_authority_issuances` | CUT |
| Worker deliveries and attention (Devin/Muse HTTP plumbing) | 0 `worker_deliveries`, 0 `worker_attention` | Decide in V7. Keep for now, because Devin and Muse are next in the campaign |
| Access approvals | 0 `agent_access_approvals` | KEEP the broker (the destructive guard uses it), CUT its digests |
| `demo` | Used by `seed.sh:60`, `Makefile:179,188`, `scripts/test-real-work-project.sh` | KEEP |
| `mcp serve` | Started for every worker (`worker_mcp_launch.go:30`) | KEEP |

### CLI verbs

`capabilities` lists 101 commands under **90 distinct top-level verbs**. That is
more than the campaign's estimate of 55, because hidden verbs count too. The
Tusker skill (`skills/tusker/`) uses about 30 of them:

`docs`, `wave`, `validate`, `reindex`, `show`, `closeout`, `work`, `verify`, `task`,
`new`, `doctor`, `projects`, `list`, `search`, `packet`, `runs`, `execution`,
`demo`, `capabilities`, `skill`, `runner`, `proof`, `message`, `init`, `config`,
`run`, `refresh`, `reconcile`, `purge`, `finish`, `daemon`, `automation`.

The UI talks to Serve over HTTP, not the CLI.

The target is about 30 top-level verbs:

| Action | Verbs |
| --- | --- |
| Drop with their cut | `xcode`, `improve`, `departure`, `acp`, `gate-run`, `logbook --morning-brief`, `trace replay`, `feedback ingest/signals/review/promote` |
| Drop as aliases | `relaunch` (= `reset`), `claim` (= `work start`), `propose` (already deprecated) |
| Fold into `work` | `heartbeat`, `release`, `handoff`, `finish`, `attempt` (a worker's own session lifecycle) |
| Fold into `gate` | `gate-ledger` |
| Fold into `status`/`digest` | `escalate`, `brief`, `closeout status` |
| Hide as internal | `runner-wrapper`, `mcp`, `sync-repo-contract`, `migrate` |
| Verify first, then likely drop | `streams`, `dashboard`, `context` (Codex JSONL audit), `factory`, `domain`, `knowledge`, `attachments`, `proposal` (no Serve or doc caller found by grep) |
| Keep, but check Serve use | `vault`, `state`, `redact`, `redrive`, `actor` (Serve source mentions them; confirm these are command calls and not JSON keys) |

Also add the missing verbs the campaign asked for: `wave list` (F4) and `runs list` (F9).

## 5. Order of work

Rules for every slice:

- Each slice owns the files listed. No two slices in the same phase own the
  same file.
- `daemon.go`, `runtime_store.go`, `cli.go`, `workflow.go` and
  `workflow_validate.go` are **hot files**. A slice may edit only the line
  ranges named in its brief. Slices that touch a hot file run one after
  another, in the order given, each rebased on the last.
- Agents make their slice compile with `go build ./... && go vet ./cmd/tusker`
  and skip the full suite. One central gate runs `rtk proxy go test ./...`
  after each phase. Take a test baseline first.
- **Profile:** "Sol-low" is `gpt-6-sol` at low effort; "Devin" is Devin SWE-2.
  Slices marked Tier 3 change a contract and go to Claude or `gpt-6-sol`
  medium, with a cross-review.

### Phase 0: unblock the live campaign (tiny, do now)

| Slice | Owns | Change | Profile |
| --- | --- | --- | --- |
| 0.1 F17 | `daemon.go:2903,2959` | `false` → `sessionResumable` | Sol-low |
| 0.2 F16 | `agent_messages.go`, `cli.go:170-176` | Nudge the daemon with `reconcile_project` after an answer; add `message reply` to the mutating list | Sol-low |
| 0.3 Circuit | `sentinel.go`, `daemon_sentinel_test.go`, `automation_commands.go:661`, `daemon.go:1447,1654` | Auto-close when clean; scope the blocker to the project; tell the owner through the escalation digest | Sol-low (after 0.1, because both edit `daemon.go`) |
| 0.4 Demo defaults | `demo_cmd.go`, `serve_command.go:1367-1444`, `internal/serve/ui/src/types/domain.ts` | Seed with `completion_reactor: disabled`; show demo projects with a badge instead of hiding them | Devin |

### Phase 1: delete unused paths (low judgment)

| Slice | Owns | Hot-file edits | Profile |
| --- | --- | --- | --- |
| 1.1 xcode, improve, trace replay CLI | `xcode_doctor*`, `v7_improve_cmd*`, CLI half of `trace_replay.go`, `serve_docs.go` (receives the helper), `skills/tusker/references/XCODE_BUILD_STATE.md` | `cli.go:355,397-404` | Devin |
| 1.2 Feedback-signal pipeline | `v7_feedback_ingest*`, `_signal*`, `_signals_cmd*`, `_review*`, `_promote_plan*`, `_canon*`, `commands_index.go:619`, `commands_v7.go` (receives canon helper) | `cli.go:382-395` | Devin |
| 1.3 codex_cloud + external loop | `runner_codex_cloud*`, `daemon_external_loop*`, `automation_external_*`, `external_architect_routing_test.go`, `execution_commands.go`, `codex_execution_adapter.go`, `runner.go:129-175` | `daemon.go` external and cloud ranges (section 3, cut 5), `runtime_store.go` external tables (tolerant read), `workflow.go:98,345,870`, `workflow_validate.go:301`, `cli.go:608-616` | Sol-low |
| 1.4 Codex ACP layer | `runner_acp_codex*`, `acp_adapter_*`, `runner_acp.go` (strip `codexPlan`), `agent_access_approval.go:743`, `capabilities_cmd.go` | `daemon.go:6902-6910`, `runner_wrapper.go:18,260`, `workflow_validate.go:196,206`, `cli.go:207-214` | Sol-low (after 1.3) |
| 1.5a App server: move helpers | `runner_codex_live.go` → new `runner_live_common.go` (move only) | none | Devin |
| 1.5b App server: new test fake | the 17 test files that use the live fake, `runner_live_test.go` | none | Sol-low |
| 1.5c App server: delete | `runner_codex_live.go`, the app-server half of `runner_codex.go`, `runner_preflight.go:460`, `runner_profiles.go:666,1148`, `agent_access_approval.go:507` | `daemon.go:6884`, `runner_wrapper.go:209-213`, `workflow.go:845`, `workflow_validate.go:203` | Sol-low (after 1.4) |

### Phase 2: cut adversarial guards (contract changes)

| Slice | Owns | Hot-file edits | Profile |
| --- | --- | --- | --- |
| 2.1 Pass handler | `completion_reactor.go` (rewrite down to handler plus handback), `completion_reactor_mode.go` → bool, `v7_close_ceremony.go` | `daemon.go:1691,3146,3886,3965` | Tier 3 |
| 2.2 Delete the completion chain | `completion_authority.go`, `completion_worker_safety.go`, `v7_completion_receipt.go`, `v7_close_authority.go`, `landing_authority.go`, `review_result.go`, `factory_operations.go`, `demo_check.go`, `v7_control_cmd.go`, `runner_claude_live.go:172`, `runner_exec.go:129`, their tests | `daemon.go:4477-4589`, `v7_validation.go:207,1739-1749`, `workflow.go:316,635-656` | Sol-low (after 2.1) |
| 2.3 Wave drift | `direct_run_authority.go`, `v7_wave_authorization.go`, `self_service_admission.go`, `armed_wave.go`, `work_session_readiness.go`, `serve_runs.go:806` | `daemon.go:2200,2517-2524,4236,4312-4319,4683`, `runtime_store.go` `run_authorizations` generation matching, `direct_wave_authority.go:158,389,765,1089` | Tier 3 |
| 2.4 Wakeup queue | `agent_coordination.go`, `agent_messages.go`, `agent_coordination_e2e_test.go` | `daemon.go:782,797`, `runtime_store.go:1239,1640-1643` | Sol-low |
| 2.5 Small leftovers | `private_path.go`, `acp_permission.go`, `agent_access.go:506`, `v7_verification_execution.go`, `daemon_event_log_failure.go` | none | Devin |

### Phase 3: departures and scheduled promotion (after phase 2)

| Slice | Owns | Hot-file edits | Profile |
| --- | --- | --- | --- |
| 3.1 Departures + morning brief | `departure_*`, `morning_brief.go`, `v7_logbook_cmd.go`, `promotion_failure.go` | `daemon.go:1113-1116`, `serve_command.go:462,741`, `cli.go:359-370` | Sol-low |
| 3.2 Scheduled promotion + full-gate provider + batch gate | `scheduled_promotion.go`, `v7_full_gate_provider.go`, `v7_full_gate_state_root.go`, `batch_gate.go`, `gate_tier.go`, `gate_ledger.go`, `resource_lease.go` promotion paths | `v7_land_cmd.go:3100-3260`, `work_recovery.go`, `workflow.go:221-225` | Tier 3 (land must keep working) |

### Phase 4: CLI fold

| Slice | Owns | Profile |
| --- | --- | --- |
| 4.1 Aliases and folds | `cli.go` command table, `capabilities_cmd.go`, `skills/tusker/` references | Devin |
| 4.2 Missing verbs | new `wave list` in `v7_wave_cmd.go`; new `runs list` in `run_runtime_commands.go` | Devin |

After each phase, rerun the live qualification step that the phase unblocked:
Q6 after phase 0, and Q7 to Q9 after 2.2.
