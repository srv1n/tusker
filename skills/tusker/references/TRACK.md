# Track

Inspect work with `tusker show <TASK-ID> --capsule`; implement from
`tusker show <TASK-ID> --packet agent`. Read its contract, then only required
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
tusker edit <TASK-ID> --if-revision <state_rev> [--body-file ...] --by <actor> --json
tusker new wave --file <request.yaml> --request-key <stable-key> --json
tusker show <WAVE-ID> --check --json
tusker show <TASK-ID> --route execute --json
```

`--epic`, `--spec-refs`, `--dependencies`, `--owned-paths`,
`--generated-outputs`, and contact fields are optional; every supplied
spec ref must resolve. Normal authoring omits priority, size, and risk
metadata. `tusker edit` changes only mutable authoring fields under the
material lock and preserves identity, history, and proof lineage; a material
change on a done or review task reopens it as rework.

Creation is inert. Execution starts only on explicit authority:

```sh
tusker start <TASK-ID> --by <agent> --current-workspace --json
tusker start <TASK-ID> --background --by <actor> --json
tusker start <WAVE-ID> --by human:<name>|operator:<name> --json
tusker stop <WAVE-ID> --by human:<name>|operator:<name> --json
```

Starting a wave authorizes its exact current material; daemon polling advances
each dependency frontier automatically. Stopping a wave pauses new admissions
while admitted attempts finish. Starting a paused wave resumes the unchanged
authorization and refuses drifted material. An explicit task start inside a
paused wave stays task-scoped and leaves the wave paused.

The author runs `tusker show <WAVE-ID> --check` before handoff: it checks the
whole DAG and every member contract, and exits nonzero unless the wave can
start. Inspect its JSON blockers and generated packets for semantic proof
quality, classification, ownership, and contacts. For a standalone task, use
`tusker check --only validate --path work/tasks/<ID>.md --json` and inspect
warnings too; `ok:true` is not a dispatchability verdict. Held drafts may
retain setup gaps; never describe them as execution-ready.

## Implement and finish

Four verbs carry the lifecycle: `start`, `done`, `review`, `stop`. For
current-conversation self implementation, run `tusker start <TASK-ID> --by
<agent> --source codex --current-workspace --json` (`--source claude` for
Claude); rerunning it renews your lease. This is execute-only and same-native
review is refused. Dispatched workers keep their claim. Self implementation
needs an explicit authorized claim and independent review.

When the work is committed, run `tusker done <TASK-ID> --by <agent>`. It
submits your live session, or reruns the task's proof when no claim exists.
`tusker done --since <ref>` or `--wave <W>` records many tasks at once. Never
claim a task afterwards just to record it. Submission is not completion; follow
`tusker show <TASK-ID> --closeout --json`.

Add planned checks with `tusker edit <TASK-ID> --covers A1 --check
"command: go test ./..." --result pending`. Escape `|` as `\|` inside a check
command. Only the executor records PASS/FAIL. Choose the smallest checks
covering acceptance, attach required evidence, and reserve human review for
explicit gates. Use CLI-managed scratch for transient logs and summaries. Keep
large build or test output in the system temp directory: scratch has a 200MiB
budget and idle entries are evicted. Never manufacture proof or rewrite
lifecycle fields to clear a refusal.
