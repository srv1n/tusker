# Closeout Protocol

## Stop Conditions

Stop and wait for a human when:

- OAuth or browser login requires a human-owned account.
- Requirements contradict each other.
- A security, release, privacy, or policy decision is required.
- Manual smoke/physical verification is required and no agent can perform it.
- `closeout status` reports `agent_action=stop_until_human_response`.

## Required Human Gate Fields

A useful gate states:

```yaml
gate_kind: auth | env | setup | decision | verification | signoff | security | release
owner: human:<name>
blocking: true
why_agent_cannot: "..."
action: "exact human action"
verification: "how completion is known"
```

## Gap ownership

The agent owns machine-fixable gaps. Humans own gates that require credentials, product calls, signoff, or external systems the agent cannot access.

## Validation cache rule

Do not repeat expensive validation while a human gate is unchanged. Re-run only after task, proof, code, or gate state changes.

## Loop guard

If the same blocker appears twice, create or update one gate and stop. Repeated retries are token waste.

## Subagent/audit guard

Do not spawn subagents or broad audits to bypass a human gate. Produce a bounded closeout packet instead.

## Agent Behavior

After a blocking human gate is recorded, do not keep running verification loops. Emit a closeout packet when machine work is complete, then stop.

## Final human-wait response

Use this shape:

```text
Machine work is complete or blocked at <TASK-ID>/<GATE-ID>.
Human action required: <exact action>.
Resume with: tusker closeout status <TASK-ID> --json
```

Use outcome `machine_complete_waiting_for_human` when proof is otherwise complete but a human gate remains.
