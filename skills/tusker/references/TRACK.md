# Track

Inspect existing work with `tusker show <TASK-ID> --capsule`; use
`tusker packet <TASK-ID> --for agent` when implementing. Read the complete
acceptance contract, then only the source and documentation it needs.

## Author tasks and waves

One bounded outcome is one task. Convert the supplied spec; interviewing is
outside this skill. Each task needs outcome, scope/non-goals, relevant source
paths and decisions, observable acceptance IDs, verification, dependencies,
review expectations, and a configured work level. Reference canonical specs
instead of copying them. Preserve enough guidance to execute without chat history.

Use tags for cross-cutting groups such as auth. Epics are legacy compatibility,
not required product structure. Check `tusker new task --help` for supported
fields; source scope includes `--owned-paths cmd/auth.go,internal/auth` and
`--generated-outputs`. Never invent flags for proposed schema fields.

For batches, use the delivery plan schema exposed by `tusker delivery --help`.
Map every accepted requirement to task acceptance or an explicit deferral.
Dependencies express required outputs/contracts; independent ownership can run
in parallel. Give each wave a one-to-two-line expected user capability, distinct
from its eventual result. Use a supported description if no dedicated field exists.

```sh
tusker delivery import --plan <plan.yaml> --dry-run --json
tusker delivery import --plan <plan.yaml>
tusker wave preflight <WAVE-ID>
```

Re-import only eligible open/disarmed backlog/held plans, preserving `scope`
and `source_key`. Progressed contracts may refuse amendments: report the exact
refusal and requested change. `tusker update` installs skills, not task edits.
Task creation/preflight does not authorize execution.

## Implement and close

Interactive ownership uses `tusker work start`, `status`, `submit`, `review`,
and `release`; inspect `tusker work --help` and obey the returned workspace and
claim. Dispatched workers retain their existing claim. Record implementation,
verification and review separately; submit is not completion. Follow
`tusker closeout status <TASK-ID> --json` for the next supported transition.

Proof needs executor-recorded PASS/FAIL bound to the reviewed material:
`tusker verify add <TASK-ID> --covers A1 --check "command: go test ./..." --result pending`.
Choose the smallest checks covering acceptance. Attach available screenshots or
reports; require human review only for an explicit human gate. Keep transient
logs in CLI-managed scratch when needed; use the harness scratchpad otherwise.
Never manufacture proof or rewrite lifecycle fields to clear a refusal.
