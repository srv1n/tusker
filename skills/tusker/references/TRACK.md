# Track

Inspect tracked work first:

```bash
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID>
```

## Create

One bounded outcome is one task. Use an epic for several tasks. Capture
observable acceptance and non-goals; put unresolved decisions in a gate.

```bash
tusker new epic --vault ./.tusker --acronym APP --title "App foundation"
tusker new task --vault ./.tusker --epic APP --title "Implement auth" \
  --priority p2 --size m --risk medium
```

## Amend an imported contract

For an open, disarmed wave with `backlog`/`held` tasks, revise its delivery plan
but preserve `scope` and every `source_key`:

```bash
tusker delivery import --plan <plan.yaml> --dry-run --json
tusker delivery import --plan <plan.yaml>
```

Re-import amends canonical contracts without changing IDs; progressed/frozen
plans refuse. `tusker update` installs skills, not task changes.

## Lifecycle

Durable flow is `idea -> backlog -> ready -> review -> done`, always via CLI:

```bash
tusker status <TASK-ID> ready --reason "Contract is actionable."
tusker verify add <TASK-ID> --covers A1 --check "command: go test ./..." --result pass
tusker status <TASK-ID> review --reason "Acceptance proof recorded."
tusker close <TASK-ID>
```

`tusker discard <TASK-ID>` cancels; `tusker status <TASK-ID> rework` reopens.
Runtime activity is not a durable status.

## Proof

The smallest verification set covering acceptance wins: exact command,
PASS/FAIL, bounded note. Noisy output goes in `.tusker/scratch/<TASK-ID>/`,
deleted on close or after 14 days. At tier 2+, close requires proof.

## Gates

A gate records one missing human fact: authority, credential, unresolved
intent, or subjective acceptance (UX, brand, legal). Settled facts and risk
alone are not gates.

```bash
tusker new gate --vault ./.tusker --blocks <TASK-ID> --kind <KIND> \
  --owner human:<name> --action <ACTION> --verification <PROOF> \
  --why-agent-cannot <BOUNDARY>
```
