# Agent Feedback

- context: OPS-T-0005 live land shakedown: queue inspection found detached dirty task worktrees.
- friction: tusker land defaults to merging task/<TASK-ID>, but the queue it is meant to drain contains detached dirty worktrees and the command gives no supported branchification or consume-this-worktree path.
- product-idea: Add a queue-drain mode for detached completed worktrees, or print the exact branch creation/commit command expected before retrying land.
- impact: The operator has to infer a manual branchification ceremony before the merge lane can test old completed task directories.
- related: OPS-T-0005, OPS-T-0006, tusker land, detached worktrees
- dedupe-key: OPS-T-0005-land-detached-dirty-worktrees
