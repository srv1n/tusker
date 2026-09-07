# Real-work test repository — fixture report

Canonical integrated execution order, proof classes, timeout behavior and cleanup: `[[completion-and-integrated-acceptance#canonical-end-to-end-execution-plan]]`.

- Fixture: `parallel-waves` scenario **v2** (standalone smoke task + three waves, 13 tasks).
- Candidate: base commit `f9d2b2b1` ("Implement installed runner boundary and work experience") plus uncommitted work. The checkout was already dirty before this packet started; this packet's files are: `cmd/tusker/demo_fixture.go`, `demo_state.go`, `demo_cmd.go`, `demo_run.go`, `demo_check.go`, `demo_realwork.go` (new), `demo_realwork_test.go` (new), `demo_cmd_test.go`, `demo_check.go` reset/check updates, `cmd/tusker/capabilities_cmd.go` (one line), `docs/system/cli.md` (demo section), `scripts/test-real-work-project.sh` (new), and this report.
- Date: 2026-09-07. Everything below was observed in this session unless marked as a prerequisite for the operator.

## What was built

One CLI reset/seed journey creates the disposable test repository: a runnable `sample/` project (README, `sample/tools/wait_progress.py` progress helper), a fictional Fixture Cafe knowledge corpus (7 notes under `.tusker/specs/fixture-cafe/` covering overview, two folder indexes, product intent, long technical brewing reference, decision record with alternatives/rationale, and one superseded note linked to its successor), one standalone smoke task (`s1`) and three four-task waves (alpha, beta, follow-up). Every task contract carries bounded context (files to inspect, owned paths, non-goals, exact verification, review requirements) and links the shared fictional spec instead of copying it. Work levels: roots `routine`/light, branches `standard`, joins `complex`/demanding.

Two lanes, one fixture. The offline lane (`--mode offline`, default) drives the deterministic demo-timer executor. The real lane (`--mode real`) resolves each task through `tusker runner route`, requires the named harness/profile with **no silent substitution**, moves tasks ready through the ordinary CLI, authorizes waves, then waits (bounded `--timeout`) for the configured runtime to perform claim, progress, submit, review, and close. Attempt IDs, profiles, harness, model, and transport-when-known are recorded from runtime inspection. The driver never writes artifacts, submits, or closes in the real lane.

## Observed results (offline journey, final binary)

Command (sandbox note: `GOCACHE=/tmp/gocache` was required because the default Go build cache is not writable here):

```sh
./scripts/test-real-work-project.sh --repo /tmp/rw-final --candidate /tmp/tusker-test --proof-dir /tmp/rw-proof-final
```

Exit `0` in ~53s (fast mode, two full passes). Proof files listed below lived outside the demo repo, so reset never deletes them.

- Seed: 13 tasks, waves `alpha beta follow-up standalone`; project `01M1XJRP5RCJ9VAD77Y4DY6F4B`; `docs check` valid (8 documents, 0 issues); roots (`s1 a1 b1`) ready, `c1` backlog.
- Reset preview: dry-run, mapping unchanged. Reset apply: 69 owned paths removed, equivalent scenario reseeded; contract identities stable across reset by design (delivery import reissues the same IDs for the same plans); attempts refresh per run (verified: no attempt ID reused across runs).
- Standalone: `R-01m1xjshxfj7qem0h4dh1r28d1`, s1 done, no lease, native review/close.
- Parallel: `R-01m1xjrwn35pxez85sz8vvph4k` (pass 1) — overlap `alpha 09:21:45–09:21:53` vs `beta 09:21:45–09:21:51`; 4 join edges ordered; every done interval bound to a runtime attempt. Pass 2 (`R-01m1xjskjkfhk1x98ra1ce7p4q`) repeated with fresh attempts.
- Follow-up: `c1–c4` never auto-started (asserted pre-start), then `R-01m1xjsx79hyt6tx94ny2msfak` completed; final `demo check` 0 failures; `demo wait` terminal; no active runs.
- Variants (separate runs, all exit 0): `fail-once` parked `b3`'s join and retry completed; `reject-once` recorded a reviewer rejection of `c2` then correction and completion; `cancel` (SIGINT mid-run) left no leases, 2/8 wave tasks done, no late success after 3s, reset clean.

## Tests

```sh
GOCACHE=/tmp/gocache go test ./cmd/tusker -run '^(TestDemoFixtureShape|TestDemoOwnedPath|TestDemoExitMapping|TestDemoRepeatableE2E|TestDemoFailureRetryE2E|TestDemoGuardsE2E)$' -count=1 -v
# ok — 6/6 pass (RepeatableE2E now drives standalone + all waves)

GOCACHE=/tmp/gocache go test ./cmd/tusker -run '^TestRealWorkFixture' -count=1 -v
# ok — 7/7 pass: Shape, StaticFiles, ProgressHelper, SeedE2E, RealRefusal, ActiveResetRefused, ResetE2E
```

`TestRealWorkFixtureRealRefusal` proves the real lane refuses unknown harnesses, profile mismatches, missing `--require-harness`/`--profile`, and offline-only flags with precondition/invalid exits, and records no timer fallback run. `gofmt` clean, `go vet` clean.

## Real execution: NOT passed (prerequisite recorded, no fake pass)

What works today: `runner catalog` reports `codex_exec` available (codex-cli 0.153.4); `runner route STN-T-0001 --lane execute` resolves `execute-standard / codex_exec / gpt-5.6-terra` from installed configuration; `--mode real --require-harness codex_exec` authorizes, records profiles, and waits. With no resident runtime driving the tasks it exits `5` after `--timeout` with the run ID, effective profiles, agent instructions, and an actionable hint — and the task stays `ready` with no lease. `claude-code` reports `error` (auth not configured), so mixed mode is a reported prerequisite, not a pass.

Exact prerequisite for the operator's Codex Luna run:

1. Configure project profiles so every fixture task routes to the Codex profile (record actual resolution with `tusker runner route <TASK-ID> --lane execute --vault <repo>/.tusker --json`).
2. Start the resident runtime from an independent shell (never from an agent session; never `tusker daemon run` from an interactive session).
3. Run: `scripts/test-real-work-project.sh --mode real --repo <dir> --candidate <tusker-bin> --require-harness codex_exec --profile <name> --timeout 20m`.
4. Mixed mode only after that is green: add `--mode mixed --codex-profile <name> --claude-profile <name>` once Muse authentication is configured (`tusker runner catalog --json` must show it `available`).

Environmental note: in this sandboxed session, writes to the default runtime store (`~/Library/Application Support/tusker`) are denied, so default-scope project registration recorded `runtime_problem` and continued (best-effort by design); tests use `TUSKER_STATE_ROOT` sandboxes where registration succeeds. Operator runs with a writable home register normally and reset removes that registration on apply.

## Known limitations

- Contract identities (task/wave IDs) are stable across reset; only attempt IDs are fresh. The old "fresh identities" reset text overstated this and was corrected.
- `demo check` skips exact-bytes assertions after a real-harness run (agent owns the bytes) and relies on `real-attempts-bound` instead.
- Two concurrent real-lane drivers would race on `manifest.json`; the script runs waves sequentially (mixed attribution is per-wave, overlap is proven by the codex-only run).
- Expiry/retention coverage remains unavailable until the retention engine lands (explicit non-goal).

## Runbook for the human

```sh
BIN=<your tusker binary>   # e.g. ./dist/tusker or $(command -v tusker)
DIR=/tmp/real-work-demo    # one explicitly named disposable directory

mkdir -p "$DIR"
$BIN demo seed --repo "$DIR" --scenario parallel-waves --json     # creates everything; records project ID
$BIN demo status --repo "$DIR" --json                             # s1/a1/b1 ready, c1 backlog
$BIN demo check --repo "$DIR" --json                              # invariants (0 failures expected pre-run: most SKIP)
```

Open the same project in the normal UI (the driver never starts a server itself):

```sh
tusker serve --vault "$DIR/.tusker" --by human:<your-name>
# view URL appears in the serve output
```

Start the standalone task, then the waves (offline timer lane; add `--mode real …` for the real lane):

```sh
$BIN demo run --repo "$DIR" --waves standalone --fast --json
$BIN demo run --repo "$DIR" --waves alpha,beta --fast --json
$BIN demo run --repo "$DIR" --waves follow-up --fast --json
$BIN demo check --repo "$DIR" --json
```

Reset and repeat at any time (preview first; apply removes only manifest-owned state and reseeds):

```sh
$BIN demo reset --repo "$DIR" --json        # preview, changes nothing
$BIN demo reset --repo "$DIR" --yes --json  # apply + reseed
```

Or run the whole acceptance journey at once: `scripts/test-real-work-project.sh --repo "$DIR" --candidate "$BIN"`.
