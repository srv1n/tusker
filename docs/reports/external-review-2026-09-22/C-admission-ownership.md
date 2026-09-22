# RESULT

```json
{
  "kind": "review",
  "verdict": "request_changes",
  "risk": "high",
  "summary": "The commit enables shared-checkout parallelism without isolating worker submissions, rejects valid wave members during arming, and exposes a task-start path that acknowledges backlog work the daemon never schedules. Adoption can also race runtime ownership, while capacity waits erase recovery lineage. These defects can lose acknowledged submissions, duplicate execution, indefinitely block work, or misreport lifecycle state.",
  "findings": [
    {
      "severity": "high",
      "path": "cmd/tusker/armed_wave.go",
      "line": 390,
      "problem": "Removing the shared-checkout serial-admission guard allows concurrent wave workers to share .tusker-worker-lifecycle.json. Their acknowledged submissions overwrite one another, and reconciliation does not bind the queued request to the run being reconciled. A completed task can lose its submission and be retried, or one task can consume another task's submission.",
      "fix": "Restore serial admission for shared-checkout waves until lifecycle requests are stored per attempt and consumption checks project, record, attempt, and lease generation against the current run. Keep isolated-workspace parallelism unchanged.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/direct_wave_authority.go",
      "line": 1148,
      "problem": "Wave arming validates every member with the dispatch-state validator but filters out only readiness failures. Canonical dependency-waiting members have next_owner=blocked_dependency, completed members have next_owner=none, and review members have next_owner=reviewer. These valid lifecycle states therefore prevent the entire wave from starting or retrying.",
      "fix": "Exclude lifecycle ownership checks from this arming-only contract validation, as readiness is already excluded. Preserve full status, readiness, and next_owner checks when selecting and dispatching individual frontier tasks.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/direct_wave_authority.go",
      "line": 1115,
      "problem": "The new tusker run task route accepts a standalone backlog task and reports it queued, but its helper only inserts a directive. The daemon filters out backlog before consulting the directive, and its only backlog projection requires an armed wave. With default trigger states, the standalone task never creates an attempt or consumes its directive.",
      "fix": "Add a directive-authorized backlog-to-ready transition before the daemon's active-state filter. Persist that transition with the existing canonical CAS discipline before claiming, and retain normal dependency, ownership, and concurrency checks.",
      "creates_followup_task": false
    },
    {
      "severity": "high",
      "path": "cmd/tusker/work_recovery.go",
      "line": 330,
      "problem": "Adoption checks live ownership only before running verification. Another task with overlapping owned paths can claim and start while verification runs. The final material lock and byte checks do not recheck runtime ownership, so adoption can publish done or review while a different worker still owns and can change the adopted material.",
      "fix": "At the adoption commit boundary, acquire the same owned-path claim lock used by run admission, reload current and conflicting run ownership, and refuse any new live holder. Keep that exclusion through the canonical transition.",
      "creates_followup_task": false
    },
    {
      "severity": "medium",
      "path": "cmd/tusker/daemon.go",
      "line": 4453,
      "problem": "Recovery parentage and the recovery prompt are derived solely from previousRun.LastError. A normal global, project, runner, or resource-capacity wait overwrites that field. When capacity returns, the recovery attempt is recorded without its parent or recovery child type and loses the instruction to inspect and preserve the uncertain prior work.",
      "fix": "Read recovery identity from durable redrive metadata or an explicit persisted recovery field, rather than LastError. Use the same identity for attempt parentage and prompt construction, retaining it until the recovery claim is committed.",
      "creates_followup_task": false
    }
  ]
}
```

# Findings

The attached snapshot was the review source; the daemon, wave-authority, and workspace-manager blob hashes match the requested commit. The findings below are source-traced failures. **Repository tests could not execute:** `go.mod:3` requires Go 1.26.5, while the available toolchain is Go 1.23.2; the focused `go test` command stopped before compilation.

### 1. High — Parallel shared-checkout workers overwrite and cross-consume submissions

**Location:** `cmd/tusker/armed_wave.go:382–390`.

This commit removes the guard requiring serial execution for shared-checkout waves. With wave, project, and global capacity permitting two workers, tasks A and B can therefore run in the same checkout even when their declared source paths are disjoint.

The submission transport is not disjoint: `queueWorkerLifecycle` writes both requests to the same checkout-relative `.tusker-worker-lifecycle.json`, returning success after an atomic replacement—not an append or attempt-scoped insertion (`cmd/tusker/daemon_worker_lifecycle.go:22–35`).

A concrete failure sequence is:

1. A submits; its request is acknowledged.
2. B submits before reconciliation; B replaces A’s request and is also acknowledged.
3. B is reconciled first and removes the request file. A’s submission is now absent.
4. A exits successfully while its canonical task remains active. The daemon classifies that as an early exit and schedules continuation rather than honoring the acknowledged submission (`cmd/tusker/runner_exit_classification.go:45–46`; `cmd/tusker/daemon.go:2664–2669`).

The opposite reconciliation order is also unsafe. The consumer validates the request’s identity against **the run named inside the request**, not the run passed into reconciliation (`cmd/tusker/daemon_worker_lifecycle.go:38–59,152–169`). Reconciling A can therefore apply B’s submission and set A’s local `workerSubmitted` flag (`cmd/tusker/daemon.go:2582–2597`).

**Minimal fix:** Restore the shared-checkout serial guard for this release. Remove it only after making requests attempt-scoped and checking their complete identity before consuming or deleting them. Disjoint `owned_paths` alone do not protect this shared control file.

### 2. High — The new arming check rejects valid dependency-waiting, completed, and review members

**Location:** `cmd/tusker/direct_wave_authority.go:1148–1152`; unconditional caller at `1200–1202`.

`directWaveArmContractBlockers` deliberately accepts each member’s current status and ignores `"readiness is …"` failures, but it still retains the dispatch validator’s `"next_owner is …"` failures.

Those failures are expected canonical state, not malformed contracts:

* An unfinished dependency produces `next_owner: blocked_dependency` (`cmd/tusker/commands_v7.go:3053–3058`).
* A completed task produces `next_owner: none` (`cmd/tusker/commands_v7.go:2995–3001`).
* A review task produces `next_owner: reviewer` (`cmd/tusker/commands_v7.go:3037–3043`).

The reused validator accepts only `agent` or `agent:*` (`cmd/tusker/commands_v7.go:4372–4375`).

Consequently, an ordinary A → B wave can fail to arm because B is correctly waiting for A. A partially completed wave can likewise fail a start/retry because a completed member correctly has no next owner. The rejection occurs before frontier queueing.

**Minimal fix:** Make this check contract-only with respect to lifecycle state: omit `next_owner` eligibility here, just as readiness is omitted. Continue enforcing both at actual task dispatch. Do not rewrite canonical owners merely to pass arming.

### 3. High — The new task-run command can acknowledge work that never reaches scheduling

**Location:** `cmd/tusker/direct_wave_authority.go:1114–1115`; scheduling filter at `cmd/tusker/daemon.go:1135–1142`.

For an enabled, registered project, create a valid standalone task with `status: backlog`, `readiness: ready`, and no dependencies. The new `tusker run <task>` route forwards to background task start.

That helper explicitly accepts backlog (`cmd/tusker/direct_wave_authority.go:1398–1403`), inserts a task-scoped directive, and reports `Queued` without changing canonical status (`1457–1469`).

The consumer does not match that admission contract. Default trigger states are `ready,rework` (`cmd/tusker/workflow.go:661–669`). Before reading a directive or reopening a terminal run, the daemon rejects other statuses unless `armedWaveDispatchTaskProjection` promotes them. That projection cannot apply to a standalone task: an empty wave ID returns “not armed” (`cmd/tusker/armed_wave.go:250–254,313–316`).

Repeated polls therefore skip the task despite available capacity and valid authorization. This is an integration defect exposed by the newly added command; the underlying background-start helper already contains the incomplete producer path.

**Minimal fix:** Recognize a current task-scoped directive before filtering backlog out, and perform its authorized canonical ready transition before claiming. Do not solve this by making all backlog tasks globally dispatchable.

### 4. High — Adoption’s ownership check expires while verification runs

**Location:** `cmd/tusker/work_recovery.go:330–356`, following the one-time ownership checks at `238–255`.

`adoptCompletedWork` checks the current task’s live run and overlapping owned-path holders, then executes verification (`cmd/tusker/work_recovery.go:278`). It acquires the material lock only afterward.

Consider task A being adopted and another ready task B declaring an overlapping source path. After A’s initial ownership check, B can acquire a legitimate lease and start. B need not have changed any bytes yet. A’s material, contract, dependency, and state-revision checks still pass, and adoption proceeds to `done` or `review` (`cmd/tusker/work_recovery.go:357–380,412–424`).

The final checks never reload conflicting run ownership. The terminal preflight is insufficient: it checks a live runner for A, not an overlapping holder B (`cmd/tusker/runtime_terminal_retirement.go:50–65`). Thus A can be reported complete while B is actively entitled to modify the material used for A’s proof.

**Minimal fix:** At final admission, take the existing owned-path claim exclusion, reload current and overlapping holders, and hold that exclusion through the canonical transition. Runtime claims already use this lock around their ownership check and claim (`cmd/tusker/run_ownership.go:263–279,435–453`); the material-document lock is not a substitute.

### 5. Medium — A capacity wait silently converts recovery into ordinary execution

**Location:** `cmd/tusker/daemon.go:4453–4460`; prompt construction at `6801–6813`.

Unknown-outcome recovery records its parent ID in the redrive reason (`cmd/tusker/serve_runs.go:498`), which becomes `RunStatus.LastError` (`cmd/tusker/run_runtime_commands.go:1168–1181`). Dispatch later parses that string to decide the attempt’s parent, `ChildType: recovery`, and special recovery instructions.

Ordinary scheduling replaces this field. For example, a full global slot invokes `persistFairDispatchReason` (`cmd/tusker/daemon_scheduler.go:481–484`), which assigns a new `LastError` (`324–326`). Project, runner, state, and resource waits use the same mechanism. Once the slot opens, the scheduler clears its waiting reason before dispatch (`572–575`).

The resulting attempt has no recovery parent/type and receives no recovery-specific instruction to inspect and preserve the previous uncertain work. The recovery-child lookup also has no correctly attributed child to recognize (`cmd/tusker/serve_runs.go:487–490`).

**Minimal fix:** Preserve recovery identity independently of presentation/error text. The durable redrive record already stores the reason; use an authoritative persisted recovery identity for both prompt construction and attempt creation, including across waits and restart.

# Architecture notes

The material risk is **checkout-scoped control state being treated as though task-owned source-path isolation also isolates lifecycle messages**. That assumption becomes false when this commit admits parallel shared-checkout workers.

Adoption has a separate authority mismatch: canonical document locking protects task bytes, while the owned-path claim lock protects runtime writers. Publishing adoption requires both protections at the transition boundary.

# Missing tests

* **Parallel shared-checkout submission:** Extend `cmd/tusker/daemon_worker_lifecycle_test.go` with two active runs sharing one checkout, both submitting before reconciliation. Exercise both reconciliation orders and assert that neither acknowledged request is lost or consumed by the other run.
* **Canonically projected wave members:** Extend `cmd/tusker/direct_wave_authority_test.go` with an A → B dependency wave after canonical projection, plus a partial wave containing a done member. The helper currently defaults `next_owner` to `agent` (`50–57`), masking the new arming failure.
* **Start-to-poll integration:** Extend `TestDirectRunQueuesTask` beyond directive insertion (`cmd/tusker/direct_wave_authority_test.go:178–191`). Start a standalone backlog task, poll with a fake runner, and assert canonical promotion, exactly one attempt, and directive consumption.
* **Ownership arriving during adoption:** In `cmd/tusker/adopt_completed_test.go`, use the existing pre-commit hook to claim an overlapping task after verification begins. Adoption must refuse without publishing done/review; include a claimed-but-not-yet-spawned holder.
* **Recovery through scheduling delay and restart:** Extend `cmd/tusker/walkthrough_recovery_test.go` through actual dispatch after capacity or resource contention, including a daemon restart. Assert preserved parentage, recovery child type, and recovery prompt. The current bounded-recovery test manually inserts the child rather than exercising this path (`279–288`).

# If you change one thing

**Restore serial admission for shared-checkout waves until submission storage and consumption are attempt-scoped.** This immediately removes the newly enabled cross-run submission corruption while preserving parallelism for isolated workspaces.
