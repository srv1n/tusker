# Task authoring admission contract

This report records the route and admission contract for FLW-T-0037. A task
may remain held and inspectable with an unresolved route. A preview never starts
a provider. A start or explicit wave execution must resolve the current worker
and reviewer before it creates execution authority.

## One authority, two lanes

`resolveRunProfileForLane` is the shared lane resolver. It delegates selection
to `resolveRunnerProfileForNote`, which applies this precedence independently
for `execute` and `review`:

1. lane-specific task override (`execute_profile` or `review_profile`),
2. legacy task `runner_profile`,
3. first matching `automation.runner_routing` rule,
4. `automation.runner_lane_profiles` mapping,
5. the task's work/review level mapping,
6. legacy semantic complexity role,
7. the configured or built-in default profile.

The route preview exposes the selected profile, model, effort, harness,
provenance, fallback candidates and repair blockers. Resolver hints are retained
in blockers, so an empty mapping names the exact
`automation.model_levels.<level>.<lane>` key to repair.

For projects still using the legacy workflow runner fields, preview and wave
admission pass the same lane-specific compatibility runner as daemon setup:
`agents.default` (including task ownership overrides) for execute and
`reviewer.runner` with the agents default fallback for review. Named profiles
still win; the legacy value only applies when resolution reaches the built-in
fallback.

| Surface | Previous behavior | Current admission behavior |
| --- | --- | --- |
| `tusker runner route` | Resolved a lane read only, but discarded structured repair hints; malformed workflow stopped before a JSON result. | Uses the shared resolver, preserves hint/path details, and emits a read only blocked JSON preview when the workflow cannot be loaded. |
| Wave preflight / arm | Checked only general runner availability; a green runner check could hide empty member mappings. | Resolves execute and review for every member through the production workflow inspector. Any failure sets `checks.runner=false` and reports member, lane and repair action. |
| Delivery Start | Rechecks the wave under the final material lock. | Keeps that recheck and receives the same per-member lane resolver; stale configuration refuses arm before execution authority is published. |
| Serve wave Execute | An already armed wave could queue directives from a cached snapshot without a fresh route read. | Immediately before `QueueWaveRunDirectives`, reloads the read only route inspector and resolves both lanes for every eligible member. No directive is written on failure. |
| Daemon dispatch | Performs its own execute/review resolution at claim/dispatch boundaries. | Remains the late safety check; the preview and queue checks do not replace it. |

## Failure matrix

| Condition | Preview/preflight result | Repair |
| --- | --- | --- |
| Empty execute mapping | `TASK: execute route blocked: <level> execute profile mapping is empty` | Configure `automation.model_levels.<level>.execute`. |
| Empty review mapping | `TASK: review route blocked: <level> review profile mapping is empty` | Configure `automation.model_levels.<level>.review`. |
| Unknown profile | Names the unknown profile and lane. | Define the profile or select a configured profile. |
| Disabled profile | Names the disabled profile and retains the resolver hint. | Enable the profile or select another configured profile. |
| Ineligible profile | Names the profile and requested level. | Add the level to profile eligibility or choose an eligible profile. |
| Explicit override | The lane override is selected first and `source` identifies task frontmatter. | Correct or remove the override if it is no longer intended. The legacy field cannot silently win. |
| Malformed/missing `WORKFLOW.md` | Every member receives execute and review route blockers naming workflow resolution and the workflow repair action. | Repair `WORKFLOW.md`, then rerun preflight. |
| Mapping removed between preview and start | Final Start/Execute route read refuses before arming or queuing. | Restore the mapping and regenerate/review the current plan. |

Route checks are read only. Human gates remain human actions with an owner and
action; they never become an LLM route or a fourth model tier. A task can be
held for inspection while its route is unresolved, but no start boundary may
turn that unresolved state into a worker or reviewer attempt.

## Verification

Focused checks are named with the ticket prefix:

```text
go test ./cmd/tusker -run TestTaskAuthoringPreflight -count=1
```

The focused cases cover per-member review failures, both lane checks at the
Serve wave queue boundary, malformed-workflow diagnostics, and escaped pipes
in verification projections. Existing resolver tests cover explicit override
precedence, semantic roles and unknown profiles. The shared checkout contains
unrelated in-flight authoring, runner, daemon, ACP and UI changes; a complete
package result must therefore be reported with its matched tests and first
unrelated failure rather than treated as route qualification.

Validation in this checkout: the focused task-authoring/admission/identity,
binding, model-level and demo fixture suite passes, as do `git diff --check`
and a production candidate build. No daemon, provider or dispatcher was
launched by these checks.
