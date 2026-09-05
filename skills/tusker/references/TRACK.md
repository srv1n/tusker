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
  --priority p2 --size m --risk medium \
  --spec-refs .tusker/specs/auth.md \
  --owned-paths cmd/auth.go,internal/auth \
  --generated-outputs internal/auth/openapi.gen.go
```

`--owned-paths` names changed source; `--generated-outputs` names its generated
files. Both are comma-separated. Execute/review closeout needs at least one
owned, generated, spec, or shared material input for its review hash.
Evidence-only proof needs no execution review. Plans declare the same fields
per task; import checks simultaneous ownership collisions.

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
tusker attempt start <TASK-ID>
tusker verify add <TASK-ID> --covers A1 --check "command: go test ./..." --result pending
tusker attempt handoff <TASK-ID>
tusker finish <TASK-ID> --request-review
tusker close <TASK-ID>
```

`tusker discard <TASK-ID>` cancels; `tusker status <TASK-ID> rework` reopens.
Runtime activity is not a durable status.

## Proof

The smallest verification set covering acceptance wins: exact command,
executor-recorded PASS/FAIL, bounded note. Command rows are added as
`pending`; an agent must not claim their result. Noisy output goes in
`.tusker/scratch/<TASK-ID>/`, deleted on close or after 14 days. At tier 2+,
close requires proof.

## Gates

A gate records one missing human fact: authority, credential, unresolved
intent, or subjective acceptance (UX, brand, legal). Settled facts and risk
alone are not gates.

```bash
tusker new gate --vault ./.tusker --blocks <TASK-ID> --kind <KIND> \
  --owner human:<name> --action <ACTION> --verification <PROOF> \
  --why-agent-cannot <BOUNDARY>
```
