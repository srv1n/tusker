# Operator Intervention

Use CLI task-management commands for manual intervention. Do not hand-edit
protected lifecycle fields.

## Return a task to rework

```bash
tusker status <TASK-ID> rework --by human:<name> \
  --reason "<specific failed acceptance item>"
```

## Cancel a task

```bash
tusker discard <TASK-ID> --reason "<why cancelled>"
```

Cancellation is terminal. Create a new task if the outcome continues under a
different contract.

## Accept or waive gates

```bash
tusker gate satisfy <GATE-ID> --by human:<name> --evidence "<reviewed proof>"
tusker gate waive <GATE-ID> --by human:<name> --reason "<why waiver is acceptable>"
```

## Verify completion state

```bash
tusker proof status <TASK-ID> --json
tusker show <TASK-ID> --capsule
```

Never delete prior evidence merely to make a task look clean. Supersede stale
evidence and keep the current truth obvious.
