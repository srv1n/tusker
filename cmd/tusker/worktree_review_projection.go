package main

import (
	"fmt"
	"strings"
	"time"
)

// projectCompletedWorktreeReviewToCanonical transfers the completed execution
// contract into the primary control plane before an authoritative reviewer is
// dispatched. It is deliberately stricter than a worktree reconciliation: the
// exact daemon-bound attempt must name the canonical revision it started from,
// so an intervening human edit is a loud conflict rather than an overwrite.
func projectCompletedWorktreeReviewToCanonical(canonicalVault string, run RunStatus) (string, int, error) {
	taskID := strings.ToUpper(strings.TrimSpace(run.ItemID))
	if taskID == "" || strings.TrimSpace(run.ActiveAttemptID) == "" || strings.TrimSpace(run.WorkspacePath) == "" {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires task, active attempt, and workspace identity")
	}
	if run.Lane != runLaneExecute {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires an execute-lane run")
	}
	worktreeVault := runnerWorktreeVaultPath(run.WorkspacePath, canonicalVault)
	if worktreeVault == "" || workspacePathsCompatible(worktreeVault, canonicalVault) {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires an isolated worktree vault")
	}
	idx, err := loadV7Index(worktreeVault)
	if err != nil {
		return "", 0, err
	}
	localTask, ok := idx.Tasks[taskID]
	if !ok {
		return "", 0, tuskerError(errorNotFound, "execution review projection cannot find worktree task "+taskID)
	}
	var bound []Note
	for _, attempt := range idx.Attempts[taskID] {
		if strings.EqualFold(stringField(attempt.Data, "runtime_attempt_id"), run.ActiveAttemptID) {
			bound = append(bound, attempt)
		}
	}
	if len(bound) != 1 {
		return "", 0, tuskerError("CAS_CONFLICT", "execution review projection requires exactly one worktree attempt bound to runtime attempt "+run.ActiveAttemptID)
	}
	attempt := bound[0]
	if stringField(attempt.Data, "status") != "handoff" ||
		stringField(attempt.Data, "lane") != runLaneExecute ||
		stringField(attempt.Data, "task") != taskID ||
		!workspacePathsCompatible(stringField(attempt.Data, "workspace_path"), run.WorkspacePath) {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection rejected mismatched worktree attempt binding")
	}
	startRevision := strings.TrimSpace(stringField(attempt.Data, "task_state_rev"))
	if startRevision == "" {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires the attempt's immutable task-state revision")
	}
	if stringField(localTask.Data, "status") != "review" || stringField(localTask.Data, "proof_status") != "satisfied" {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires a proof-satisfied worktree task in review")
	}
	branch, err := currentGitBranchIn(run.WorkspacePath)
	if err != nil || !strings.EqualFold(branch, v7TaskBranchName(taskID)) {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires checked-out implementation branch "+v7TaskBranchName(taskID))
	}
	sourceSHA, ok := gitRevParse(run.WorkspacePath, "HEAD^{commit}")
	if !ok {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection requires a committed implementation source")
	}

	canonicalTask, err := resolveV7Note(canonicalVault, taskID, "task")
	if err != nil {
		return "", 0, err
	}
	canonicalData, _, err := parseFrontmatterMustRead(canonicalTask.AbsolutePath)
	if err != nil {
		return "", 0, err
	}
	canonicalRevision := stringField(canonicalData, "state_rev")
	if canonicalRevision != startRevision {
		return "", 0, tuskerError("CAS_CONFLICT", "execution review projection refused because canonical task changed after dispatch")
	}
	data, body, err := parseFrontmatterMustRead(localTask.AbsolutePath)
	if err != nil {
		return "", 0, err
	}
	if !v7StateRevMatches(data, body, stringField(data, "state_rev")) {
		return "", 0, tuskerError(errorInvalidTransition, "execution review projection rejected malformed worktree task state")
	}
	// Wave membership is canonical control-plane authority. A worker receives it
	// as context, but must never be able to remove, replace, or invent it while
	// returning its implementation snapshot. In particular, completion must
	// retain the armed wave binding that the completion reactor later verifies.
	if waveID := strings.TrimSpace(stringField(canonicalData, "wave")); waveID != "" {
		data["wave"] = waveID
	} else {
		delete(data, "wave")
	}
	// A review result is keyed by the completed implementation candidate, not
	// the newly-created task placeholder.  The first candidate is revision 1;
	// every later rework completion advances it again.  This happens only at the
	// canonical execute-to-review boundary, after the exact source commit is
	// known and while the dispatch-start revision still protects us from drift.
	workRevision := maxInt(intField(canonicalData, "work_revision"), intField(data, "work_revision")) + 1
	data["work_revision"] = workRevision
	data["source_sha"] = sourceSHA
	data["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	data["updated_by"] = "daemon:review-projection"
	if _, err := saveV7DocumentCAS(canonicalTask.AbsolutePath, data, body, v7FrontmatterOrder["task"], canonicalRevision); err != nil {
		return "", 0, err
	}
	if err := emitV7Event(canonicalVault, taskID, "task", "review_requested", "daemon:review-projection", map[string]any{
		"attempt": run.ActiveAttemptID, "source_sha": sourceSHA, "workspace": run.WorkspacePath,
	}); err != nil {
		return "", 0, err
	}
	return sourceSHA, workRevision, nil
}

func projectExecutionReviewProjectionReason(err error) string {
	if err == nil {
		return ""
	}
	return "execution review projection failed: " + firstActionableLine("", fmt.Sprint(err))
}
