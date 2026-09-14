# Track

Inspect work with `tusker show <TASK-ID> --capsule`; implement from
`tusker packet <TASK-ID> --for agent`. Read its contract, then only required
source and documentation.

## Author tasks and waves

One bounded outcome is one task; one atomic wave request is one batch. Read
`HANDOFF.md` before authoring, amending or reviewing. Set `work_level` for
each new agent task: `light`, `standard`, or `demanding`, chosen by remaining
worker judgment. Review inherits unless `review_level` plus `review_reason`
override. Human work uses named gates; configured profiles own model/harness
choices.

The task body is the implementation contract. Author it directly:

```sh
tusker new task --title <title> --work-level <level> --body-file <path|->
tusker task update <TASK-ID> --if-revision <state_rev> [--body-file ...] --by <actor> --json
tusker wave create --file <request.yaml> --request-key <stable-key> --json
tusker wave review <WAVE-ID> --json
tusker runner route <TASK-ID> --lane execute --json
tusker runner route <TASK-ID> --lane review --json
```

`--epic`, `--spec-refs`, `--dependencies`, `--owned-paths`,
`--generated-outputs`, and contact fields are optional; every supplied
spec ref must resolve. Normal authoring omits priority, size, and risk
metadata. `task update` mutates only mutable authoring fields under the
material lock and preserves identity, history, and proof lineage; a material
change on a done or review task reopens it as rework.

Creation is inert. Execution starts only on explicit authority:

```sh
tusker task start <TASK-ID> --mode interactive --by <agent> --current-workspace --json
tusker task start <TASK-ID> --mode background --by <actor> --json
tusker wave start <WAVE-ID> --mode background --by human:<name>|operator:<name> --json
tusker wave pause <WAVE-ID> --by human:<name>|operator:<name> --json
tusker wave resume <WAVE-ID> --by human:<name>|operator:<name> --json
```

One wave Start authorizes the exact current material; daemon polling advances
each dependency frontier automatically. Pause blocks new wave admissions while
admitted attempts finish; resume restores the unchanged authorization and
refuses drifted material. An explicit task Start inside a paused wave stays
task-scoped and leaves the wave paused.

Inspect JSON blockers, not just exit status. Verify classification, ownership
and contacts in generated packets/capsules. Held drafts may retain setup gaps;
never describe them as execution-ready.

## Implement and close

Use `tusker work start`, `status`, `submit`, `review`, and `release`; inspect
`tusker work --help` and obey its claim. For current-conversation self
implementation, use `tusker work start <TASK-ID> --by <agent> --source codex
--current-workspace --json` (use `--source claude` for Claude); this is
execute-only and same-native review is refused. Dispatched workers retain
their claim. Self implementation requires an explicit authorized claim and
independent review. Submission is not completion; follow
`tusker closeout status <TASK-ID> --json`.

Add planned checks with `tusker verify add <TASK-ID> --covers A1 --check
"command: go test ./..." --result pending`. Only the executor records PASS/FAIL.
Choose the smallest checks covering acceptance, attach required evidence, and reserve human
review for explicit gates. Use CLI-managed scratch for transient logs. Never
manufacture proof or rewrite lifecycle fields to clear a refusal.
