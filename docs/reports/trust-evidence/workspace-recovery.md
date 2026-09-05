# Workspace recovery evidence

- Revision: `03201019`; host: `Saravanans-MacBook-Pro.local`.
- Executed: `GOMAXPROCS=2 scripts/with-validation-lock.sh -- go test -p=1 -parallel=1 ./cmd/tusker -run '^TestTrustWorkspaceRecovery$' -count=1 -v` — PASS.
- [TestTrustWorkspaceRecovery](../../../cmd/tusker/trust_workspace_recovery_test.go) used temporary real Git worktrees, retained a dirty checkpoint across a new manager, preserved an active durable owner at cap pressure, and confirmed failed Git setup creates no workspace receipt.

| Acceptance | Evidence | Status |
| --- | --- | --- |
| A1 | Git worktree failure is fail-closed and active ownership prevents cap cleanup. | Temporary-Git PASS |
| A2 | A new manager reuses the dirty workspace and preserves its checkpoint. | Temporary-Git PASS |
| A3 | The active-worktree cap is enforced conservatively; two concurrent temporary-Git worktree preparations both succeed with distinct worktrees under the shared root lock. Existing build-cache reuse has no new cache layer and was not separately measured. | Temporary-Git PASS |

This is local filesystem/Git evidence; it does not run external workers.
