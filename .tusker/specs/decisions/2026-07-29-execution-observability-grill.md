---
title: "Decision log: execution observability and direct-agent identity"
subject: execution-observability-grill
keywords: [executions, names, direct agents, subagents, tracking]
part_of: execution-observability
status: canonical
created: 2026-07-29
read_when: "You want to know why execution names, immutable IDs, optional ticket bindings, and the unbound inbox were chosen."
skip_when: "You only need the resulting contract — read [[execution-observability]]."
decides_for: ".tusker/specs/execution-observability.md"
---

# Decision log: execution observability

This grill refined the standing [[software-factory]] decision that work
registers in Tusker no matter who drives it. It followed a code-level comparison
between Tusker and Paseo, plus checks of the installed Codex and Claude command
surfaces.

## D1 — What should an operator follow?

**Asked:** Should the main customer story be an execution graph rooted in one
user intent, where managed and provider-native children remain visible, or
should Tusker stay task-centric and treat children as secondary diagnostics?

**Operator said:** The concrete pain is a wave started through Codex or Claude
where one CLI job creates several sub-agents. The missing capability is knowing
what was spawned, how to name it, how to track it, and how it relates to the
wave of tickets.

**Locked:** The operator view must preserve wave, task, attempt, session, and
child relationships instead of flattening them into one retry list. Tasks
remain the delivery contract; executions become the observable runtime graph.

## D2 — Are provider-native children first-class even when read-only?

**Asked:** Should every provider-native child appear in Tusker even when Tusker
cannot stop or steer it independently?

**Operator said:** The requested plan must cover the sub-agents created below a
top-level Codex or Claude execution because that is the visibility missing
today.

**Locked:** Provider-native children are first-class observable records.
Controls remain read-only unless a provider adapter proves independent control.
Tusker never invents a lease or process for a provider-owned child.

## D3 — What is a name, and what is identity?

**Asked:** Should every execution receive a mandatory generated ID, an optional
human display name, and optional task/wave bindings, with direct unbound work
kept in an inbox and denied delivery authority until attached?

**Options:** Provider-native names only; ticket IDs as names; or Tusker-owned
identity plus separate human/provider labels (recommended).

**Operator said:** Agreed and asked to lock the recommendation in.

**Locked:**

- Tusker generates the immutable execution ID.
- Display name, task ID, wave ID, provider session/task ID, provider child ID,
  and reusable agent type remain separate fields.
- Direct exploratory work may register unbound.
- Unbound work appears in an explicit inbox and cannot provide proof, request
  review, land, or close a ticket until an audited attachment succeeds.

## D4 — Does direct work remain legitimate?

**Asked earlier in the factory grill:** Who orchestrates day to day?

**Operator said then:** Manual Codex and Claude work must remain possible while
the UI matures; the task must show that it was picked up outside the daemon so
the UI stays truthful.

**Reaffirmed here:** The operator may start Codex, Codex Cloud, or Claude work
outside the resident daemon. Native provider names and IDs help, but Tusker
must own the durable mapping.

**Locked:** Direct work uses the same observation model without becoming daemon
dispatch. The daemon remains the only background dispatcher, and direct
registration grants no wave, proof, landing, release, or spending authority.

## Facts established during the grill

- Claude Code supports a top-level display name and exposes session ID,
  subagent ID, agent type, transcript, and start/stop hook events.
- Codex supports named reusable custom-agent roles and exposes top-level thread
  IDs plus child agent IDs/types through events and hooks, but its direct
  `exec` surface has no equivalent top-level display-name flag.
- Codex Cloud supplies a durable cloud task ID but no equivalent name flag in
  the installed CLI.
- Tusker already stores several attempt relationship fields but its principal
  API/UI drops them, and nested provider IDs are intentionally excluded from
  parent resume identity without a parallel child projection.
- Tusker's transactional claims, generation fencing, detached wrapper, and
  process identity remain stronger execution authority than the compared Paseo
  snapshot and must not be replaced.
