# Orchestration Runbook

Tusker orchestration has four layers:

```text
markdown work records  -> durable human/task truth
runtime store/leases   -> local process truth
events/attempts        -> audit trail
generated dashboards   -> rebuildable projections
```

Only markdown work records and accepted evidence/gates should drive lifecycle truth. Runtime state is operational bookkeeping.

## Execution ownership

| Role | Owns | Must never do |
|---|---|---|
| Interactive Codex/Claude | Current user's shell commands, edits, tests, and task updates | Start a foreground daemon, invoke dispatch, or launch another model runner |
| Resident daemon | Poll enabled projects and launch eligible background implementation/review workers | Dispatch disabled projects or treat task creation alone as authorization |
| Implementation worker | One injected task, attempt, and workspace | Work another task, launch a daemon/runner, or self-review |
| Reviewer worker | Read-only verification of one implementation handoff | Edit implementation files or review forever |

Interactive sessions do not enter the daemon claim/heartbeat lifecycle. Before
taking over the same tracked task they inspect an existing live run and
coordinate; disabled automation never blocks their direct work.

## Dispatch eligibility

A daemon or agent runner may pick up a task only when:

```text
effective kind == task
status in ready|rework
readiness == ready
next_owner == agent or agent:<name>
no valid closeout checkpoint says stop_until_human_response
no open blocking non-agent gate exists
```

Do not dispatch `review` tasks to implementation workers. Review belongs to reviewer/human lanes.

## Runtime loop

```text
poll:
  read current task/gate/evidence index
  reconcile running leases against task state
  skip terminal, human-wait, external-wait, and reviewer-owned work
  pick ready/rework agent-owned task
  claim/lease in runtime store
  run configured runner
  collect attempt summary and proof artifacts
  if machine work complete:
    request review or emit closeout
  if task still agent-owned and machine gaps remain:
    allow one continuation within budget
  if only human/external gaps remain:
    release lease, write stop decision, do not retry
```

## Closeout path

```text
runner exits cleanly
      |
      v
classify proof/gates by owner
      |
      +-- machine gaps remain -> one continuation/rework path
      |
      +-- reviewer gaps remain -> request reviewer once
      |
      +-- human/external gaps only
              |
              v
       emit review packet/checkpoint
       set/recognize held plus a human/external next_owner
       release lease
       supervisor decision: stop_for_human / stop_for_external
       no continuation retry
```

## Review lane

Reviewer lanes are independent from implementation workers.

- Low/medium risk may be closed by an allowed reviewer when proof, gates, docs impact, and policy pass.
- High/critical risk may get advisory reviewer output, but final close follows human/reviewer policy.
- If review fails, move to `rework` with exact acceptance gaps.
- If review leaves only human gates, set/recognize human-wait and stop.
- Dispatch at most one reviewer per handoff and three automated review cycles per task. At the cap, leave the task in review for operator intervention.

## Continuation retry rule

A clean runner exit is not automatically a reason to continue.

Continue only when:

```text
machine_missing is nonempty
same state has not already consumed continuation budget
next_owner is agent-owned
readiness is ready
```

Do not continue when:

```text
machine_missing is empty
human_missing or external_missing is nonempty
open blocking gates are human/external-owned
closeout packet exists for current fingerprint
```

## Validation cache

Validation should be keyed by a state fingerprint:

```text
git HEAD + dirty hash + task/gate/evidence state + command + tusker version
```

Same fingerprint plus previous pass means validation is reusable. Do not re-run validation on an unchanged human-wait state.

## Supervisor decisions

Useful normalized decisions:

| Decision | Meaning |
|---|---|
| `continue_machine_work` | Machine gaps remain and budget allows continuation. |
| `request_review` | Machine proof is sufficient; reviewer owns next action. |
| `stop_for_human` | Only human-owned blockers remain. |
| `stop_for_external` | Only external system/service blockers remain. |
| `stop_budget_exceeded` | Machine attempts exceeded configured budget. |
| `interrupt_stale_state` | Task state changed under a running lease. |

## Human-wait packet

A review packet/checkpoint should contain:

- task ID and title;
- last clean validation command/result/fingerprint;
- proof mode and acceptance coverage;
- machine/reviewer/human/external missing proof lists;
- open gates with owner/action/verification;
- artifacts/evidence links;
- exact human options: accept, waive, reject/rework.

## Failure containment

- Do not paste raw logs into task bodies.
- Do not store transcripts as evidence.
- Do not use generated dashboards as source of truth.
- Do not spawn repeated subagent audits after a valid closeout packet.
- Do not let open human gates make a task agent-runnable.
