---
schema: tusker.task/v5
id: {{ID}}
title: {{title}}
type: task
kind: bug
epic: {{ACR}}
status: intake
priority: p1
risk: medium
domains:
  - {{domain}}
doc_nodes:
  - {{doc_node}}
created: {{date}}
updated: {{date}}
---

# {{ID}} · {{title}}

## Failure claim

One sentence describing the defect.

## Impact

Who is affected? How bad is it?

## Reproduction contract

Steps:

1.
2.
3.

Expected:

Observed:

## Before evidence

Attach at least one:

- failing test
- screenshot
- video
- log excerpt
- trace
- exact command output

No before evidence, no verified bug fix.

## Acceptance contract

| AC | What must be true | Verification | Deliverables | Doc nodes |
|---:|---|---|---|---|
| 1 |  |  |  |  |

## Root cause hypothesis

-

## Fix constraints

The fix must:

-

The fix must not:

-

## Regression contract

What prevents this bug from returning?

-

---

## Agent packet

### Workpad

### Evidence packet

#### Before

-

#### After

-

#### Regression proof

-

### Knowledge delta

Use `none` if no durable knowledge changed.

| Change type | Topic | Before | After | Audience | Target doc nodes | Mode impact | Status |
|---|---|---|---|---|---|---|---|

### Verification log

### Work log
