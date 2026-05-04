---
schema: tusker.task/v5
id: EVAL-T-0005
title: Wire docs evals into close gate by risk and doc type
type: task
kind: feature
epic: EVAL
status: ready
priority: p1
risk: high
domains:
  - evals
doc_nodes:
  - tusker/evals
created: 2026-04-29
updated: 2026-04-29
depends_on:
  - EVAL-T-0002
  - DOC-T-0004
  - VAL-T-0002
---

# EVAL-T-0005 · Wire docs evals into close gate by risk and doc type

## Outcome

High-risk docs changes and agent-facing runbooks require eval pass or waiver before task closure.

## Acceptance contract

| AC | What must be true | Verification | Deliverables | Doc nodes |
|---:|---|---|---|---|
| 1 | docs-map nodes can declare required evals. | docs-map validator | validator output | tusker/evals |
| 2 | Close gate requires eval pass/waiver for configured nodes when affected. | close fixture | close output | tusker/evals |
| 3 | Low-risk docs can skip evals unless configured. | fixture | test output | tusker/evals |

## Scope

### In

- Extend docs impact gate.
- Record eval.waived events.
- Document policy.

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

- Extend docs impact gate.
- Record eval.waived events.
- Document policy.

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
| changed | evals | Docs evals could be disconnected from closure. | Docs evals become required where they protect task success. | developer, agent | tusker/evals | reference, how-to | pending |

### Verification log

Worker fills Evidence Packet. Verifier fills this section.

-

### Work log

- 2026-04-29 — tusker — task created from v5 implementation pack.
