# Tusker pre-push hardening — patch request (GPT-5.5 Pro Extended)

## Role & lane

You previously reviewed this repository (handoff `cgpt_mrc05ks3_5fcda4b9`), verdict
`request_changes` / risk `high`, 7 findings. **Four** of your findings are already
fixed and landed on `main` (terminal-retirement guard on status-ready reconcile,
heartbeat tolerance before first event, serve stdout-capture + panic recovery, serve
CSRF/host guard). This request is a **dev patch** for the **three findings that were
filed but not yet patched**. The attached codebase zip is **current `main`** — it
already contains the four landed fixes, so patch against what you see in the zip.

**Deliverable: exactly ONE complete unified diff**, applicable from the repo root with
`git apply --3way`, that implements all three fixes **with Go tests proving every
acceptance item below**. Do not truncate. Do not emit multiple diffs. Put any
assumptions, build notes, or caveats in prose *outside* the diff — the diff itself must
be clean and self-contained.

The graders' Go toolchain is 1.25.8 (see `go.mod`); yours may differ, so you are not
expected to compile. We run the full gate (`go build ./... && go vet ./... && go test
./... -count=1`, plus the serve UI build/test) on our side. Your job is a patch that
**applies cleanly** and whose **tests encode the acceptance criteria precisely** — the
tests are how we trade agent time for human test time, so make them real
(construct actual git repos/worktrees in `t.TempDir()`, exercise the real code paths,
assert on real outcomes — no stubs that assert nothing).

Match the surrounding code style. Keep helpers local to the package. Do not refactor
unrelated code or reflow untouched lines. Existing land tests live in
`cmd/tusker/*_test.go` (search for `Land`, `Dispatch`, `Claim`, `Lease` harness
patterns and reuse them).

---

## Finding 1 (HIGH) — `land`: bind branchification source to the task — OPS-T-0007

`tusker land` branchification does not bind its source commit to the task being landed:

- `cmd/tusker/v7_land_cmd.go:269` — `land --from <ref>` accepts any resolvable
  ref/worktree commit and immediately creates the task branch from it, with **no check
  that the ref actually belongs to this task**.
- `cmd/tusker/v7_land_cmd.go:320` — the auto-discovery fallback matches any detached
  worktree whose absolute path merely *contains* the task id
  (`strings.Contains(strings.ToUpper(wt.Path), upperID)`), so `APP-T-0001` can match
  `APP-T-0001x`, a scratch path, or an unrelated worktree.

Provenance signal already present: `v7WorkspaceRecordID(worktreePath)` reads
`.tusker/workspace.json`'s `record_id` (see the reader near
`cmd/tusker/v7_land_cmd.go:359`).

**Required fix.** Validate the branchification source against task-owned provenance
*before* creating the branch:

- Validate explicit `--from` and auto-discovered worktrees against the workspace
  `record_id` with an **exact match** to the task id (not a substring, not a
  path-contains). A worktree whose `record_id` is not exactly this task id is not a
  valid source.
- For a raw commit ref passed to `--from` (no workspace.json), require task-local
  tracker provenance or an explicit, logged trusted override; otherwise refuse.
- Replace the broad `strings.Contains` path match with the exact `record_id` match
  only.
- A source that cannot be proven to belong to the task must be **refused with an
  actionable error**, never landed.

**Acceptance (each needs a focused test):**
- **A1** — `land --from <ref>` is refused with an actionable error when the
  ref/worktree does not belong to the task (verified against workspace `record_id` /
  task-local tracker metadata); a matching source still lands.
- **A2** — Auto-discovery no longer accepts a substring path match: only an exact
  `record_id` match selects a worktree; `APP-T-0001` never matches `APP-T-0001x` or an
  unrelated path.
- **A3** (broad_test) — no behavior regression for the correct-source land path.

**Non-goals:** don't change the serialized integration-branch / merge mechanics (that's
Finding 2); this is strictly source selection/provenance.

---

## Finding 2 (HIGH) — `land`: guard main update-ref against a checked-out default branch — OPS-T-0008

`landV7WaveToMain` (`cmd/tusker/v7_land_cmd.go:634`) advances the default branch with
`git update-ref refs/heads/<defaultBranch>` **even when that branch is checked out in a
worktree** (the operator's main working tree or another linked worktree).

Consequence: the branch ref moves forward but the checked-out worktree's index/working
tree stays at the old tree. The operator is left on a stale, dirty-looking `main` after
a *successful* land — a state where an accidental commit or `git checkout -- .` / reset
can silently revert the just-landed changes. Data-loss-adjacent footgun on the primary
merge path.

**Required fix.** Before `update-ref`, detect whether the default branch is checked out
in any worktree (`git worktree list --porcelain`, or per-worktree
`git rev-parse --symbolic-full-name HEAD`). Then either:
- (a) when the checked-out worktree is **clean**, advance the branch *through* that
  worktree so its HEAD/index stay synced to the new ref (e.g. a fast-forward/reset that
  is safe on a clean tree), or
- (b) when the checked-out worktree is **dirty**, refuse the land with an actionable
  error **before** moving the ref (no partial/wedged state).

Never leave a checked-out worktree desynced from its advanced branch ref.

**Acceptance (each needs a focused test):**
- **A1** — default branch checked out and clean: a successful wave land leaves that
  worktree's HEAD/index synced to the advanced ref (no stale/dirty main).
- **A2** — default branch checked out and dirty: the land refuses with an actionable
  error **before** moving the ref (no partial/wedged state).
- **A3** — default branch not checked out anywhere: current `update-ref` fast path
  behavior is unchanged.
- **A4** (broad_test) — no regression to the serialized wave/land lane or the landing
  summary.

**Non-goals:** don't change source-selection/provenance (Finding 1); don't move away
from the low-level `commit-tree`/`update-ref` integration approach — only make the final
main advance safe w.r.t. checked-out worktrees.

---

## Finding 3 (MEDIUM) — daemon dispatch: claim via atomic `ClaimRunLease` CAS, not blind `UpsertRun` — RUN-T-0043

`dispatchRun` (`cmd/tusker/daemon.go`, the `UpsertRun(run)` claim near
`cmd/tusker/daemon.go:2204`) claims work by mutating an in-memory `RunStatus` and blindly
`UpsertRun`-ing it, even though `RuntimeStore` already exposes an atomic
`ClaimRunLease` (`cmd/tusker/runtime_store.go:976`). Workspace prep, prompt rendering,
`SaveAttempt`, and the final upsert are **not** guarded by a compare-and-swap against
the row read at poll start.

Consequence: a concurrent control action (stop/cancel/limit change from CLI or serve UI)
or a second `daemon`/`once` process can be silently overwritten by a stale dispatch
claim — a lost update / lease race that can double-dispatch or clobber a just-changed
runtime transition. Low probability single-daemon, but it undermines the invariant that
the active-run cap and lease state are authoritative.

**Required fix.** Move dispatch claiming to `ClaimRunLease` with
`owner`/`generation`/`work_revision` preconditions **before** any external side effect
(workspace prep, spawn). Make every subsequent write in the dispatch path conditional on
the same `lease_owner` + `lease_generation`. On a failed CAS, abort the dispatch cleanly
(another actor won the row) with **no side effects**.

**Acceptance (each needs a focused test):**
- **A1** — dispatch claims via the atomic CAS (`ClaimRunLease` with
  owner/generation/work_revision preconditions) before workspace prep or spawn; a lost
  CAS aborts dispatch with no side effects.
- **A2** — a concurrent control mutation (e.g. cancel/limit) that changes the row
  between poll and claim is not overwritten by the stale dispatch; the dispatch backs
  off.
- **A3** (broad_test) — single-daemon happy path unchanged (a run still dispatches
  normally when uncontended).

**Non-goals:** no change to active-run capacity accounting / invariant-circuit logic
beyond making the claim atomic; no multi-daemon coordination feature — only close the
existing single-store CAS gap.

---

## Output format

1. One prose paragraph of assumptions/notes (optional).
2. Then exactly one fenced unified diff, from repo root, `git apply --3way`-clean,
   implementing Findings 1–3 with Go tests for every A-item listed above.
