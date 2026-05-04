---
id: "{{TASK_ID}}"
type: "task"
title: "{{title}}"
epic: "{{EPIC_ID}}"
status: "draft"
owner: "{{owner}}"
risk: "medium"         # low | medium | high | critical
priority: "p2"         # p0 | p1 | p2 | p3
size: "m"              # xs | s | m | l | xl
domains: []
doc_nodes: []
depends_on: []
created: "{{date}}"
updated: "{{date}}"
---

# {{TASK_ID}} · {{title}}

## Outcome

Describe the finished state in plain language.

## Problem

What is missing, broken, or painful right now.

## Scope

In:
- 

Out:
- 

## Acceptance criteria

These are the verdict conditions. Keep them user-observable and testable.

- [ ]
- [ ]

## Deliverables

These are the proof artifacts required to close the task.

- Demo:
- Screenshots:
- Tests:
- Logs / traces:
- Doc update:
- Rollback note:

## Canon to read first

List only the sources that matter, in order.

- [[{{EPIC_ID}}]]
- 

## System anchors

Files, symbols, routes, jobs, tables, or services the agent should start from.

- 

## Human checkpoints

The agent must stop and ask for human input if any of these happen.

- Secrets or external credentials needed
- Irreversible migration or destructive action
- Scope needs to expand beyond this task
- 

## Execution plan

1. 
2. 
3. 

## Verification

How each acceptance criterion will be checked.

- 

## Docs impact

```yaml
action: update        # none | update | create | review
targets:
  - 
notes:
  - 
```

## Evidence

Attach or link the actual proof here after execution.

- 

---

## Agent packet

### Constraints

- Do not widen scope without updating this task.
- Prefer existing patterns and shared utilities.
- Record anything surprising in the work log.

### Work log

- {{date}} — task created
