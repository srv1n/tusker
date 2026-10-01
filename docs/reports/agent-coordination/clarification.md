---
title: "Clarification behavior"
read_when: "Recovering the recorded clarification, durable wakeup and wait-cycle behavior evidence for agent coordination."
skip_when: "You need the overall coordination qualification verdict; read the agent-coordination report or current system chapter."
---

# Clarification behavior

Blocking questions persist before a wakeup is queued. Wakeups are deterministic database records with zero model turns until the resident scheduler consumes one. Duplicate wake reasons coalesce; answers arriving before yield prevent parking; explicit wait cycles are detected.

Verification: `TestClarificationWorkflowReplyBeforeYield` and `TestAgentCoordinationE2EFiveTasksTwoWaves` — PASS.
