---
kind: doc
title: Coordinator batches
subject: coordinator-batches
part_of: orchestration
status: current
code_conformance: unverified
describes: [cmd/tusker/coordinator_run.go, cmd/tusker/coordinator_policy.go, cmd/tusker/coordinator_audit.go, cmd/tusker/coordinator_author.go, cmd/tusker/coordinator_continuity.go]
read_when: Running an explicitly authorized coordinator batch without the resident daemon.
skip_when: Using the existing daemon task or wave workflow.
---

# Coordinator batches

A batch is a foreground command which schedules existing tasks, launches the
selected CLI profiles, recovers mechanical failures, and prints one summary
when its work finishes or every remaining task is blocked. The coordinator
starts it through its harness's background-process facility. No daemon, wave
arming, coordinator heartbeat, or orchestration model is involved.

## Coordinator workflow

1. Agree on the work and record the governing decision. Add each already-settled
   question with `tusker new fork --epic APP --id format --question "Which format?"
   --ruling "Use the current format" --by coordinator --doc <reference>`.
2. Create tasks with the normal task body, acceptance IDs and owned paths.
   Batch tasks additionally pin `--runner-profile <global-profile>`,
   `--resources cargo:mac=1,box:lab=1`, and `--budget 2h`. The existing
   `--execute-profile` pin also qualifies. The profile must name both model
   and effort; no implicit model-level or harness default qualifies.
3. Inspect `tusker show APP-T-0001 --packet worker`. It contains the body,
   ownership, governing references, active epic rulings, active flaky entries,
   report contract, and the AGENTS.md pointer. `--routing` includes the older
   routing packet explicitly. Local Markdown links and backticked documentation
   references must name existing files and sections. Task creation/update and
   `tusker check --only validate` also check them.
4. Create an inert batch: `tusker new batch --name fixes --epic APP
   --tasks APP-T-0001,APP-T-0002 --max-parallel 2`.
5. Run `tusker start fixes` through the coordinator harness's background
   facility. Process completion supplies the wakeup. An optional `--notify
   '<command>'` receives the digest on stdin and `TUSKER_BATCH_ID` in its
   environment. Do not configure both mechanisms if you want one notification.
6. On completion, inspect the digest and review the changes in the named
   worktrees/branches. Close each task with `tusker done APP-T-0001 --verdict
   accept|accept-with-followups|reject --note <review>`. Audit exceptions require
   `--override-audit <reason>`. Explicit human gates still block close.
7. A new session starts with `tusker digest --since last`. Add a ruling for a
   parked question, then rerun the batch. Finished tasks stay finished.

Batch creation reserves those tasks for this mode; the daemon refuses them.
Task contracts, resource requirements, explicit profile selection and profile
contents are frozen when executing. Changed contracts/profiles require a new
batch. Rulings remain live and are incorporated on the next attempt.

## Scheduling and recovery

Default concurrency is one; `--max-parallel N` permits independent tasks to
run together. Dependencies inside the batch advance only after a passing
report **and** passing audit (the task remains in review). Dependencies outside
the batch require a done task. A checkpoint never releases a dependency.
Novel questions and explicit human gates park only the affected work.

Each task defaults to a separate Git worktree under the machine runtime root,
with branch `srv1nc/batch-<batch>-<task>`. It starts from the repository's committed
HEAD captured at batch creation; uncommitted source edits are not copied. Upstream batch commits merge
before a dependent task's baseline is captured. `--workspace-path <absolute>`
can select an existing clean task workspace. Workers commit there; closing a
task records acceptance, and does not merge its branch into the original
checkout or push it. Use the normal Git integration workflow after review.
Worktrees and evidence remain available for review and recovery.

Resources are machine-wide named counting locks, shared across batches and
repositories using the same `TUSKER_STATE_ROOT`. Default capacity is one.
Declare larger capacity at creation with `--resource-capacity name=2`; a later
conflicting capacity refuses admission. `docs-only` requests no counting lock.
A task also holds an exclusive task lock. Supervisors and workers inherit lock
file descriptors, so controller death does not release a live worker's locks.
A rerun observes that attempt until its supervisor writes a durable receipt;
it does not relaunch a surviving worker. PID/start-time/process-group identity
is checked before timeout or stop signals. All workers have a total wall-clock
budget, including retries and waiting after the first start.

`tusker stop fixes --drain` stops admission and lets existing attempts
finish. Without `--drain`, it stops running workers too. `--wake-on parked`
returns as soon as a novel question parks; other admitted workers remain
supervised and the batch can be resumed. Default behavior waits for independent
work to finish. `tusker show fixes --json` retains complete state.

| Outcome | Automatic action | Cap per task |
| --- | --- | --- |
| Harness crash or missing supervisor receipt | Resume from existing workspace/evidence | 1 |
| Usage/rate limit | Wait, then resume (`--usage-wait`, default 60m) | 12 |
| Only registered flaky tests fail | Run each supplied isolated command twice; retain logs; continue after two passes | 1 per test |
| Unknown failing gate | Resume with repair-or-actionable-error instruction | 1 |
| Question cites active fork ID, or exactly matches its question | Resume with the ruling | 2 |
| Novel question or checkpoint | Park, retain evidence, continue independent work | No automatic answer |
| Budget exhausted | Stop the worker group, mark stalled, retain evidence | No automatic budget increase |

Register a flaky test with `tusker new flaky <exact-test-id> --reason <text>
--until <YYYY-MM-DD|RFC3339|fix-task-id>`. A date expires at the end of that UTC
date. A linked task expires on done/cancelled/superseded. Matching is exact;
expired or missing fix-task references never authorize an exception. Failed
report gates carry `test` and `isolated_command`. The command must isolate that
test and must include any remote-host invocation it needs.

## Reports, audits, and follow-ups

Attempt artifacts live under `<state-root>/coordinator/<vault-hash>/<batch>/
<task>/<attempt>/`. `TUSKER_BATCH_RUN_DIR` points there. The directory contains
`prompt.md`, `pid`, `process.json`, `run.log`, `supervisor.log`, `receipt.json`,
`last.md`, and worker-written `REPORT.json`/`REPORT.md`. These are durable
runtime artifacts, not evictable vault scratch. `tusker runs events
<batch>:<task>` reads the task's shared event log.

The JSON report schema is `tusker.batch-report/v1`:

```json
{
  "schema": "tusker.batch-report/v1",
  "task": "APP-T-0001",
  "verdict": "pass",
  "commits": ["123abcd"],
  "gates": [{"acceptance":"A1","command":"go test ./affected","result":"pass","host":"local"}],
  "questions": [],
  "remaining": [{"title":"Follow-up","why":"Remaining behavior","evidence":"file.go:42"}],
  "boxes": [],
  "path_exceptions": [],
  "first_blocker": ""
}
```

Verdicts are pass, checkpoint, fail, blocked. Every acceptance ID needs a
passing gate with a command. An actual completed harness receipt is also
required; a pass report cannot hide a crash. The audit inspects every reported
commit and detects omitted commits since the baseline. It checks target-branch
reachability, configured Git author, AI attribution, ignored/scratch paths,
ownership (or exact path plus reason in `path_exceptions`), remaining uncommitted
changes, and evidence/gate coverage. Executable `.tusker/audit.d/*` hooks receive
the task ID and commit list, run in the task workspace, and have a one-minute
limit. Audit failures park the task before dependents advance.

No-push auditing is explicitly evidence-limited: changed local remote-tracking
refs or logged push commands raise a failure. Neither unchanged refs nor an
absent command proves that no remote write occurred. The digest must not claim
stronger assurance. The audit records reported check results; it does not turn
worker testimony into independent test execution or human acceptance.

Every valid `remaining` item becomes a backlog task in the parent's epic, with
origin/evidence and inherited ownership hints. A normalized-title match adds a
parent link instead of creating another task. Harvesting happens for blocked
and failed reports too. Follow-ups are never automatically admitted to the
current batch. Promote, merge or discard them during review.

## Continuity, spend, and cleanup

`digest --since last` tracks a per-vault watermark and shows running work,
recent results, human gates, coordinator questions/audit failures, newest ten
open follow-ups, and live boxes. Human-readable summaries are capped at 40
lines; JSON retains the full result. The batch digest also records per-attempt
harness token counts when supplied, worker wall time, policy actions and costs.

Workers report boxes with `id`, `type`, `created`, optional `deleted`, `expires`,
`eur_per_hour`, and final `eur`. Timestamps are RFC3339. The ledger accepts
`tusker ledger box add --id <id> --type <type> --eur-per-hour <rate>
--expires <RFC3339> --batch fixes --epic APP` and `ledger box delete --id <id>`.
Those commands record facts; they do not provision or delete cloud machines.
`ledger --json` includes batch, epic and UTC-day cost totals (final bills apportioned by elapsed time). Unclosed
boxes accrue estimated cost and overdue expiry warnings. The digest reads this
ledger, rather than reparsing worker reports.

`tusker triage --epic APP --older-than 14d --output <new-file.json>` previews
open tasks with dead refs, inactivity, duplicate titles and changed owned paths.
Review each row's action (`keep`, `discard`, `merge`), reason, and optional
`merge_into`, then call `tusker triage apply <file>`. Revisions must still match;
stale review tables refuse. Discard preserves the task body and history.
No old tasks are discarded merely because of age or a numeric cleanup target.

The integration tests in `cmd/tusker/coordinator_test.go` exercise this mode
using synthetic local CLI workers. They require no paid harness, daemon or box.
Production harness authentication and the rzn campaign's human-reviewed vault
cleanup remain deployment exercises, separate from those deterministic checks.

## Task body template

Save a body using this shape and pass it to `tusker new task --body-file <path>`:

```markdown
# Concrete outcome

## Problem
Observed failure, evidence, and the desired behavior.

## Prior work to read
Existing implementation and governing decision, with valid file/section links.

## Steps
1. Implement the smallest complete change.
2. Exercise the acceptance checks and record evidence.

## Acceptance
- [ ] A1: Observable behavior and expected result.

## Verification
| Covers | Check | Result | Notes |
| --- | --- | --- | --- |
| A1 | command: <exact focused check> | pending | <evidence> |

## Owned and off-limits paths
Owned paths match --owned-paths. Preserve other work.

## Rules
Use active epic rulings; report unresolved questions and every remaining item.
```

## Reproducing verification

Run the focused workflow checks with:

```sh
go test ./cmd/tusker -run '^TestCoordinator' -count=1
go test -race ./internal/runner -count=1
```

The coordinator tests cover the three-task dependency/resource schedule,
controller death with a surviving worker, missing profiles, dead references,
crash and ruling recovery, flaky and usage caps, novel questions, automatic
audits, follow-up harvesting and deduplication, session watermark, ledger,
reviewed triage, reservation rollback/replacement, canonical vault lookup,
wall-clock budgets, and independently supervised check deadlines. Synthetic
workers create real local Git commits and reports; no paid model is involved.

The production rollout remains an explicit exercise in the target project:
select real runner profiles, review the triage table, run the agreed backlog
batch, and inspect its retained branches and evidence before accepting it.
A two-week wake/relaunch/lost-follow-up measurement requires those real runs;
unit and integration results cannot establish that operational outcome.
