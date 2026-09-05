# FLW-T-0006 · Independent baseline-test evidence (help/skill lane)

Lane: `cmd/tusker/fresh_clone_baseline_test.go`,
`cmd/tusker/skill_lifecycle_test.go`,
`cmd/tusker/skill_progressive_disclosure_test.go`,
`e2e/contractconvergence/contract_convergence_test.go`, this file.
State revision, CAS, reconciliation, snapshot source/tests belong to the Terra
worker; `cmd/tusker/trust_baseline_test.go` belongs to the parent. None touched.

Host: `Saravanans-MacBook-Pro.local`. Revision at write time: `03201019`
plus shared dirty baseline (preserved; see `/tmp/tusker-orchestration-baseline.txt`).
Runner note: the repo `scripts/with-validation-lock.sh` default lock dir lives
under `.git/` (sandbox read-only, `mkdir` denied, waits forever), and the
default `GOCACHE` is likewise unwritable here, so focused runs used
`TUSKER_VALIDATION_LOCK_DIR=/tmp/tusker-validation-flw-t-0006.lock` and
`GOCACHE=/tmp/gocache-flw-t-0006` with `GOMAXPROCS=2 go test -p=1 -parallel=1`.
Full suite was not run (parent scope); no commits, installs, or tracker writes.

## PASS/FAIL matrix (executed tests, not zero-match selectors)

| Command | Result |
|---|---|
| `go test ./cmd/tusker -run 'TestFreshCloneBaselineCLIRunsHelpAndV7Init\|TestSkillReservesHumanApprovalForHumanOnlyBoundaries\|TestTuskerSkillProgressiveDisclosure' -count=1 -p=1 -parallel=1 -v` (pre-repair repro, `.tusker/scratch/FLW-T-0006/baseline-repro.log`) | FAIL x3, each `=== RUN` present: `fresh_clone_baseline_test.go:41` help wording; `skill_lifecycle_test.go:100` human-approval string; `skill_progressive_disclosure_test.go:223` knowledge 667 recorded vs 753 measured |
| Same selector for the two repaired tests (`.tusker/scratch/FLW-T-0006/baseline-repair-verify.log`) | PASS: `TestFreshCloneBaselineCLIRunsHelpAndV7Init (36.61s)`, `TestSkillReservesHumanApprovalForHumanOnlyBoundaries (0.00s)`, `ok 37.238s` |
| `go test ./cmd/tusker -run 'TestSkillContractCompatibility' -count=1 -p=1 -parallel=1 -v` (`.tusker/scratch/FLW-T-0006/baseline-compat-verify.log`) | PASS (0.07s) |
| `go vet ./cmd/tusker` after repairs | PASS |
| `gofmt -l` on the two edited test files | clean |
| `TestTuskerSkillProgressiveDisclosure` post-repair rerun | not run: fails on fixture data outside this lane (see below); rerun belongs after the skill-owner repair |
| `TestContractConvergence` (e2e wrapper) | not run: transitively blocked on the same fixture; no file change needed (it shells to the focused tests and already lists them) |

## Obsolete assertions repaired in-lane (same invariant, re-pinned)

- `fresh_clone_baseline_test.go`: help header is authoritatively
  `Tusker - repo-local work tracking` (`cmd/tusker/cli.go:778`); the asserted
  `V7 repo-local work tracking` is stale. Assertion re-pinned; V7-init half
  of the test passes unmodified (36.61s executed).
- `skill_lifecycle_test.go` (`TestSkillReservesHumanApprovalForHumanOnlyBoundaries`):
  commit `03201019` deliberately reworded the Gates paragraph, same rule.
  `Anything the task, spec, or a linked decision already settles is settled`
  / `subjective acceptance (UX feel, brand, legal)` / `risk alone is not a gate`
  became `A gate records one missing human fact` /
  `subjective acceptance (UX, brand, legal)` /
  `Settled facts and risk alone are not gates`. Assertions re-pinned; no
  invariant weakened (settled-facts-are-not-gates, risk-alone-is-not-a-gate,
  human-only-boundary all still asserted against live skill text).

## Genuine defects / owner decisions reported, not edited (out of lane)

1. `skills/tusker/testdata/progressive-disclosure-budget.json` is stale for
   6 of 7 cases (measured router+guide words: track 649=recorded;
   knowledge 753 vs 667; specs 533 vs 445; run 720 vs 634; operate 501 vs 409;
   xcode 547 vs 461; onboarding 641 vs 1672). Five cases exceed their
   `max_loaded_words` (675/475/650/425/475). Re-pinning `loaded_words` is
   mechanical, but raising the maxima weakens the progressive-disclosure
   bound — that is a skill-owner + parent decision (trim guides vs raise
   budgets). Owning files: the budget fixture plus
   `skills/tusker/references/{KNOWLEDGE,SPECS,RUN,OPERATE,XCODE_BUILD_STATE}.md`.
2. `skills/tusker/references/REPO_ONBOARDING.md` rewrite (`a5dfc522`,
   248→80 lines) dropped the `Storage Boundary` section and the
   `Existing Repository Onboarding` title casing the budget's `required`
   strings pin; no replacement found (`rg -i 'never execution authority'`
   leaves only `Registration does not enable automation`). Either restore the
   storage-boundary guidance or deliberately update the budget's `required`
   list. Owning files: `REPO_ONBOARDING.md`, budget fixture. Verified still
   green in that test: frontmatter/900-word/140-line caps, 7-route table
   deep-equal, no cross-reference recursion, no duplicated normative
   paragraphs, all other cases' `required` strings, install symlinks.
3. Transient blocker observed, not mine: sibling in-flight edit of
   `cmd/tusker/v7_proof_cmd.go` left the package unbuildable twice
   (`undefined: vaultPath` ~12:38, `expected '}', found 'default'` ~12:39);
   resolved by ~12:40. `go vet` is clean at write time. No action; recorded
   so a future red build is not misattributed to this lane.

## Remaining acceptance not yet proven

- `TestTuskerSkillProgressiveDisclosure` green after the skill-owner
  fixture/content repair; then `TestContractConvergence` green in its hermetic
  sandbox (8-minute gate, includes Terra-owned tests — coordinate).
- Complete Go suite through `scripts/with-validation-lock.sh` (parent scope).
- Tracker closeout by the verification executor only.
