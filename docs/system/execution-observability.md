---
title: "Execution observability"
subject: execution-observability-system
keywords: [executions, direct work, provider children, lineage, timeline]
part_of: overview
status: canonical
read_when: "You need to register, inspect, bind, recover, or operate direct or daemon execution visibility."
skip_when: "You only need task lifecycle or generic daemon dispatch rules."
---

# Execution observability

Tusker makes execution visible without confusing visibility with authority. An execution is one durable, observable strand: a wave authorization root, a leased task attempt, a direct Codex or Claude session, or a provider-native child. SQLite is the ownership authority. Provider hooks, JSON streams, and cloud status are untrusted observations: they can add idempotent facts, never claim, bind, prove, review, land, arm, release, or spend.

The product contract is [the execution-observability spec](../../.tusker/specs/execution-observability.md). This page documents the implemented operator surface.

## Identity, names, and relationships

Every root and node has an immutable Tusker execution ID. A display name is optional and audited. Task/wave bindings, provider session IDs, provider child handles, and reusable agent types are distinct fields. Search and UI labels may combine them for convenience; authority checks never do.

There is one root for every direct invocation and for each wave-authorization generation. A task attempt is a managed child with its own admission, lease, workspace, controls, and outcome. A provider-native child is a first-class observable node but remains provider-owned unless the adapter proves an independent capability. It never gets a fake Tusker lease. Retry and resume edges continue work; they are not concurrent children.

```text
wave authorization root
└── managed task attempt (leased by Tusker)
    ├── retry/resume (continuation)
    └── provider-native child (visible, provider-owned)
```

## Direct work and binding

Use the execution CLI to allocate identity before a provider launch, correlate an existing provider session, give it a name, inspect it, or find unbound work. The operations are authority-neutral.

```bash
tusker execution register --source direct_codex --provider codex --name "Lease audit" --json
tusker execution attach --id exec_… --provider codex --provider-session-id THREAD_ID --json
tusker execution rename --id exec_… --name "Lease recovery audit" --json
tusker execution inbox --json
tusker execution list --name lease --provider-id THREAD_ID --json
tusker execution show --id exec_… --json
```

`bind` derives the wave from the task's canonical membership and rejects an explicit mismatch or a conflicting live owner. `detach` and `rebind` create a new generation boundary. Pre-binding observations remain visible but are proof-ineligible forever; do not use a bind operation to launder exploratory work into delivery evidence.

```bash
tusker execution bind --id exec_… --task ORC-T-0000 --json
tusker execution detach --id exec_… --json
```

`execution launch` reports local process facts only for direct local work and refuses nested agent sessions. A `codex_cloud` launch has no local PID and is observation-only. `execution cancel` requests cancellation only when the adapter has proved it for that target; provider acknowledgement and operating system settlement are separate facts.

## Graph, lifecycle, and timeline

`tusker execution list` is a relationship-complete graph query. It searches by execution/root/parent, task, wave, source, provider/provider ID, agent type, binding, lifecycle, name, and child attention. Its graph nodes keep the dimensions separate:

| Dimension | Meaning |
|---|---|
| Delivery | Unbound/bound and proof eligibility |
| Admission | Registration and claim/start progress |
| Process | Local process evidence, including unknown or settlement |
| Provider | Provider-reported state, not a lease outcome |
| Outcome | Succeeded, failed, canceled, interrupted, lost, or none |
| Session | Attached, resumable-detached, or non-resumable closed |
| Child attention | Active, failed, or needs-attention child summary |

Serve exposes the same projection at `GET /api/executions`, the unbound inbox at `GET /api/executions/inbox`, and the convergent timeline at `GET /api/executions/timeline`. The Operations → Executions screen is a graph-specific drill-down, not a second generic operations shell. It shows managed versus provider-owned nodes, lifecycle facts, guarded bind/rename, capability-aware controls, and active/failed/attention counts.

Timeline notifications improve latency only. Correctness comes from the authoritative fetch API. Rows retain source execution, epoch, and sequence; clients advance their checkpoint from fetch results, not from subscriptions. Fetch answers explicitly with `reset`, `gap`, `stale_cursor`, `has_older`, and `has_newer`. On any reset/gap/stale result, discard the local checkpoint and fetch the authoritative tail until the returned checkpoint reaches the committed tail.

## Provider coverage and limits

| Source | Identity and child facts | What not to infer |
|---|---|---|
| Codex local/app | Top-level thread correlation and child observations from structured events | A hook cannot establish ownership or delivery authority. |
| Codex Cloud | Cloud task ID/status and enumerated children from authoritative cloud fetch | Cloud status is not a local PID, heartbeat, or OS-death fact. |
| Claude Code | Top-level session metadata plus `SubagentStart`/`SubagentStop` observations | A child ID never replaces the parent's resumable session. Missing start/stop data is partial visibility, not child termination. |

Adapters accept bounded, replay-safe envelopes. Duplicates are harmless; out-of-order or regressing provider facts become degraded visibility that calls for authoritative fetch. A terminal parent never forges a child terminal state. Controls are hidden or unavailable when a capability is unknown, stale, or false; provider-native child control is never invented.

## Migration, recovery, and compatibility

The legacy attempt lineage is backfilled into immutable execution records and typed edges while keeping existing lease owner/generation and PID/PGID/start time fences authoritative. Backfill is idempotent and validates equivalent existing rows; it does not replace SQLite ownership with provider state.

For migration, restart, stale cursor recovery, binding conflicts, partial children, cancellation settlement, and a bounded disable-observation response, use [the execution-observability runbook](../runbooks/execution-observability.md).

Compatibility handoff: **ORC-T-0046** consumes this graph-specific projection inside its broader operations composition; it must not duplicate the execution screen. **ORC-T-0047** consumes the focused execution reliability fixture for its end-to-end regression; it must not recreate provider/replay coverage.

