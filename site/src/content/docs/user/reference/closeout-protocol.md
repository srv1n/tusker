---
title: "Closeout Protocol"
description: "This protocol prevents token-burning loops when machine work is complete but the task still has human, reviewer, or external gates."
tusker:
  audience: "user"
  publish_path: "user/reference/closeout-protocol"
  route: "/user/reference/closeout-protocol/"
  source_kind: "repo_doc"
  source_path: "skill/references/CLOSEOUT_PROTOCOL.md"
  summary: "This protocol prevents token-burning loops when machine work is complete but the task still has human, reviewer, or external gates."
  tags:
    - "reference"
    - "closeout"
  updated: "2026-05-20"
---

# Closeout Protocol

This protocol prevents token-burning loops when machine work is complete but the task still has human, reviewer, or external gates.

## Core invariant

An agent may continue only while there is machine-owned work left.

```text
machine gaps remain       -> continue narrowly
reviewer gaps remain      -> request or wait for reviewer
human/external gaps only  -> emit packet/checkpoint once, then stop
unchanged clean state     -> do not revalidate
```

## Preferred command flow

When supported by the installed CLI:

```bash
tusker closeout status <TASK-ID> --json
```

If this returns `agent_action: stop_until_human_response` or `stop_until_external_response`, perform no tool work.

When implementation is done and closeout is needed:

```bash
tusker closeout <TASK-ID> --emit-packet --validate "<command>"
```

Expected JSON shape:

```json
{
  "ok": true,
  "task": "SAM-T-0008",
  "closeout_state": "machine_complete_waiting_for_human",
  "agent_action": "stop_until_human_response",
  "validation": {
    "result": "pass",
    "cached": false,
    "fingerprint": "sha256:..."
  },
  "machine_missing": [],
  "reviewer_missing": [],
  "human_missing": ["proof_required:human_signoff"],
  "external_missing": [],
  "open_human_gates": ["SAM-G-0001"],
  "review_packets": ["tusker/_generated/packets/SAM-T-0008.reviewer.md"]
}
```

Older CLI fallback:

```bash
tusker proof status <TASK-ID>
tusker show <TASK-ID> --capsule
tusker search <TASK-ID> --type gate --status open --json
```

Then classify gaps manually using the rules below. Do not retry unsupported commands.

## Gap ownership

Classify every remaining gap before deciding to continue.

| Gap kind | Owner | Agent behavior |
|---|---|---|
| failing focused test caused by current change | machine | fix or summarize after budget |
| missing focused/broad test required by proof mode | machine | add/run the smallest useful check |
| missing docs delta for changed durable understanding | machine | update docs/knowledge once |
| missing evidence card/artifact required by proof mode | machine if artifact is locally producible | create one concise proof object |
| human signoff | human | stop after packet |
| manual smoke requiring device/env/judgement | human/external unless explicitly agent-owned | stop after gate/packet |
| product/security/release approval | human | stop after gate/packet |
| credentials or unavailable external service | human/external | create/update gate, then stop |
| reviewer recommendation for high/critical task | reviewer then human | request review once, then stop |
| CI still running | external | record status/link, then stop |

Ambiguous manual proof is not agent-owned. Convert it to a gate with owner, action, and verification.

## Terminal human-wait state

Use this current-compatible state when machine work is complete and the remaining blockers are human-owned:

```yaml
status: review
readiness: held
machine_status: complete
human_status: pending
closeout_status: machine_complete_waiting_for_human
next_owner: human:<name>
next_source: gates
next_ref: SAM-G-0001,SAM-G-0003
next_action: Accept, waive, or reject the listed gates.
agent_action: stop_until_human_response
```

This state is terminal for agents. The only allowed response is a concise status summary.

## Validation cache rule

A clean validation result is reusable while the state fingerprint is unchanged.

A fingerprint should include, when available:

```text
git HEAD
git diff / dirty state
task state_rev or frontmatter hash
relevant gate/evidence hashes
validation command
tusker version
```

Rules:

- same fingerprint + previous pass = do not re-run validation;
- changed fingerprint = run validation once;
- human explicitly asks for fresh validation = run once and report that it was requested;
- terminal human-wait + unchanged fingerprint = zero validation runs.

## Loop guard

Compute the mental loop key:

```text
TASK-ID + state_fingerprint + command + normalized_result
```

If the same clean result appears twice without a state change, stop. The second run produced no new information.

## Subagent/audit guard

Subagents are for one deep audit, not recurring anxiety.

Allowed:

- one audit after a task first claims machine-complete;
- one targeted audit after a material rework state change.

Forbidden:

- spawning subagents while `agent_action` is stop;
- repeating broad audits because human gates are still open;
- using subagents to compensate for an unclear closeout state instead of creating a gate.

## Final human-wait response

Use this response shape:

```text
Machine work is complete.

Last clean validation:
- <command>: PASS
- errors/warnings: <count>
- fingerprint/state_rev: <value if known>

Review packet:
- <path>

Remaining human/external gates:
- <gate-id>: <owner> must <action>

Agent action: stop_until_human_response.
No further validation was run because the state has not changed.
```
