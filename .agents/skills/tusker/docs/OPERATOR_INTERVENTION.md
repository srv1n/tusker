# Operator Intervention

Use CLI control commands for manual intervention. Do not hand-edit protected lifecycle fields unless repairing corruption.

## Resume agent work

Use this when a human/reviewer rejected closeout and the task has concrete machine work again:

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

## Verify and close

```bash
tusker proof status <TASK-ID> --json
tusker validate --json
tusker close <TASK-ID> --by reviewer:<name> --reason "<acceptance/proof summary>"
```

Low/medium work may be closed by an allowed independent reviewer. High/critical work follows the configured human/reviewer policy.

Never delete prior evidence or summaries just to make the task look cleaner. Supersede stale evidence and keep the current truth obvious.
