---
subject: 2026-09-10-agent-coordination-grill
title: "Agent coordination decisions — 10 September 2026"
keywords: [architect, messages, clarification, autonomy, tokens, rollout]
part_of: agent-coordination
status: canonical
created: 2026-09-10
read_when: "Understanding the operator's examples and why coordination must avoid model-driven relaying."
skip_when: "Implementing the resulting contract; read agent-coordination."
decides_for: .tusker/specs/agent-coordination.md
---

# Agent coordination decisions

These entries preserve the discussion, including corrections. They do not claim that proposed field names, exact limits or an unasked implementation trade-off received operator approval.

## D1 — Close the architect loop

Discussion: The operator described harnesses retaining conversations that can receive later messages, then asked how Tusker could wake an architect after a wave, report completion, ask for the next tasks and continue autonomously.

Response proposed: Extend existing session/daemon/delivery machinery with durable architect identity, reliable notifications and continuation.

Operator confirmed: The objective is to remove the manual coordination loop and continue working across waves.

Locked requirement: Preserve an addressable architect conversation across waves and return verified results to it for subsequent work.

## D2 — Clarification and peer contacts are necessary

Operator example: A wave starts tasks one through five; task four gets stuck. It must be able to ask its architect directly, and threads must also be able to message relevant other threads. Tickets need parent/architect/contact references so later continuations know whom to address.

Locked requirement: Task-to-architect and peer messaging are first-class scope, not optional follow-up. References survive worker completion/retry and messages retain their question/reply association.

Implementation recommendation: Separate origin/architect/peer contacts from execution parentage and scheduling dependencies. Reuse stable execution roots and task addresses; keep transient provider endpoint details in the runtime store.

## D3 — Remove copy/paste and model-driven bookkeeping

Operator rationale: Repeated copying and pasting is the work to eliminate. Earlier attempts spent expensive Astra and Sol turns acting as an orchestrator. High-tier architect profiles, including the examples Astra, Sol and Fable, should be called only when needed.

Locked requirement: Tusker code owns routing, waiting, status aggregation, retries and wakeups. Premium models perform substantive design/clarification work. Configured profiles remain exact; do not invent availability or downgrade them to save bookkeeping cost.

Implementation recommendation: Durable message queue, selective event triggers, coalesced questions and compact evidence references. Measure model calls by recipient and trigger; unchanged polls must use zero turns.

## D4 — The pause is temporary, not the product boundary

Earlier response emphasized that present wave authorization prevents automatic subsequent-wave starts.

Operator correction: Automatic execution was paused only while confidence is established. The mechanisms should be designed and built in parallel with that testing. Repeatedly returning to the temporary authorization setting misses the intended scope.

Locked requirement: Implement successive-wave continuation and messaging now; preserve current live rollout settings. Paused/enabled behavior is configuration, not an excuse to defer architecture or require a new per-wave product approval ritual.

## D5 — Harness capabilities are empirical

Operator said: Not every harness will support the APIs. Store the information and add messaging as support is available.

Locked requirement: Store typed identity and capability evidence independent of universal provider support. Integrate supported APIs incrementally, exposing unavailable operations honestly.

Earlier question: Must the first version attach to an existing conversation, or may Tusker start it? Tusker-owned first was recommended.

Operator response: Broadened the requirement to parent and peer threads without choosing an exclusive ownership mode.

Implementation recommendation, not a claimed user choice: Represent both externally registered and Tusker-created contacts. Qualify actual attach/resume/control capability per route; do not make universal external attachment a prerequisite for the generic mechanism.

## Follow-through

Canonical contract: [[agent-coordination]]. The implementation plan emits independently owned identity, mailbox, transport, clarification, wave-continuation, UI and qualification tasks. Current system documents change with verified implementation; this planning session does not certify or launch the feature.
