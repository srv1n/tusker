---
title: "Tusker Cheat Sheet"
type: note
created_at: "{{date}}"
updated_at: "{{date}}"
tags: [cheatsheet]
---

# Tusker Cheat Sheet

## Task flow

```text
idea -> backlog -> ready -> review -> done
                  ^          |
                  |          v
                rework <-----+

any nonterminal state -> cancelled | superseded
```

Runtime `claimed/running` is not task `status`. A task is agent-runnable only when:

```text
status in ready|rework
readiness == ready
next_owner == agent or agent:<name>
agent_action != stop_until_human_response
```

## Human-wait stop

When only human/external blockers remain:

```yaml
status: review
readiness: held
next_owner: human:<name>
agent_action: stop_until_human_response
```

Agents do no more tool work in this state.

## Common commands

```bash
tusker list --type epic
tusker search "<term>" --type task
tusker show <TASK-ID> --capsule
tusker proof status <TASK-ID> --json
tusker verify add <TASK-ID> --covers A1 --check "<check>" --result pass
tusker finish <TASK-ID> --request-review --summary "<proof map>"
tusker validate --json
tusker closeout status <TASK-ID> --json  # when supported
```

## What to read

- `references/QUICK_MODE.md` for routine log/resume/close.
- `references/CLOSEOUT_PROTOCOL.md` for human gates and loop prevention.
- `references/WORKFLOW.md` for lifecycle state.
- `references/RISK_AND_EVIDENCE.md` for proof expectations.
