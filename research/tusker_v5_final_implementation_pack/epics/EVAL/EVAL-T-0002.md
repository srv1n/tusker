---
schema: tusker.task/v5
id: EVAL-T-0002
title: Implement docs eval runner skeleton
type: task
kind: feature
epic: EVAL
status: ready
priority: p2
risk: medium
domains:
  - evals
doc_nodes:
  - tusker/evals
created: 2026-04-29
updated: 2026-04-29
depends_on:
  - EVAL-T-0001
  - CLI-T-0001
---

# EVAL-T-0002 · Implement docs eval runner skeleton

## Outcome

Tusker can run or dry-run docs evals and collect evidence without locking into one provider.

## Acceptance contract

| AC | What must be true | Verification | Deliverables | Doc nodes |
|---:|---|---|---|---|
| 1 | Runner can list evals, show prompt/fixture/doc_nodes, and record manual pass/fail. | CLI fixture | runner output | tusker/evals |
| 2 | Provider execution is optional and behind config. | config test | test output | tusker/evals |
| 3 | Eval results write events and generated index updates. | event/index fixture | event log output | tusker/evals |

## Scope

### In

- Implement manual/dry-run runner first.
- Reserve provider adapter interface.
- Record events.

### Out

- Unrelated behavior changes outside this task.
- Broad refactors not needed to satisfy the acceptance contract.
- Marking the task done without evidence and docs impact resolution.

### Non-goals

- Perfecting downstream tasks in this epic.
- Adding daemon automation unless explicitly listed.

## Context and canon

Read in order:

1. `SPEC/TUSKER_V5_FINAL_SPEC.md`
2. `SPEC/IMPLEMENTATION_SEQUENCE.md`
3. `SPEC/MIGRATION_PLAN.md`
4. This task file

## Constraints and stop conditions

The agent must preserve:

- Plain Markdown readability in Obsidian.
- Current task status in task frontmatter.
- Generated indexes as rebuildable cache.
- Runtime/audit data outside human-facing frontmatter.

The agent must not change:

- The v5 hierarchy: Epic → Task; docs as durable pages.
- The rule that Markdoc is optional and never canonical for task contracts.

Stop and ask for human input if:

- Existing repo behavior contradicts the v5 spec.
- Existing user data could be destructively migrated.
- A domain or doc_node is needed but not present in docs-map.

## Implementation notes

- Implement manual/dry-run runner first.
- Reserve provider adapter interface.
- Record events.

## Verification contract

Required commands:

```bash
go test ./...
tusker validate --fixtures
```

Required artifacts:

- test output
- diff summary
- validator output

## Review focus

Human reviewer should inspect:

- Whether this implements the v5 contract, not just a rename.
- Whether Obsidian readability is preserved.
- Whether docs and skill references were updated when behavior changed.

Verifier agent should inspect:

- Acceptance coverage.
- Docs-map/doc_node correctness.
- Validator, migration, or CLI output.

---

## Agent packet

### Workpad

Plan:

- Read the spec and current repo shape.
- Implement the smallest coherent patch for this task.
- Run the required checks.
- Fill evidence, knowledge delta, and verification sections.

Assumptions:

- The current repo is still close to the public README shape: Story/Bug/Doc files under epic folders.
- Existing tests may be sparse; add focused fixtures where needed.

Progress:

- [ ] Implementation complete
- [ ] Tests/checks run
- [ ] Evidence packet filled
- [ ] Knowledge delta filled
- [ ] Verification log updated

Blockers:

- None yet.

### Evidence packet

#### Summary claim

> This task satisfies the acceptance contract because...

#### Changed surfaces

-

#### Commands run

| Command | Result | Notes |
|---|---:|---|
|  |  |  |

#### Artifacts

-

#### Acceptance proof

| AC | Evidence |
|---:|---|
| 1 |  |
| 2 |  |
| 3 |  |

#### Risks and residuals

-

### Knowledge delta

| Change type | Topic | Before | After | Audience | Target doc nodes | Mode impact | Status |
|---|---|---|---|---|---|---|---|
| changed | evals | Docs evals were conceptual. | Docs evals have runnable/manual skeleton. | developer, agent | tusker/evals | reference, how-to | pending |

### Verification log

Worker fills Evidence Packet. Verifier fills this section.

-

### Work log

- 2026-04-29 — tusker — task created from v5 implementation pack.
