---
title: "Current planning and execution design"
subject: reports/skills-docs/current-design
keywords: [current design, planning, execution, architect, model routing]
part_of: planning-handoff-and-agent-entry
status: current
created: 2026-09-11
read_when: "Recovering the current planning and execution direction without replaying historical overrides."
skip_when: "Looking up installed CLI syntax or runtime qualification evidence."
sources:
  - .tusker/specs/planning-handoff-and-agent-entry.md
  - .tusker/specs/spec-to-proof.md
  - .tusker/specs/decisions/2026-09-10-agent-coordination-grill.md
  - .tusker/specs/model-level-configuration.md
  - .tusker/specs/skills-and-documentation.md
capsule:
  what: "Cold-reader account of current planning, routing and coordination decisions."
  use_when: "Distinguishing settled direction, proposals, open questions and deferred work."
  skip_when: "Treating design intent as proof of installed behavior."
---

# Current planning and execution design

## Current account

The goal is a faithful path from a user-and-architect product conversation to bounded work with inspectable proof. External methods may shape the product specification. Tusker owns durable documents, task/wave contracts, configured execution and review routing, lifecycle state, evidence and mechanical coordination.

Settled choices:

- Imported plans remain inert until explicitly authorized. Once work is authorized, code handles scheduling, status, retries and message delivery without using an architect model for bookkeeping.
- Workers can address durable questions to the architect or relevant peers. Configured continuation may return accepted or stalled wave results to the architect for the next product decision.
- The current rollout keeps task and wave starts manual until coordination is qualified. That safety setting does not cancel the settled requirement to build configurable continuation.
- Task contracts choose work/review levels. Current configuration resolves those levels to profiles, models, effort, permissions and explicit fallbacks; historical named-model tables preserve intent but do not prove or override current configuration.
- Executor-recorded checks, independent review and explicit human decisions are distinct acceptance methods. Routine independent review is not a new human gate.

Active proposals include exact schema/UI names and transport-specific wake/resume integrations. The centralized service is an approved direction whose migration remains deferred. Open questions are which installed transports can truthfully resume existing contacts and what qualification permits enabling continuation for a real objective. Other deferred work includes broad multi-machine placement, semantic recipient selection, formal spec locking and automatic change-impact scheduling.

Non-goals for this account: changing product policy, runtime behavior, model settings, automation, UI, installed skills or the broader obligations of `FLW-T-0026`.

## Reading route

1. [Current planning handoff](../../../.tusker/specs/planning-handoff-and-agent-entry.md#current-direction)
2. [Core spec-to-proof direction](../../../.tusker/specs/spec-to-proof.md#current-reading-path)
3. [Agent coordination contract](../../../.tusker/specs/agent-coordination.md) and [decision record](../../../.tusker/specs/decisions/2026-09-10-agent-coordination-grill.md)
4. [Configured model levels](../../../.tusker/specs/model-level-configuration.md)
5. [September 11 auxiliary contract](../../../.tusker/specs/skills-and-documentation.md)

## Disposition

| Material | Disposition | Current authority or preserved source |
| --- | --- | --- |
| External design conversation; Tusker handoff ownership | Retained and summarized | [Planning handoff](../../../.tusker/specs/planning-handoff-and-agent-entry.md#confirmed-user-decisions) |
| Inert import and bounded orchestration | Clarified: authorization and post-start mechanics are separate | [Planning conversion](../../../.tusker/specs/planning-handoff-and-agent-entry.md#spec-to-task-conversion) |
| Manual next-wave starts | Retained as current rollout setting, not permanent design prohibition | [Local pilot override](../../../.tusker/specs/spec-to-proof.md#immediate-delivery-override--local-pilot-first) |
| Architect and peer contact; configured continuation | Retained as settled product direction | [Coordination decisions D1–D4](../../../.tusker/specs/decisions/2026-09-10-agent-coordination-grill.md) |
| Fixed Luna/Terra/Sol examples | Preserved as September 6 intent; current resolution delegated to configuration | [Historical model roles](../../../.tusker/specs/spec-to-proof.md#historical-initial-model-roles-and-current-cost-boundary) and [model configuration](../../../.tusker/specs/model-level-configuration.md) |
| Centralized service | Retained, explicitly deferred | [Deferred destination](../../../.tusker/specs/spec-to-proof.md#centralized-service-decision--deferred-destination) |
| Auxiliary work and broader `FLW-T-0026` obligations | Links retained; neither is closed or weakened here | [Auxiliary contract](../../../.tusker/specs/skills-and-documentation.md#parallel-ownership-and-existing-work) |

No material decision was removed or relocated. The edit adds a current route and status labels around preserved source text.

## Cold-reader exercise

A reviewer should answer these without raw transcripts:

1. What is the current product goal?
2. Which choices are settled, and which items remain proposals?
3. What is explicitly out of scope or deferred?
4. Who owns model routing and mechanical coordination?
5. What is the next open decision, and what evidence is still unavailable?

Expected answer to question 5: decide when configured architect continuation can be enabled for a real objective, after installed transport wake/resume behavior and the coordination lifecycle are qualified. These documents state design intent only; they do not prove installed runtime behavior, live model execution, message delivery or safe automatic continuation.
