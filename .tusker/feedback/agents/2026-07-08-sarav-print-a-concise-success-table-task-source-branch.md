# Agent Feedback

- context: OPS-T-0005 live shakedown: tusker land TRC-T-0002 AGX-T-0005 FBK-T-0003 TRC-T-0003 RUN-T-0033 FBK-T-0005 RUN-T-0002 exited 0.
- friction: Successful land produced no stdout summary, so the operator has to inspect wave records and refs to know which branches landed, what target moved, and whether the gate ran.
- product-idea: Print a concise success table: task, source branch, target integration branch, gate summary, commit, and whether the wave was eligible for main.
- impact: A first live drain cannot be audited from the command result alone; silent success makes it too easy to miss partial integration or main-not-moved semantics.
- related: OPS-T-0005, OPS-T-0006, W-0001, tusker land
- dedupe-key: OPS-T-0005-land-silent-success
