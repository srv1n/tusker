# Workflow

## Durable Task Statuses

```text
idea -> backlog -> ready -> review -> done
                    |       ^
                    v       |
                  rework ---+
```

Terminal statuses are `done`, `cancelled`, and `superseded`.

Do not use `active` as a durable task state. `claimed`, `running`, `leased`, and `interrupted` are runtime states on runs, leases, sessions, attempts, and workspaces.

Human-only review becomes `readiness: waiting_on_human` with `next_owner: human:<name>` and `agent_action: stop_until_human_response`.

## Documentation Churn

Low/medium tasks without `doc_nodes` default to `Knowledge delta: None expected.` Do not ask agents for changelog, docs, or canon updates unless the task contract names `doc_nodes`; repeated lessons belong in `tusker feedback promote`, not per-task prose.

## Dispatch Predicate

A task can dispatch only when all are true:

```text
kind = task
status in [ready, rework]
readiness = ready
next_owner = agent or agent:<enabled-runner>
agent_action != stop_until_human_response
no open blocking human gate
no unsatisfied blocking dependency
acceptance is concrete
verification maps to acceptance
proof_mode and proof_required are valid
runner is enabled
no active run already owns the task
concurrency/workspace limits allow dispatch
```

Use `tusker automation plan <task> --json` instead of guessing.

## Design Traceability

For non-trivial design work, run the interactive planning session first, then
write the durable spec or design note under `docs/specs/` or `docs/design/`.
Use Mermaid diagrams in the spec when state flow, routing, or ownership is
easier to inspect visually than in prose.

Link the plan to execution in both directions:

- Add `spec_refs` to each governing epic or task. Values are repo-relative spec
  paths such as `docs/specs/10-runtime.md`, repo-relative decision paths, or V7
  decision ids such as `RUN-D-0001`.
- Add a `## Work streams` section to the spec or decision, linking the epics and
  tasks that implement it, for example `[[RUN]]` and `[[RUN-T-0004]]`.
- `tusker validate` warns when `spec_refs` points at a missing spec/decision, or
  when a `Work streams` section names an unknown epic/task id.
- `tusker show <TASK-ID> --capsule`, `tusker packet <TASK-ID> --for agent`, and
  `tusker automation plan <TASK-ID> --json` surface `spec_refs` as read-next
  targets so execution reads governing design before code.
