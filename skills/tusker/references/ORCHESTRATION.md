# Orchestration

## Plan First

Before constructing an invocation, an orchestrator reads `tusker capabilities
--json`. The versioned, read-only manifest is the installed binary's authority
for supported commands, schemas, runner adapters, optional capabilities, and
deprecated replacements. It is not a runner-health check and does not inspect
or change project state.

```bash
tusker automation plan <TASK-ID> --json
```

The plan is the stable control-plane answer for daemon pickup, manual dispatch, Obsidian views, and future UI. It should be trusted over ad hoc inspection.

Planning is read-only. A human-opened Codex or Claude session executes direct
work itself and never invokes dispatch or starts nested model processes. Only
the independently running resident daemon may turn an eligible plan into a
background worker, and only when project automation is enabled.

For every multi-unit implementation, author a context-bound
`tusker.delivery-plan/v2` DAG: requirements, tasks, and gates use stable source
keys; dependencies describe the hard and soft edges. A lone task is still a
wave of one. Dependencies unlock the runnable frontier, so independent work
can advance without inventing a serial Markdown checklist. Tusker allocates
all final IDs during held import. Use Tusker CLI lifecycle commands for task,
proof, gate, and wave changes; never hand-edit Markdown status fields.

## Reconciliation and activity

Canonical task, proof, gate, evidence, wave, and closeout mutations go through
the Tusker CLI. After a successful write, the CLI sends a best-effort targeted
notification for the affected project; bursts coalesce before one reconcile.
The resident daemon keeps a separate bounded safety cadence for raw editor
changes and crash recovery. That cadence backs off independently for idle
projects and resets immediately when the CLI mutates a project or the Serve UI
attends/refreshes it. A project with live or retry-sensitive runtime work stays
hot enough for lease and recovery guarantees.

Do not lower a global poll interval to make one project responsive. Use CLI
mutations or targeted UI refresh. Timed polling is fallback correctness, not the
normal control plane.

## Plan Shape

```json
{
  "decision": "dispatch",
  "task": "APP-T-0001",
  "lane": "execute",
  "runner": "codex_exec",
  "workspace": "...",
  "blockers": [],
  "required_reads": ["SKILL.md", "work/tasks/APP-T-0001.md", "knowledge/domains/project/INDEX.md", "knowledge/domains/project/CANON.md"]
}
```

## Runner Rule

Implementation runners return normalized results: summary,
patch/diff/artifacts, verification rows, questions, usage, and outcome.
Independent reviewer runners are read-only and return exactly one immutable
`pass`, `changes_requested`, or `blocked` result with acceptance coverage and
proof/gate fingerprints. Reviewers never edit implementation, change task or
gate state, merge, land, close, or move refs; deterministic Tusker handlers
consume the result.

## Runner Discovery and Route Preview

`tusker runner catalog --json` observes locally available harness capabilities.
`tusker runner profiles --json` previews an additive semantic-profile bootstrap;
`--write` is the one explicit configuration write and preserves existing policy.
It still cannot enable automation. `tusker runner route <TASK-ID> --lane
execute|review --json` is read-only and reports the profile dispatch would use,
its harness/model/effort, source/rule, semantic role, precedence, and blockers.

Tasks may use model-neutral `complexity: routine|standard|complex|frontier`.
Route precedence is task `runner_profile`, first routing rule, lane profile,
semantic role, then project/built-in default. Missing semantic profiles block
routing; Tusker never substitutes a different model silently. Catalog, profile
preview, and route preview are inert: they do not claim, dispatch, arm, enable
automation, or move refs.

## Browser-backed ChatGPT Rule

Treat browser-backed ChatGPT as a reasoning worker and artifact producer. It can return patches, analysis, attachments, and suggested verification. Tusker imports the normalized result and local tools verify it. Browser sessions must not become the durable state machine.

## Fanout Rule

Fanout is opt-in. A parent task must explicitly allow child work. Invisible subagents are not acceptable proof. Child work should create child tasks, proposals, or bounded artifacts.
