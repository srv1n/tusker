# Clarification behavior

Blocking questions persist before a wakeup is queued. Wakeups are deterministic database records with zero model turns until the resident scheduler consumes one. Duplicate wake reasons coalesce; answers arriving before yield prevent parking; explicit wait cycles are detected.

Verification: `TestClarificationWorkflowReplyBeforeYield` and `TestAgentCoordinationE2EFiveTasksTwoWaves` — PASS.
