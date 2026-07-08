# Agent Feedback

- context: OPS-T-0005 live shakedown: batch tusker land exited 0 after staging W-0001.
- friction: Every task in the batch failed during merge staging, integration/W-0001 stayed at main, and the command still exited 0 with no stdout while moving all seven member tasks to rework.
- product-idea: When no requested task lands, tusker land should exit non-zero or print an unmistakable failed-batch summary before/while applying rework transitions.
- impact: Automation and operators can mistake a total landing failure for success; the only evidence is hidden in wave audit/task state after the fact.
- related: OPS-T-0005, OPS-T-0006, W-0001, tusker land
- dedupe-key: OPS-T-0005-land-total-failure-exit-zero
