# Agent Feedback

- context: OPS-T-0005 live shakedown: W-0001 landing audit after failed batch.
- friction: Each failed landing audit row summarizes the merge failure as the first 'Auto-merging ...' line, not the actionable CONFLICT/error line or file that needs attention.
- product-idea: Filter merge progress chatter out of landingFailureSummary and preserve the first conflict/error line plus the conflicted path in task next_action.
- impact: The command kicked tasks to rework with low-signal instructions, forcing operators to reproduce failures to know what actually broke.
- related: OPS-T-0005, OPS-T-0006, W-0001, tusker land
- dedupe-key: OPS-T-0005-land-conflict-summary-chatter
