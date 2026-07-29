---
capsule:
  what: "The delivery contract for durable names, lineage, provider-native child visibility, and convergent execution timelines."
  use_when:
    - "Planning or implementing execution identity, direct Codex or Claude registration, provider child tracking, or the multi-agent operations view."
  skip_when:
    - "Changing task lifecycle or daemon dispatch without changing execution identity, relationships, provider observations, or operator visibility."
---

# Execution observability

## Product outcome

An operator can follow one named wave, task, or direct model invocation from
its immutable Tusker execution identity through every managed attempt, retry,
resume, and provider-native child. The graph stays truthful after daemon
restart and when work began outside the daemon.

The full product and architecture contract is
[the canonical execution-observability spec](../../.tusker/specs/execution-observability.md).
The operator decisions behind it are in
[the grill log](../../.tusker/specs/decisions/2026-07-29-execution-observability-grill.md).
This document is the bounded delivery intake used to fingerprint and emit the
implementation DAG.

## Requirements

| ID | Outcome |
|---|---|
| R1 | Every direct invocation or wave-authorization generation has one Tusker-generated immutable execution root. Every observed strand has an immutable ID, optional human display name, explicit node kind, and typed retry, resume, fork, managed-child, or provider-child relationships. Existing attempt relationships migrate without losing history or weakening SQLite ownership authority. |
| R2 | Direct Codex, Codex Cloud, and Claude work can register before launch or attach after a provider ID exists, can remain visibly unbound, and can be renamed or bound through audited conflict-checked operations. Unbound or pre-binding history cannot provide proof, review, landing, close, wave, release, or spending authority. |
| R3 | One bounded, idempotent, untrusted provider-event envelope records top-level sessions and provider-native children. Codex local/app/cloud and Claude adapters preserve provider IDs, names or agent types, parentage, status, cursors, and capabilities without minting fake leases or overwriting the parent resume identity. |
| R4 | CLI and Serve expose a relationship-complete execution graph and an authoritative epoch/sequence/cursor timeline. Reconnect, retention, replacement, subscriber loss, late events, and restart return explicit reset, gap, stale-cursor, older, and newer facts rather than silent holes. |
| R5 | Delivery, admission, process, provider, outcome, session, and child-attention state remain separate evidence dimensions. Cancellation reports provider acknowledgement and operating-system settlement separately and only offers controls proved by the adapter. |
| R6 | The operations surface supports search by name, ticket, wave, provider ID, and agent type; shows managed versus provider-owned children, active/failed/attention counts, timeline convergence, and an explicit inbox for unbound direct work without duplicating the generic factory operations product. |
| R7 | Migration, replay, duplicate events, out-of-order events, provider loss, parent exit, process loss, cancellation races, daemon crash/restart, and cross-provider fan-out have deterministic focused proof, followed by canonical system documentation and a compatibility handoff to the broader ORC operations and end-to-end work. |

## Invariants

- SQLite remains the only execution-ownership authority.
- Existing lease owner/generation and PID/PGID/start-time fences remain
  authoritative.
- A provider observation can report facts but cannot claim or bind work,
  satisfy proof, finalize delivery, arm a wave, move a ref, release, or spend.
- A provider-native child inherits grouping only. It cannot contribute task
  proof or receive a fake independent lease.
- A managed child is a separately admitted ready or rework task with its own
  claim, lease, workspace, controls, and outcome. A parent worker may submit
  intent but never launch a nested Tusker runner.
- Binding creates a recorded generation and time boundary. Earlier unbound
  events do not become retroactively authoritative.
- Parent termination never forges child termination.
- Notification loss is repaired by authoritative fetch.
- Display names, ticket IDs, provider IDs, reusable agent types, and immutable
  Tusker IDs remain separate fields.

## Delivery boundaries

- Reuse `ORC-T-0041` as the universal claimed work-session foundation.
- Reuse the completed lease/liveness and live snapshot work rather than
  creating a second ownership store.
- Add focused graph and provider recovery proof before the broad
  `ORC-T-0047` factory-loop regression.
- Add only the graph-specific operator surface. The generic operations
  composition remains owned by `ORC-T-0046`.
- Codex local/app/cloud and Claude Code are the first provider set.

## Non-goals

- Starting, arming, dispatching, pausing, resuming, or landing a delivery while
  authoring or importing this plan.
- Replacing SQLite with provider state, files, or an in-memory graph.
- Letting interactive or dispatched workers recursively launch Tusker runners.
- Automatically converting provider-native children into tasks.
- Inventing independent child controls unsupported by the provider.
- Treating cloud status as local process evidence.
- Cross-agent chat, peer messaging, or providers beyond the first provider
  set.

<!-- tusker:delivery-import:a09de4af14f6b288:begin -->

## Work streams

- `[[ORC-T-0059]]` implements delivery source `claude-execution-adapter`.
- `[[ORC-T-0058]]` implements delivery source `codex-execution-adapter`.
- `[[ORC-T-0061]]` implements delivery source `cursor-timeline-projection`.
- `[[ORC-T-0056]]` implements delivery source `direct-execution-registration`.
- `[[ORC-T-0060]]` implements delivery source `execution-graph-read-model`.
- `[[ORC-T-0055]]` implements delivery source `execution-identity-ledger`.
- `[[ORC-T-0062]]` implements delivery source `execution-lifecycle-control`.
- `[[ORC-T-0065]]` implements delivery source `execution-observability-guidance`.
- `[[ORC-T-0063]]` implements delivery source `execution-operations-surface`.
- `[[ORC-T-0064]]` implements delivery source `execution-recovery-dogfood`.
- `[[ORC-T-0057]]` implements delivery source `provider-observation-envelope`.

- `[[W-0006]]` is the imported delivery wave.

<!-- tusker:delivery-import:a09de4af14f6b288:end -->

<!-- tusker:delivery-import:66d04491e5cad2a0:begin -->

- `[[ORC-T-0070]]` implements delivery source `claude-execution-adapter`.
- `[[ORC-T-0069]]` implements delivery source `codex-execution-adapter`.
- `[[ORC-T-0072]]` implements delivery source `cursor-timeline-projection`.
- `[[ORC-T-0067]]` implements delivery source `direct-execution-registration`.
- `[[ORC-T-0071]]` implements delivery source `execution-graph-read-model`.
- `[[ORC-T-0066]]` implements delivery source `execution-identity-ledger`.
- `[[ORC-T-0073]]` implements delivery source `execution-lifecycle-control`.
- `[[ORC-T-0076]]` implements delivery source `execution-observability-guidance`.
- `[[ORC-T-0074]]` implements delivery source `execution-operations-surface`.
- `[[ORC-T-0075]]` implements delivery source `execution-recovery-dogfood`.
- `[[ORC-T-0068]]` implements delivery source `provider-observation-envelope`.

- `[[W-0007]]` is the imported delivery wave.

<!-- tusker:delivery-import:66d04491e5cad2a0:end -->
