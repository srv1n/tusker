---
title: "Agent runbook: <task>"
mode: how-to
reader: agent
source_of_truth: "<path or URL>"
requires_confirmation: false
last_reviewed: "YYYY-MM-DD"
stale_when: []
---

# Agent runbook: <task>

## Scope

Use this runbook to <task>. Do not use it for <excluded task>.

## Preconditions

- <precondition>
- <permission/tool/version>

## Inputs

| Name | Type | Required | Validation |
|---|---|---:|---|
| <name> | <type> | <yes/no> | <validation> |

## Procedure

### 1. Validate starting state

Command/API/check:

```bash
<command>
```

Expected state:

```text
<state>
```

### 2. Execute change

```bash
<command>
```

### 3. Validate final state

```bash
<command>
```

Expected state:

```text
<state>
```

## Rollback

Use rollback only when <condition>.

```bash
<rollback command>
```

## Failure modes

| Failure | Detection | Agent action |
|---|---|---|
| <failure> | <check> | <action> |

## Escalate or ask for confirmation when

- <condition>
