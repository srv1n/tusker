
## 7. Dispatched agents follow AGENTS.md's Tusker steps and stall on dependency gates (medium)
- A Codex lane told to build TSK-T-0009 ran `tusker start TSK-T-0009 --current-workspace` (per AGENTS.md "Tracked work") and got `WORK_SESSION_DEPENDENCY_BLOCKED: Dependency TSK-T-0008 is not satisfied` because TSK-T-0008 was in `review`, not `done`. It stopped without writing code (~10 min lost).
- `tusker packet` and `tusker runs inspect`, which AGENTS.md names, are not available; the agent fell back to `tusker show --packet agent --force`.
- Two lanes also reported `tusker done` refusing with `proof_required:focused_test` (same as bug 3) and one with "stale contract lists missing deploy/workers".
- Expected: a dependency in `review` with its proof recorded should satisfy a hard dependency for starting dependent work (or the refusal should say "dependency in review; coordinator may override"), and AGENTS.md's command names should match the CLI.

## 8. Test classifier ignores wrapped test commands — root cause of bug 3 (high)
- `v7CommandInvokesTest` (cmd/tusker/v7_proof_cmd.go:2004) matches only program + first argument: `cargo test`, `make test` count; `make cargo-any CARGO_ARGS='test …'` (mandatory in AGENTS.md) does not. Every Rust task here fails `proof_required:focused_test`/`broad_test` with passing checks.
- Fix: recognise `CARGO_ARGS='test …'`/nextest, or a per-repo test-runner pattern list in WORKFLOW.md.

## 9. Verification timeout: 10 min hard cap, shared by all rows, cannot be raised (high)
- `const v7VerificationCommandTimeout = 10 * time.Minute` (v7_verification_execution.go:21). One deadline is set before the row loop, so row 2 gets what row 1 left. `verification-timeout-ms` can only lower it.
- 2026-10-24: cold iter test builds took 13–15 min each on the Air; BEN-T-0001 row A2 timed out at 9m58s.
- Fix: per-row timeout, repo-configurable ceiling, or a warm-up phase that does not count.

## 10. Budget exhaustion discards the already-observed result (medium-high)
- v7_verification_execution.go:187 returns "total wall budget exhausted" inside the loop, before `updateV7VerificationLedger` saves anything. The timed-out row's fail result and output are lost, and the message hides which row used the budget (BEN-T-0002 lost 10 min this way).
- Fix: save rows already executed, then refuse naming the row that consumed the budget.

## 11. Refusals do not explain why a passing check did not count (medium)
- "machine proof is incomplete: proof_required:focused_test" on tasks whose rows all passed; `tusker show --proof` adds nothing. Needed the Go source.
- Fix: "row A1 passed but `make cargo-any …` is not a recognised test runner".

## 12. Changing proof_required invalidates passed command receipts (medium)
- proof_required is in the contract fingerprint, so `--proof-mode inline --required command` reset every passed row to pending though no command changed; forces full re-runs.
- Fix: exclude proof policy from command-receipt invalidation.

## 13. Acceptance "Proof" column ignored; uncovered acceptance found only at done time (medium)
- Acceptance rows saying "Verification A1" for A2–A5 count as unproven; only the verification Covers column matters. Nothing flagged it at `tusker new`/`edit`/`check`.
- Fix: honour the column, or flag uncovered acceptance IDs at authoring time.

## 14. Test proof is gameable and focused == broad (medium)
- `"test": nil, "[": nil` are test runners, so `[ -f x ]` satisfies focused_test. Both focused_test and broad_test share one case: one command satisfies both.
- Fix: drop `test`/`[`; define broad (e.g. whole package, no name filter).

## 15. Identical command rows execute repeatedly (low)
- BEN-T-0008 had three rows with the same command; it ran three times. Fix: run once per task, copy receipt.

## 16. Usability (low)
- No `--proof-required` on `edit`; working route `--proof-mode inline --required command` is not in `tusker help edit`.
- `--verify-remove` takes one index; indices shift, so remove highest-first.
- `tusker done` is serial and host-unaware (had to wrap it in RZN_BUILD_SLOT env to use the Air) and prints nothing while a check runs.
- Every command rewrites dashboards and generated indexes, so the shared workbench always shows dozens of modified `.tusker/` files, complicating pathspec commits.

## 17. Check failures keep only a hash of the output (medium)
- 2026-10-24: BEN-T-0004 and BEN-T-0007 were refused with only "exit status 2". The receipt stores `output_sha256`, not the text, so finding the cause meant rerunning the command by hand (it passed).
- Fix: keep the last ~50 lines of a failing check's output under `.tusker/scratch/<TASK-ID>/` and print the path in the refusal.

## 18. Checks run against the live shared tree, so a concurrent commit poisons the run (medium)
- 2026-10-24 09:03 IST: another lane landed a Cargo.toml/Cargo.lock change during `tusker done`. Warm builds went stale (BEN-T-0001 then hit the 10 min cap) and two checks failed mid-change; both are recorded as plain fails.
- Fix: run checks from a frozen snapshot of the task's commit (cargo-any already supports frozen staging), or at least record the tree hash per row and flag "tree changed during verification" instead of fail.

## 19. One `done` batch does not order dependencies, and refusal order is inconsistent (low-medium)
- Answers bug 7's open question: with BEN-T-0001 first in the list and refused, every dependent was refused. Some dependents (0002, 0003, 0005, 0006) were refused for the dependency before their checks ran; others with the same single dependency (0004, 0007) ran their checks first and were refused for those.
- Correction (09:34 IST run): when the earlier task passes, one batch does work in order (BEN-T-0001 reached review, then its dependents recorded in the same call), and a dependency in review satisfies it. The remaining issue is only the refused-dependency case.
- Fix: skip (not refuse) dependents of a refused task with one line saying so, and check dependencies before spending check time, consistently.

## 20. Checks run with a stripped environment, silently dropping build routing (high)
- `v7VerificationCommandEnv` (v7_verification_execution.go:1031) keeps only HOME, PATH, TMPDIR/TMP/TEMP, LANG/LC_*, TZ, GOCACHE, GOMODCACHE, CARGO_TARGET_DIR and TUSKER_VALIDATION_LOCK_DIR. `RZN_BUILD_SLOT=rzn-spare RZN_ROUTE_NO_LOCAL=1 tusker done …` therefore built cold on the local Mac instead of the warm Air, and BEN-T-0001 hit the 10 min cap twice (2026-10-24 08:47 and 09:12 IST). Nothing says the variables were dropped.
- Workaround used: inline the variables in the check command (`command: RZN_BUILD_SLOT=… make cargo-any …`), which bakes a host into the task contract.
- Fix: a repo-configurable env allowlist in WORKFLOW.md (e.g. `RZN_*`), and print the dropped variable names once per `done` run.

## 21. Notes from creating BEN-T-0009/0010 (2026-10-24)
- Narrows entry 13: `tusker new task` does warn at creation when no verification command can satisfy `proof_required` ("…has no verification command that can satisfy it"). Good. The example it suggests, `test ...`, is the shell builtin that makes entry 14 gameable; suggest `cargo test …` / the repo's wrapper instead.
- New tasks still default to `proof_required: [focused_test, broad_test]`, so every Rust task in this repo needs a follow-up `tusker edit <ID> --proof-mode inline --required command` until entry 8 is fixed. A WORKFLOW.md default for `proof_required` would remove that step.

## Status in Tusker (2026-10-25, tusker commit 8ada4e24, installed)
- Fixed in dd773810: 8, 9, 10, 11, 12, 15, 17, 19, 20. Each check has its own time limit; nothing is discarded when one runs long.
- 14, fixed: `test` and `[` no longer count as test runners. broad_test now needs two different passing test commands (for example the focused filter plus the package's wider run), so one command no longer satisfies both.
- 18, fixed without a frozen snapshot: a check that fails while HEAD moves or files change stays pending instead of being recorded as a fail, and the refusal names the changed paths ("rerun when the tree is quiet").
- 7: a hard dependency in review still blocks `tusker start`, but the refusal now says so and prints the exact `tusker edit <ID> --dependencies …` that turns it soft while keeping every other dependency.
- 16, churn: the vault .gitignore now ignores generated files (event and attempt logs, `_generated/`, `scratch/`, `workspaces/`, `_runtime/`, locks, evidence artifacts). `tusker check` reports a missing block and generated files still tracked; `tusker check --fix` writes the block. Still open: spreading checks across build hosts (`verification.env` passes `RZN_BUILD_*`/`RZN_ROUTE_*` so the cargo-any wrapper routes).
- 21: new standalone tasks default to `proof_required: [focused_test]`, same as wave tasks, and the authoring warning suggests `go test …` or WORKFLOW.md `verification.test_commands` instead of `test …`.
- 13: unchanged. Authoring warns about acceptance rows no check covers; the Proof column is still not read.
- This repo, commit 7eb0c84: WORKFLOW.md `verification:` (timeout 30m, env `RZN_BUILD_*`, `RZN_ROUTE_*`, `RZN_CARGO_*`, cargo-any test pattern) and the AGENTS.md command names. Once, run `tusker check --fix` here, then the untrack command `tusker check` prints for generated files, and commit both.

## PIX wave issues (W-0034, 2026-10-25), status in 8ada4e24
1. spec_refs still resolve only under docs/system (kept on purpose: it is the one managed docs root). A ref to an existing file elsewhere, such as docs/plans/x.md, is now refused with "move it to docs/system/proposals/x.md and give it front matter `kind: proposal`".
2. Fixed: a wave request takes a top-level `epic: PIX`; members without their own epic join it and get PIX-T- IDs. Members can also set `proof_required`.
3. Fixed: `tusker start <ID> --current-workspace` starts a backlog task.
4. Fixed: several tasks can be claimed in one checkout when each declares owned paths and none overlap. Otherwise the refusal (WORK_SESSION_CHECKOUT_BUSY) names the task holding the checkout.
5. Fixed: owned paths are required only when a task is created ready or rework, or moved to ready.
6. Fixed: see 21; standalone and wave tasks both default to focused_test.
7. Fixed: recording a member of an unarmed wave says the wave is not armed and gives the next step: `tusker review <ID> --by <reviewer>` from another session, or `tusker done <ID> --by human:<name>`.
