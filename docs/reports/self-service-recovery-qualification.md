# Self-service recovery qualification (TSK-T-0048)

Bounded consumer-level qualification of the CLI and Serve recovery surfaces
across disposable process boundaries, executed 2026-09-22 for wave W-0038
(lifecycle and restart qualification).

## Proof boundary and identity

- Source: `cbc14957dd5d73635da0ecf0b00b1b9426977310` (working tree carries
  unrelated in-progress changes outside the owned paths; see git status).
- Reference installed binary (NOT under test here):
  `archive/pre-convergence-main-20260727-430-gcbc14957-dirty`
  (`tusker capabilities --json`, vcs time 2026-09-22T01:18:01Z). The CLI
  behavior below is qualified against the source-built test binary.
- Go suite under test: `TestSelfServiceProcess` in
  `cmd/tusker/self_service_process_test.go` (this checkout, `go test`
  `./cmd/tusker -run '^TestSelfServiceProcess' -count=1`). Helper
  processes are the same test binary re-executed, so CLI behavior is
  qualified against the source build, not the older installed binary
  (which still reports `doctor` as unknown).
- UI suite reused as-is: `bun test --cwd internal/serve/ui
  test/self-service-recovery.test.ts` (7 pass, 0 fail on 2026-09-22).
- State isolation: every scenario uses temp-dir vaults (bootstrap direct-wave
  fixtures) and temp-dir `TUSKER_STATE_ROOT` state roots. Each disposable
  helper process is a cold re-exec of the test binary running exactly one CLI
  argv through the real parser and dispatch; the harness store is closed
  across every helper boundary. No resident daemon was started and no
  operator project was touched.
- What this report does NOT claim: installed-app and live-provider
  qualification (NOT RUN, see below). A provider-free pass is never called
  installed/live-provider qualification.

## Scenario verdicts

| Scenario | Verdict | Evidence |
| --- | --- | --- |
| cli_start_queues_root | PASS | Helper `wave start --mode background` exits 0; store holds exactly the root directive bound to the armed fingerprint. |
| reopened_review_reads_frontier | PASS | Fresh helper `wave review` renders `authorization authorized` with the queued root after the start process exited. |
| cli_task_start_refuses_dependent | PASS | Helper `task start APP-T-0002 --mode background` exits non-zero with `DEPENDENCY_WAITING` naming `APP-T-0001`; no directive is queued. |
| cli_verify_add_plans_check | PASS | Helper `verify add` (pending row) exits 0 with `Added`; only the verification gate executor may record pass/fail. |
| acceptance_dispatches_next | PASS | Claim -> terminal success -> ready -> reviewer accept -> daemon frontier advance queues the dependent under the exact armed authority with exactly one attempt. |
| restart_reads_settled_state | PASS | Fresh helper `wave review` after acceptance shows `APP-T-0001 completed` and the queued dependent; attempt count stays one. |
| no_stream_rest_readback_queued | PASS | `handleWaveReviewAPI` (httptest, no stream) after the cross-process journey reports the dependent queued, matching the CLI surface. |
| post_arm_contract_edit_stales | PASS | A further helper `verify add` after arming changes the dependent contract; fresh `wave review` reports `authorization stale` and the frontier admits nothing until re-armed. |
| doctor_reports_actionable_fault | PASS | Helper `doctor W-0001` on the queued wave with no daemon poll exits 1 with `doctor-daemon-absent`, `Next actor: operator`, and typed no-repair guidance. |
| doctor_reports_structured_json | PASS | Helper `doctor W-0001 --json` exits 1 with schema `tusker.doctor/v1`, `exit_code` 1, primary code `doctor-daemon-absent`, classification `recoverable_fault`. |
| doctor_reports_normal_wait | PASS | Helper `doctor APP-T-0002` on the dependency wait exits 0 naming `APP-T-0001`: a normal wait is not dispatchability. |
| doctor_refuses_invalid_invocation | PASS | Bare `doctor` exits 2 with usage; `doctor NOPE-0000` exits 2 naming the missing target. |
| unsupported_repair_refuses | PASS | `repair` is unimplemented (spec-proposed only): the CLI refuses `Unknown command: repair` with a non-zero exit and no `ok:true`. |
| installed_app_qualification | NOT RUN | Explicitly unclaimed. Requires the installed Mac app destination named by the human gate process; no installation was performed. |
| live_provider_qualification | NOT RUN | Explicitly unclaimed. Requires spend on live providers and an installed build; this pass is provider-free by design. |

## Discovered gaps (against owning prerequisites, not absorbed here)

1. `tusker doctor` exists in the source build (`executionDoctorCmd`) but
   not in the older installed binary, which still reports it as unknown.
   `tusker repair` remains spec-proposed only with no command. Diagnosis
   across the boundary is `doctor` exit codes 0/1/2 plus `wave review` /
   `task start` refusal codes and the shared admission predicates.
2. Actor vocabularies differ by surface: admission refusals speak to the
   claimant (`agent`), while the wave projection names the resolver
   (`daemon` for waits, `operator` for pauses/faults). Pinned per branch in
   `TestSelfServiceJourneysA2`; unifying them belongs to the diagnosis
   tasks, not this qualification.
3. The review member message for any record-level staleness is the generic
   "task contract drifted from its stored fingerprint; rebind required",
   even when the precise cause is a `state_rev` mismatch on ledger-only
   bytes. The precise reason is one predicate call away
   (`directWaveTaskContractStaleReason`); projection follow-up belongs to
   the wave/task flow owners.
4. Planning a verification row after arming (even via the sanctioned
   `verify add`) changes task contract material and stales the wave until
   re-armed. This is the designed exact-material fence, but authors should
   plan verification before Start — as this suite now does.

## Suite linkage

`TestSelfServiceProcess` asserts this report exists and names every scenario
row above, so the report cannot drift from what was executed. The
BLOCKED/NOT RUN rows never count toward green: `journeysAllGreen` requires
every executed scenario PASS, and any FAIL/BLOCKED/NOT RUN verdict (or an
empty matrix) summarizes non-green.
