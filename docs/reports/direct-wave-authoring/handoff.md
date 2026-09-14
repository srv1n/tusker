# W-0025 continuation handoff

This write-up hands the remaining wave (FLW-T-0050, 0048, 0046, 0049) to the
next team after the FLW-T-0045 stabilization pass.

## Working tree warning

`main` is a shared, heavily dirty working tree. Do **not** reset, stash,
commit, or clean it. Foreign in-flight work for W-0017/W-0023 and other
tracks is interleaved with Tusker's own changes — preserve every existing
edit, including files you did not touch. Do not add or remove code comments.
Do not mutate Tusker task lifecycle records from the shell.

## Completed in this session

- **FLW-T-0043 — direct task/wave authoring.** `tusker new task`, batch
  wave authoring, atomic wave creation, `contract_fingerprint` at creation
  and on every `task update` material mutation.
  Sources: `cmd/tusker/direct_authoring_cmd.go`, `cmd/tusker/commands_v7.go`.
  Report: `docs/reports/direct-wave-authoring/direct-authoring.md`.
  Verified: `go test ./cmd/tusker -run '^TestDirectWaveAuthoring' -count=1`
  → ok (10 substantive tests in the final focused pass).
- **FLW-T-0044 — delivery-plan migration.** `tusker migrate direct-waves
  inspect [--json]` (byte-for-byte read-only) and `apply --confirm
  sha256:<fingerprint> [--allow-partial] [--fail-after-unit <n>]`
  (all-or-nothing default, per-unit only under allow-partial). Canonical
  `contract_fingerprint` / `dependency_contracts` /
  `close_policy_snapshot` / `proof_contract` / `proof_results` /
  `strict_proof_lineage` / `migration_receipts` semantics with strict
  authority validation and cross-scope resolution.
  Sources: `cmd/tusker/direct_wave_migration.go`,
  `cmd/tusker/direct_wave_migration_test.go`,
  `cmd/tusker/delivery_verification_contract.go`.
  Report: `docs/reports/direct-wave-authoring/migration.md`.
  Verified: `go test ./cmd/tusker -run '^TestDirectWaveMigration' -count=1`
  → ok (~17s, 16 tests).
- **FLW-T-0047 — external agent registration/routing.** Contact
  registration and truthful capability routing; no fabricated execution.
  Sources: `cmd/tusker/agent_contacts.go`,
  `cmd/tusker/agent_coordination.go`, `cmd/tusker/agent_messages.go`,
  `cmd/tusker/execution_commands.go`, `cmd/tusker/execution_ledger.go`,
  `cmd/tusker/external_architect_routing_test.go`.
  Report: `docs/reports/direct-wave-authoring/contacts.md`.
  Verified: `go test ./cmd/tusker -run '^TestExternalArchitectRouting'
  -count=1` → ok (8 tests); `go test ./cmd/tusker -run
  '^TestAgentMessages' -count=1` → ok.
- **FLW-T-0045 — direct review/start authority (stabilized this pass).**
  `tusker wave review <WAVE-ID>`, `tusker wave start <WAVE-ID> --mode
  background --by human:|operator:<name>`, `tusker task start <TASK-ID>
  --mode interactive|background --by <actor> [--current-workspace]`, plus
  Serve `GET /api/projects/{p}/waves/{w}/review` and POST start actions.
  Shared `tusker.wave-review/v1` builder; durable-only material
  fingerprint; locked revalidation before authorization; task-scoped
  run directives; read-only review runtime access.
  Sources: `cmd/tusker/direct_wave_authority.go`,
  `cmd/tusker/direct_wave_authority_test.go`, `cmd/tusker/wave_execute.go`,
  `cmd/tusker/v7_wave_authorization.go`, `cmd/tusker/work_session_cmd.go`,
  `cmd/tusker/serve_actions.go`, `cmd/tusker/serve_command.go`,
  `cmd/tusker/cli.go`, `cmd/tusker/capabilities_cmd.go`.
  Report: `docs/reports/direct-wave-authoring/authority.md`.
  Verified: `go test ./cmd/tusker -run '^TestDirectWaveAuthority' -count=1`
  → ok; `go test ./cmd/tusker -run '^TestDirectStartAuthority' -count=1`
  → ok; `go test ./cmd/tusker -run '^TestWorkSession' -count=1` → ok,
  175.3s.

## Proof classification

All verification is fixture-level and locally executed (`go test` on temp
vaults/runtime stores). Provider-live proof was **NOT RUN**: no installed
runner, daemon, browser, or external connector was exercised. The full
test suite was intentionally not run; focused groups only. The migrated
production vault (this repo's own `.tusker`) has **not** had migration
applied — `migrate direct-waves` was only inspected, never applied here.

## Remaining tickets in exact order

1. **FLW-T-0050 — continue authorized waves automatically with reliable
   pause and resume (D8).** Key acceptance: wave Start releases eligible
   roots and later dependency stages without another click while
   unrelated waves stay unapproved; Pause/Resume/task-scoped starts
   preserve scope; offline runtime/restart/duplicate/competing claims
   recover without duplicate attempts or false running. Pick-up point:
   `directWaveStart` already persists durable authorized intent before
   queueing (the `Waiting` recovery seam); the daemon must advance
   frontiers and handle pause.
2. **FLW-T-0048 — lean CLI authoring with complete technical worker
   packets (D6 guidance/packets).** Key acceptance: no administrative
   fields for fresh authoring; substantial specimens keep concrete
   source/schema decisions and edge cases; shipped help/skills match real
   commands; one-action scoped Start in guidance. Depends on 0043/0045
   interfaces being final — do after 0050 so packet Start semantics match
   continuation behavior.
3. **FLW-T-0046 — replace plan discovery and Start screens with wave
   review (D4 UI).** Key acceptance: no plan picker/path entry; blocked
   routes and human actions suppress inappropriate starts; routed browser
   evidence; consistent one-action Start/Pause/Resume and waiting states.
   Consumes the Serve API shipped in 0045; do after 0050/0048 so the UI
   reflects settled Start and packet semantics.
4. **FLW-T-0049 — delete delivery-plan implementation and qualify the
   migrated task-wave flow (D7 + qualification).** Key acceptance:
   deletion scan finds no active delivery-plan command/endpoint/parser/
   file dependency and no dormant backup; migrated flows preserve
   dependency/gate/route/proof/independent-review semantics with no plan
   input; Q1–Q14 qualification matrix with independent review. Last —
   requires 0050/0048/0046 so nothing still calls the legacy path.

## Known remaining risks

- Legacy delivery-plan implementation is still present and callable for
  unmigrated D4 callers; the new direct paths never call it but nothing
  has been deleted yet.
- D8 (FLW-T-0050) autonomous advancement and pause recovery are **not**
  implemented; authorized-but-unqueued intent is durable and waits.
- The Serve UI is not migrated (FLW-T-0046 pending).
- Shipped skills/help docs are not yet updated for the new commands
  (FLW-T-0048 scope).
- Physical migration has not been applied to this real dirty vault.
- `internal/serve/ui/dist` contains generated dirty artifacts — foreign
  build output, leave alone.
- No full-integration or independent review pass exists for the 0043–0047
  stack; FLW-T-0049 owns the Q-matrix.
- External live connectors are unsupported and NOT RUN (0047 registered
  capability only).
- `migration.md` restart example needs `--allow-partial` alongside
  `--fail-after-unit` (that flag is valid only under allow-partial);
  fix the example when docs are next touched.
- Red tests observed in the dirty checkout (see below).

## Recommended first commands

```bash
git status --short                                  # inspect, never reset
tusker show FLW-T-0050 --force                      # task packets
tusker packet FLW-T-0050 --for agent --force
go test ./cmd/tusker -run '^TestDirectWaveAuthority' -count=1
go test ./cmd/tusker -run '^TestDirectStartAuthority' -count=1
go test ./cmd/tusker -run '^TestWorkSession' -count=1
go test ./cmd/tusker -run '^TestDirectWaveMigration' -count=1
tusker migrate direct-waves inspect --json          # read-only inventory
```

Do not start `tusker daemon run`, invoke automation dispatch, or change
profile/model-level configuration as part of pickup.

## Do not do

- No delivery compatibility layer, no `*_BKP`/backup copies.
- No broad deletion — physical removal is FLW-T-0049 only.
- No fabricated live proof — label fixture vs runtime evidence honestly.
- No bypassing human gates or lifecycle-record edits from the shell.
- No resetting/stashing/committing dirty foreign work.
- No implicitly enabling automation or profile/model-level mappings.

## Current verification

Exact commands and outcomes from this stabilization pass:

- `gofmt -w cmd/tusker/direct_wave_authority.go
  cmd/tusker/direct_wave_authority_test.go cmd/tusker/wave_execute.go` —
  gofmt completed; `git diff --check` is clean.
- `go build ./cmd/tusker` — clean.
- `go test ./cmd/tusker -run '^TestDirectWaveAuthority' -count=1` —
  **ok** (11 tests incl. soft-edge frontiers, project-scoped runtime
  facts, full-body instructions, cross-wave dependency contracts,
  stored-fingerprint staleness, locked revalidation, ownership
  admitted-vs-conflict, read-only runtime).
- `go test ./cmd/tusker -run '^TestDirectStartAuthority' -count=1` —
  **ok** (7 tests incl. directive consumed-once/stale-material refusal,
  locked task material queueing, interactive `--by` requirement,
  interactive claim/gate/owner cases).
- `go test ./cmd/tusker -run '^TestWorkSession' -count=1` —
  **ok, 175.3s**.
- Directive/daemon group `^(TestRunDirective|TestDaemonHonorsDirective|
  TestDaemonTaskDirective|TestDirectedClaim|TestInteractiveCannotDispatch
  Directive|TestServeRunDirective|TestServeWaveExecute)` — **9 failures**
  with reasons `standard execute profile mapping is empty`, `demanding
  ready task must declare resolvable spec_refs links`, and `expected
  one-shot dispatch refusal`. They fail at dispatch/route resolution
  before directive authority matching, so they appear to be foreign
  fixture/admission incompatibilities; baseline causality was not
  independently established. Assigned for follow-up: reconcile the
  fixtures with the model-levels/spec_refs admission requirements.
- `TestWaveAuthorizationFingerprint` failure observed in the dirty
  checkout: the test reads `docs/specs/delivery.md`, which the current
  fixture does not create (spec material lives under `.tusker/specs`).
  It appears to be an existing fixture-path failure; baseline causality
  was not independently established.
