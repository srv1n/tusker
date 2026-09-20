# Recovery qualification — 2026-09-16

## Result

Implementation and deterministic qualification: **PASS**.

Installed CLI, TuskerBar, and daemon health: **PASS** on 2026-09-17 after explicit operator authorization to install and test.

Retained-campaign recovery, provider execution, and human acceptance: **NOT RUN**. Installation refreshed the bundled daemon, but no provider was dispatched, no retained attempt was retried, and `/private/tmp/tusker-walkthrough-20260916` was not mutated by qualification.

## Retained diagnosis

- `STN-T-0001` retains its accepted command receipt and independent review. The defect was projection: an unavailable current-material read was collapsed into stale/work-changed. Recovery now reports `missing`, `failed`, `unavailable`, or `changed`, including the changed dimension and previous/current identities when known. Unavailable evidence explicitly does not prove material drift.
- `ALP-T-0001` attempt `01M2MCYRA167QSNJR83GX4JHGJ` is a failed review attempt, not an awaiting reviewer. The retained failure shows a malformed proposal `task_state_rev`: submitted `sha256:1087ac407177ac115...`, retained task `sha256:1087ac407177115...`; project, task, attempt, work revision, and source otherwise match.

## Implemented recovery

- `Retry review` queues the existing one-shot directive on the review lane, preserves the submitted implementation, keeps the attempt count, refuses active owners, paused/inert windows, and exhausted budgets, and is idempotent on repeat admission.
- `Rerun checks` executes only invalid current command rows through the existing verification executor and records a new receipt while retaining the prior receipt text.
- Failed review and proof-invalid causes/actions render in the task drawer; unavailable material disables rerun rather than claiming a change.
- Fresh wave navigation opens Dependencies for every lifecycle state. Explicit Tasks/Results selection remains authoritative.

## Deterministic evidence

| Boundary | Check | Result |
| --- | --- | --- |
| Local Go | `go test ./cmd/tusker -run TestRecovery -count=2` | PASS; two consecutive runs |
| Local rendered UI | `bun test --cwd internal/serve/ui test/recovery-actions.test.ts test/recovery-dag-default.test.ts --rerun-each 2` | PASS; 6/6 test executions, 40 assertions |
| TypeScript | `bun run --cwd internal/serve/ui typecheck` | PASS |
| Full UI | `make ui-test` through the release gate | PASS; 290 tests, 1,880 assertions, including real Chrome at 390 px and 1,440 px |
| Go recovery and authority | `go test ./cmd/tusker -run 'TestDirectWaveAuthorityOwnershipSemantics|TestDirectWaveAutonomousStartReauthorizesStalePausedWave|TestRecovery' -count=1` | PASS; 4 tests |
| Static analysis | `make vet` | PASS |
| Release integrity | `make release-test` | PASS; GNU tar reproducibility check skipped because GNU tar is unavailable |
| Production build | `make build-go` | PASS |
| Installed CLI | `make install`; compare `dist/tusker` and `~/.local/bin/tusker` SHA-256 | PASS; both `23b141d2dc689f0265fddf7d058c9717d58bab74b1435be8b17bcfe01e96b7d5` |
| Installed TuskerBar | signed bundle verification and bundled `tusker version --json` | PASS; signature valid, revision `94dfd615d0c3bdd8dc513e178c407cd4ebc3e76f`, modified build |
| Installed daemon | `tusker daemon status --json` | PASS; alive, Serve bound to `127.0.0.1:7420`, invariant and crash-loop circuits closed |
| Diff hygiene | `git diff --check` | PASS |

The repository-wide Go suite is **FAIL**, not promoted to pass: the 20-minute run retained unrelated skill-package and crash-recovery fixture failures, plus a timeout after those failures. Repository validation is also **FAIL** with 208 pre-existing tracker/spec errors. The owned recovery and authority regressions above pass; the checkout is installed and usable but the whole dirty repository is not release-green.
