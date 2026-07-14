# Closeout Protocol

## Stop Conditions

Stop and wait for a human when:

- OAuth or browser login requires a human-owned account.
- Requirements contradict each other.
- A security, release, privacy, or policy decision is required.
- Manual smoke/physical verification is required and no agent can perform it.
- `closeout status` reports `agent_action=stop_until_human_response`.

Do not stop for a question already answered by the approved task, acceptance
criteria, governing spec, or linked decision. That is implementation work, not
a human gate.

## Approval Boundary

Human gates are limited to:

- capability: credentials, login/account ownership, unavailable devices or environments;
- authority: security, privacy, legal, billing, production release, or destructive external actions;
- unresolved intent: a concrete contradiction or omission in the approved contract, with the agent's recommended resolution;
- subjective acceptance: explicitly requested human review of screenshots, recordings, UX/brand feel, or final artifacts.

Agents and reviewer lanes own code/diff review, test and log inspection,
implementation judgment, objective artifact checks, and every choice already
entailed by the contract. Do not create `decision` or `signoff` gates to ask a
human to accept removal, migration, mapping, naming, compatibility, or other
implementation behavior the contract already specifies.

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

`why_agent_cannot` must name the actual capability or authority boundary. “The
human should approve this,” “this is high risk,” and “human review is safer” are
not boundaries. Risk may reserve final close through configured policy without
creating a per-task approval gate.

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
