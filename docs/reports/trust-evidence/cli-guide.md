# CLI guide evidence

Status: command/proof guidance passed; skill density and convergence need the
root source-coherence release before rerun.

The authoritative CLI help and installed `skills/tusker` guide now add command
verification as `pending`, then hand off and request review. They do not suggest
an agent can write a command result as `pass`. `verify add` still accepts manual
rows where appropriate; only command rows are executor-recorded. The guide
retains the explicit human-only gate boundary and interactive no-dispatch rule.
One-task creation now accepts `--spec-refs`, `--owned-paths`, and
`--generated-outputs`; the shipped example declares all three. Read-only and
artifact-only work can record evidence without an execution review, but every
execute/review closeout must declare an owned, generated, spec, or shared
material input for a meaningful review hash. Progressive disclosure was reduced
by removing the universal stale-reset section from the router and restoring the
onboarding storage boundary; recorded word counts were re-measured without
raising their ceilings.

Executed on local macOS arm64 at `03201019`:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^(TestTrustCliGuideUsesExecutorRecordedCommandProof|TestVerifyAddCannotForgeCommandExecutionResult)$' -count=1 -v
```

Result: PASS (2.124s, shared focused command), before the later skill density
repair and single-task scope addition. A current focused rerun was attempted:

```sh
scripts/with-validation-lock.sh -- go test ./cmd/tusker -run '^(TestTuskerSkillProgressiveDisclosure|TestTuskerSkillLifecycle|TestTrustCliGuideUsesExecutorRecordedCommandProof)$' -count=1 -v
```

It is currently blocked before the selector by unrelated stale test callsites:
`claimExistingWithAuthorization` now requires `RunAttempt`, while the factory,
fair-dispatch, and work-session tests still pass three arguments. Repair those
shared test callsites, then rerun the command above.
