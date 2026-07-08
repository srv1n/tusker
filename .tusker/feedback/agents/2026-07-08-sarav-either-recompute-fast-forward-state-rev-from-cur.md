# Agent Feedback

- context: Recording proof on a task contract whose implementation was done by dispatched runners (outside the claim->attempt->verify lifecycle), via 'tusker verify add <task> --covers ... --result pass'.
- friction: verify add fails with [CAS_CONFLICT] 'verification write conflicted with a newer proof edit' on tasks that are backlog/held and were not concurrently edited (reproduced on OPS-T-0007, OPS-T-0008, SRV-T-0018). The 'retry exactly' hint does not resolve it, so there is no path to attach proof or close such a task; its ledger state is unreachable from the CLI.
- product-idea: Either recompute/fast-forward state_rev from current on-disk content when the task is internally consistent instead of hard-failing, or surface the real precondition (e.g. 'proof requires the task to be claimed/in-progress') instead of a generic CAS conflict, and offer a reconcile/--force-rev escape hatch for out-of-band or runner-implemented tasks.
- impact: The agent-time workflow (runners implement, planner records proof/closes) cannot close the loop: proof writes are rejected, leaving landed+proven work stuck at backlog in the ledger.
- related: tusker verify add / proof CAS; state_rev in task frontmatter; runner-implemented tasks
- dedupe-key: verify-add-cas-conflict-blocks-proof
