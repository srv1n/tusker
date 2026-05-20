# Dispatcher Pseudocode

Runtime dispatch reads task/gate/evidence indexes and runtime leases. It does not mutate protected lifecycle fields directly from implementation workers.

```text
poll:
  index = load_work_index()
  reconcile_runtime(index)

  for task in index.tasks:
    if effective_kind(task) != "task":
      continue
    if task.status not in ["ready", "rework"]:
      continue
    if task.readiness != "ready":
      continue
    if not owner_is_agent(task.next_owner):
      continue
    if closeout_checkpoint_valid(task):
      continue
    if has_open_non_agent_blocking_gate(task):
      continue

    claim(task)
    result = run_worker(task)
    report = classify_proof_and_gates(task)

    if report.machine_missing:
      if continuation_budget_available(task):
        queue_continuation(task, report.machine_missing)
      else:
        stop_budget_exceeded(task, report.machine_missing)
      continue

    if report.reviewer_missing:
      request_review_once(task)
      continue

    if report.human_missing or report.open_human_gates:
      emit_closeout_packet(task, report)
      release_claim(task)
      stop_for_human(task)
      continue

    if report.external_missing:
      emit_closeout_packet(task, report)
      release_claim(task)
      stop_for_external(task)
      continue

    request_review_or_close_by_policy(task)
```

The durable markdown task is the workflow contract. Runtime storage is process bookkeeping.
