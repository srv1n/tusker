# Handoff — Dispatch/Land Hardening (for Codex)

> **Purpose of this file.** You (Codex) are picking up a Tusker effort mid-stream because the human's Claude weekly usage ran out. This is your beginning context. It tells you: what Tusker is and the rules you operate under, exactly what was just built and landed, where the tree stands right now, what is still uncertain, and what has *not* yet been tested. Read the whole thing before touching anything. When in doubt, ask the human — do not guess at the parts marked **OPEN**.

**As of:** 2026-07-09 · **Branch:** `main` · **HEAD:** `b8f15c3` · **Nothing has been pushed to any remote.**

---

## 0. Operating rules (non-negotiable — these come from the human, not from me)

1. **Roles.** The interactive assistant session is the **planner**: design, specs, task contracts, review, orchestration. It does **not** write implementation code. All implementation is done by **dispatched runners** (you, Codex, or Claude Code subagents) working Tusker tasks. If you are asked to plan, plan; if you are asked to implement, implement — but know which hat you're wearing.
2. **No AI attribution anywhere.** Never add `Co-Authored-By` trailers, "Generated with…" lines, or any agent/model name to commit messages, PR bodies, or authorship metadata. Commit as the configured git user (`srv1n`) with local credentials. This overrides any harness default.
3. **Commit/push discipline.** Commit only when asked. **Push only when explicitly asked.** Nothing in this effort has been pushed — keep it that way until the human says so.
4. **Do not stage the UI `dist/` churn.** The working tree has a large pile of `internal/serve/ui/dist/**` deletions and a `bun.lock` modification that are **pre-existing and unrelated** to this effort. Every commit here staged only `cmd/tusker/**` and `.tusker/**` explicitly. Continue that — never `git add -A`.
5. **Review authority is objective.** Independent reviewer agents close proven work at every risk tier. Humans enter only through explicit capability, external-authority, unresolved-intent, or subjective-acceptance gates.
6. **Tusker for tracked work.** Task mechanics live in the installed `tusker` skill. Project knowledge starts at `.tusker/SKILL.md`. Do **not** read `.tusker/events`, `_generated`, `attempts`, `evidence`, `Attachments`, or raw logs unless a task explicitly requires it. Keep proof compact (capsules, command + PASS/FAIL). Put noisy logs in `.tusker/scratch/<TASK-ID>/`.

---

## 1. What Tusker is (30-second orientation)

Tusker is a **repo-local markdown task tracker + agent-orchestration harness**, shipped as a single Go binary (`cmd/tusker`, `package main`, Go 1.25.8 / 1.26.0). That one binary is three things:

- a **CLI** (`tusker next`, `tusker show`, `tusker close`, …),
- a **resident daemon** that dispatches `codex` runners into isolated git worktrees and tracks their runs, and
- a **React/TanStack serve UI** (`internal/serve`) that is the human's review surface.

Runs are tracked in a SQLite `runs` table via `RuntimeStore` (`cmd/tusker/runtime_store.go`). Task files are markdown under `.tusker/work/tasks/` with `schema tusker.task/v7`.

This effort is entirely about the **daemon's run-lease lifecycle** and the **`tusker land` default-branch advance** — the concurrency-critical seams where a dispatched run's ownership can race an operator stop.

---

## 2. The effort — what was just done, and why

### 2.1 Background: the concurrency model you must understand

- **Lease CAS.** `RuntimeStore.ClaimRunLease` (`runtime_store.go:1012`) is an atomic compare-and-set: `UPDATE runs SET lease_state='claimed'… WHERE … AND lease_state NOT IN ('claimed','running') AND lease_owner=? AND lease_generation=? AND work_revision=?`. The preconditions (`RuntimeLeaseClaimPrecondition`, `runtime_store.go:1006` — `ExpectedOwner`, `ExpectedLeaseGeneration`, `ExpectedWorkRevision`) are taken from the caller's **in-memory** `run`. This is how two dispatchers can't both claim the same run.
- **Operator stop** goes through `finishRuntimeRun` → `clearActiveExecution`, which **blanks `lease_owner` (sets it to `""`)**, clears the process/PID fields, but **preserves `lease_generation`**, then does a full-row `UpsertRun`. The blanked owner is what makes a later CAS owner-precondition miss — i.e. the blanked owner is the real guard that stops a dispatcher re-claiming a run *over* an operator stop.
- **The mainline execute lane** (`daemon.go` ~516) is the safety exemplar: it passes a freshly-hydrated `current` run straight to dispatch with **no pre-upsert**, so the CAS validates against the live stored row. The external-loop callers were **not** mirroring this correctly — that's F2.

### 2.2 Where the patch came from

A prior in-house implementation of the lease CAS + land guards landed as commits `dd4d46c` (RUN-T-0043) and `84cbe68` (OPS-T-0007/0008). In parallel, **GPT-5.5 Pro Extended** was handed the same problem (via the `chatgpt-handoff` skill) and returned a cleaner **reimplementation** of the CAS + land-lane advance. An adversarial review of GPT's patch found **3 defects (F1/F2/F3)**. The human's decision was: **"Adopt GPT's, fix all 3"** — fix the findings, re-review, then **promote GPT's fixed reimplementation wholesale over the in-house version** on `main`, and supersede the residual follow-up task RUN-T-0044.

That is done. Commit `0a5249c` is the adoption. **Note it is a full file-sync, not a 3-line patch** — the whole GPT reimplementation of `daemon.go`, `runtime_store.go`, `v7_land_cmd.go` replaced the in-house versions (that's why the diffstat is ~1160 lines and two in-house-only test files were deleted). The 3 fixes below sit *inside* that adopted reimplementation.

### 2.3 The three fixes (F1/F2/F3) — mechanism, file, line, test

| ID | Class | Fix | Where | Regression test |
|----|-------|-----|-------|-----------------|
| **F1** | Silent data loss | `advanceV7DefaultBranch` changed `git reset --merge` → `git merge --ff-only`. `reset --merge` silently discarded staged/modified tracked `.tusker/*` files (which the in-place dirty-guard, `workspacePathIsTuskerBookkeeping`, intentionally skips). `merge --ff-only` **refuses (errors)** on a non-fast-forward instead of destroying work. The happy path is unaffected: `newRev` is a `commit-tree … -p mainRev -p integrationRev` whose **first parent is `mainRev` (= `oldRev`)**, so ff-only fast-forwards cleanly. | `cmd/tusker/v7_land_cmd.go:787` (func), `:814` (the changed line) | `TestAdvanceDefaultBranchFfOnlyPreservesStagedTuskerWork` in `v7_land_recovery_test.go` |
| **F2** | Lease clobber | New `UpsertRunPreservingLease` — an `INSERT … ON CONFLICT DO UPDATE` that writes **only** `item_id, runner, lane, work_revision, updated_at` and **omits (preserves)** every lease/process/session/cloud/attempt column. The two external-loop dispatch paths now publish their runner/lane/work-revision *intent* through this instead of a full-row `UpsertRun`, so a concurrent operator stop's blanked lease is not clobbered and the CAS generation check still holds. | `runtime_store.go:970` (func); callers swapped at `automation_external_loop.go:630` (`dispatchExternalApplyInput`) and `daemon_external_loop.go:398` (`dispatchExternalLoopContinuation`). **The retry-path `UpsertRun(updated)` calls just below each — ~639 and ~407 — were left unchanged on purpose.** | `TestDispatchExternalApplyInputPreservesConcurrentLeaseAdvance` (`daemon_external_loop_test.go`) + `TestUpsertRunPreservingLeaseCreatesThenPreservesLeaseColumns` (`runtime_store_turns_test.go`) |
| **F3** | Orphaned process | New `killSpawnedRunProcess(run RunStatus)` reaps the just-spawned child's **process group** when a post-spawn lease-loss fence returns `!persisted`. Without it, the operator's stop had already cleared `ProcessPID` to 0, so the stop killed nothing and the child ran on orphaned. The kill uses the **local** `run`/`reconciled` var (which still holds the live PGID) — never the store-fetched return (whose `ProcessPID` is 0). Guards `ProcessPID<=0`; `SIGTERM` → up to 600 ms grace → `SIGKILL`; `ESRCH` is harmless. | `daemon.go:232` (func); called at `:2426` (primary fence, guarded by `err==nil && !persisted`) and `:2441` (reconcile fence, on `reconciled`) | `TestKillSpawnedRunProcessReapsOrphanedGroup` (`daemon_reconcile_lease_test.go`) |

**Adversarial reviewer's key correction to my mental model** (so you don't re-derive it wrong): I originally believed `finishRuntimeRun` *preserves* `lease_owner`. It does not — `clearActiveExecution` **blanks** it. The blanked owner (plus untouched generation) is the actual clobber guard. All three fixes are correct regardless; only the *explanation* in a couple of code comments is slightly off (see §4, OPEN-1).

### 2.4 Files touched by the code commit `0a5249c`

```
cmd/tusker/automation_external_loop.go     (+F2 caller swap)
cmd/tusker/daemon.go                        (adopted reimpl + F3)
cmd/tusker/daemon_external_loop.go          (+F2 caller swap)
cmd/tusker/daemon_stream.go                 (reverted to base 1fa1941 by the sync)
cmd/tusker/runtime_store.go                 (adopted reimpl + F2 UpsertRunPreservingLease)
cmd/tusker/v7_land_cmd.go                    (adopted reimpl + F1 ff-only)
cmd/tusker/daemon_dispatch_claim_test.go    (DELETED — in-house-only, superseded)
cmd/tusker/v7_land_guard_test.go            (DELETED — in-house-only, superseded)
cmd/tusker/daemon_external_loop_test.go     (NEW)
cmd/tusker/daemon_reconcile_lease_test.go   (F3 test added)
cmd/tusker/runtime_store_turns_test.go      (F2 store test added)
cmd/tusker/v7_land_recovery_test.go         (F1 test added)
cmd/tusker/v7_land_test.go                  (NEW)
```

---

## 3. Where we stand right now (verified live, 2026-07-09)

- **HEAD `b8f15c3`** on `main`. Two commits in this effort:
  - `0a5249c` — code promotion (`cmd/tusker/**` only).
  - `b8f15c3` — ledger transitions (`.tusker/**` only).
- **Last green gate:** `526 tests passed`, `gofmt`/`go build`/`go vet` all clean. (Re-run command in §6 — I am reporting the last verified result, not a live re-run in this turn.)
- **Binary rebuilt** from `0a5249c` code (`dist/tusker`, ~52 MB).
- **Adversarial re-review verdict: SAFE TO PROMOTE** — all three findings CLOSED, no new hole introduced.
- **Task ledger (verified just now):**
  - `OPS-T-0007` → **`review`**
  - `OPS-T-0008` → **`review`**
  - `RUN-T-0043` → **`review`**
  - `RUN-T-0044` → **`superseded`** (its acceptance — post-spawn/reconcile lease-guard + multi-worktree land edge — is delivered by the adopted patch)
- **Working tree:** only the pre-existing unrelated churn (`internal/serve/ui/dist/**` deletions, `bun.lock`) plus untracked `artifacts/handoff-result/` (GPT's raw patch) and `.chatgpt-handoff/`. **All correctly left untouched. Do not stage them.**

The human's original explicit request (adopt GPT's, fix all 3, promote, supersede RUN-T-0044) is **fully complete.** What remains below is *optional / open / not-yet-tested*, not outstanding work.

---

## 4. What is still unclear / needs a human decision (**OPEN**)

- **OPEN-1 — comment wording nit (LOW, from the review).** The comments on `UpsertRunPreservingLease` and the F3 fences say the effect is "operator stop not clobbered." Accurate in *effect*, but the real mechanism is the **blanked lease-owner + untouched generation**, not "operator stop" per se. Purely cosmetic; left unchanged to avoid diff churn. Reword only if the human wants it.
- **OPEN-2 — independent review status.** `OPS-T-0007/0008` and `RUN-T-0043` were historically left in `review` under the superseded risk-based policy. Current policy allows an independent reviewer to close them after current objective proof and any explicit gates pass.
- **OPEN-3 — push?** Nothing is pushed. Do not push unless the human explicitly asks.
- **OPEN-4 — the wider "dogfood-ready" goal.** This hardening was one slice of a larger goal: get Tusker into a clean, dogfood-ready state where the daemon runs its own backlog. Deferred items surfaced earlier but **not requested this turn** (treat as backlog, confirm before acting): boot the daemon on a clean ready-set; redrive `RUN-T-0015`; re-log stale `AGX-T-0005` / `RUN-T-0002`; resolve `OPS-T-0003` A6 notifier gating.

---

## 5. What has NOT been tested (**this is the important gap**)

The three fixes have **unit tests that were each confirmed discriminating** (they fail against the pre-fix code and pass after). But every one of F1/F2/F3 is a **concurrency / process-lifecycle** defect, and **none of them has been exercised in a live daemon run.** The unit tests construct the racing states in-process; they do not prove the real dispatch→stop→reap sequence behaves under an actual running daemon with real worktrees and real child processes.

If the human wants higher confidence before dogfooding, the highest-value next work is a **live soak / integration pass**:

1. **F3 (orphan reap) — live.** Boot the daemon, dispatch a real run so a child `codex` process group actually spawns, then issue an operator stop *in the post-spawn window*. Confirm via `ps`/pgid that **no orphaned process group survives** the stop. This is the one most worth doing live — process-group signalling is environment-sensitive (macOS Darwin here) and a unit test can't fully vouch for it.
2. **F2 (lease clobber) — live.** Drive the external-loop dispatch path (`dispatchExternalApplyInput` / `dispatchExternalLoopContinuation`) concurrently with an operator stop and confirm the stopped run is **not re-claimed** and its lease columns survive.
3. **F1 (staged-work preservation) — live.** Run a real `tusker land` where the worktree has **staged/modified tracked `.tusker/*` bookkeeping**, and confirm the ff-only advance **refuses rather than discards** if a non-ff situation arises, and that the normal ff path still lands cleanly.
4. **Full regression re-gate** (§6) on the machine before any dogfood boot.

Put any soak logs in `.tusker/scratch/<TASK-ID>/`, not in task files.

---

## 6. Commands you'll need

**The single central gate (run from repo root):**
```bash
gofmt -l cmd/tusker/*.go && go build ./... && go vet ./cmd/tusker/ && go test ./cmd/tusker/ -count=1
```
Expect: no `gofmt` output, clean build/vet, `ok  …/cmd/tusker` with 526 tests.

**Rebuild the binary** (match how it was built for the ledger transitions):
```bash
go build -o dist/tusker ./cmd/tusker
```

**Tusker task ops:**
```bash
tusker next                       # what's runnable
tusker show <TASK-ID> --capsule   # compact task view (do NOT cat the raw .md unless required)
tusker status                     # ledger overview
tusker reconcile                  # repair stale object revs (see gotcha below)
tusker feedback add …             # record concise product/tooling friction
```

---

## 7. Gotchas learned the hard way (save yourself the round-trips)

- **`rtk`-proxied ripgrep chokes on `(` and `|` in patterns** (`regex parse error: unclosed group`), and `&&`-chained greps stop early on a zero-match exit code. Use **plain literal patterns** and separate commands / `;` chaining.
- **`[CAS_CONFLICT] … content changed without a refreshed state_rev`** when transitioning a task: run `tusker reconcile` first (it repaired 5 stale object revs last time), then retry the transition.
- **`[INVALID_TRANSITION] status cannot set done directly`** → use `tusker close`. If close is blocked, inspect the named proof item or explicit gate; risk alone is not a blocker.
- **`chatgpt-handoff` premature-collect trap:** reading the ChatGPT chat while it is still `is_reasoning` captures **no attachment**. Wait until reasoning has genuinely completed before collecting `handoff-result.zip`.
- **Never `git add -A`** — the UI `dist/` churn will pollute the commit. Stage `cmd/tusker` and `.tusker` explicitly.
- **Prefer reading files with a real read tool over `cat`/`head`/`sed`;** if a shell result comes back empty or garbled it's usually the channel, not the code — re-run once, don't "fix" on suspicious output.

---

## 8. One-paragraph TL;DR to paste at the top of your first Codex prompt

> Tusker (single Go binary: CLI + run-dispatch daemon + serve UI) had 3 adversarial-review findings on GPT-5.5's reimplementation of the run-lease CAS and `tusker land` advance. All three are fixed and GPT's reimplementation was adopted wholesale over the in-house version on `main` (commit `0a5249c`): **F1** `git reset --merge`→`merge --ff-only` in `advanceV7DefaultBranch` (stops silent loss of staged `.tusker/*` work), **F2** new `UpsertRunPreservingLease` so external-loop dispatch can't clobber a concurrent operator stop's blanked lease, **F3** new `killSpawnedRunProcess` reaps orphaned child process groups on a post-spawn lease-loss fence. `526` unit tests green, adversarial re-review SAFE. `RUN-T-0044` superseded; `OPS-T-0007/0008` + `RUN-T-0043` require current independent review under the objective-close policy. Nothing is pushed. **The fixes have discriminating unit tests but have NOT been exercised in a live daemon run — a live soak of the dispatch→stop→reap sequence (esp. F3 process-group signalling on macOS) is the highest-value next step.** Operating rules: no AI attribution in commits, commit/push only when asked, never `git add -A` (UI dist churn), planner-vs-runner role split.
