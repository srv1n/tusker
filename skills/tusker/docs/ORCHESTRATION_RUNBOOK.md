# Task Management Runbook

Tusker's supported agent role is deliberately small:

```text
request -> task contract -> lifecycle state -> proof/gates -> closeout
```

## Read

```bash
tusker list
tusker search "<query>"
tusker show <TASK-ID> --capsule
tusker show <TASK-ID> --acceptance
```

Use the smallest useful view. Task records and accepted evidence/gates are
durable truth; generated dashboards and raw runtime output are not.

## Mutate

Use `tusker new`, `tusker status`, `tusker verify add`, `tusker gate`,
`tusker discard`, and `tusker close`. Never hand-edit protected lifecycle,
proof, gate, or generated fields.

## Human wait

Stop changing Tusker state when only a human-owned fact, authority, credential,
or subjective judgment remains. Report the exact task/gate ID and clearing
action. Do not retry unchanged validation or manufacture proof.

## Failure containment

- Keep raw logs and transcripts out of task bodies.
- Keep proof to the smallest acceptance-mapped rows.
- Treat tracker failure separately from implementation or test failure.
- Do not run setup repair or background execution as a side effect of task
  management.
