# Operator Intervention

Use CLI control commands for manual intervention. Do not hand-edit protected lifecycle fields unless repairing corruption.

## Resume agent work

Use this when a human rejects a subjective gate or a typed reviewer result
requests concrete machine changes:

```bash
tusker status <TASK-ID> rework --by human:<name> --reason "<specific failed acceptance item>"
```

Set or expect:

```yaml
readiness: ready
next_owner: agent
agent_action: continue
```

Old closeout checkpoints are stale after rework.

## Stop for human

Use or confirm this when the machine work is complete and only human gates remain:

```yaml
status: review
readiness: held
next_owner: human:<name>
agent_action: stop_until_human_response
```

Do not ask the agent to continue unless you also accept, waive, or reject the gate.

## Cancel a task

```bash
tusker status <TASK-ID> cancelled --by human:<name> --reason "<why cancelled>"
```

Cancellation is terminal. Create a new task if the work continues under different scope.

## Accept or waive gates

```bash
tusker gate satisfy <GATE-ID> --by human:<name> --evidence "<what was reviewed>"
tusker gate waive <GATE-ID> --by human:<name> --reason "<why waiver is acceptable>"
```

After gate changes, run one final closeout validation if the task can close.

## Verify deterministic completion

```bash
tusker proof status <TASK-ID> --json
tusker validate --json
tusker automation explain <TASK-ID> --json
```

Normal review completion is not an operator ceremony. The independent reviewer
submits only a typed verdict; Tusker's deterministic completion reactor
materializes verification, integrates the exact reviewed SHA, closes the task,
and wakes newly eligible successors. If that reactor is disabled or parked,
repair the named authority/configuration or transaction blocker. Do not ask a
reviewer to merge or close as a fallback.

Never delete prior evidence or summaries just to make the task look cleaner. Supersede stale evidence and keep the current truth obvious.
