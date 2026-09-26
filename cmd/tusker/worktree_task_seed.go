package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// syncCanonicalV7TaskIntoWorkspace refreshes a task's control-plane snapshot
// immediately before an execute attempt. Unlike the one-time seed, a retry
// may reuse a workspace whose local DAG was created before an upstream task
// became reviewable or done. The daemon owns this boundary and replaces only
// the task contract; source files and workspace-local attempts remain intact.
func syncCanonicalV7TaskIntoWorkspace(canonicalVault, workspacePath, taskID string) error {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if !v7TaskIDPattern.MatchString(taskID) {
		return tuskerError(errorInvalidField, "worktree task sync requires a valid V7 task identity")
	}
	canonicalVault = canonicalProjectPath(canonicalVault)
	workspacePath = canonicalProjectPath(workspacePath)
	if canonicalVault == "" || workspacePath == "" {
		return tuskerError(errorConfigInvalid, "worktree task sync requires canonical vault and workspace paths")
	}
	worktreeVault := runnerWorktreeVaultPath(workspacePath, canonicalVault)
	if worktreeVault == "" || workspacePathsCompatible(worktreeVault, canonicalVault) {
		return nil
	}
	canonicalPath := filepath.Join(canonicalVault, "work", "tasks", taskID+".md")
	canonicalRaw, err := os.ReadFile(canonicalPath)
	if err != nil {
		return fmt.Errorf("read canonical task for worktree sync: %w", err)
	}
	canonicalData, canonicalBody, err := parseFrontmatter(string(canonicalRaw))
	if err != nil {
		return tuskerError(errorInvalidTransition, "canonical task sync is malformed: "+err.Error(), withPath(canonicalPath))
	}
	if effectiveV7Kind(canonicalData) != "task" || stringField(canonicalData, "id") != taskID ||
		!v7StateRevMatches(canonicalData, canonicalBody, stringField(canonicalData, "state_rev")) {
		return tuskerError(errorInvalidTransition, "canonical task sync identity or state revision mismatch", withPath(canonicalPath))
	}
	targetPath := filepath.Join(worktreeVault, "work", "tasks", taskID+".md")
	if info, err := os.Lstat(targetPath); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return tuskerError(errorInvalidTransition, "worktree task sync target must be a regular file", withPath(targetPath))
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect worktree task sync target: %w", err)
	}
	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	return writeText(targetPath, string(canonicalRaw))
}

func canonicalV7ExecutionTaskSnapshotIDs(canonicalVault, taskID string) ([]string, error) {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	idx, err := loadV7Index(canonicalVault)
	if err != nil {
		return nil, err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return nil, tuskerError(errorNotFound, "canonical execution snapshot task not found: "+taskID)
	}
	ids := []string{taskID}
	for _, edge := range v7TaskDependencyEdges(task, idx) {
		if _, ok := idx.Tasks[edge.ID]; ok {
			ids = append(ids, edge.ID)
		}
	}
	return uniqueStrings(ids), nil
}

// seedCanonicalV7TaskForPreparedWorkspace refreshes the execute task snapshot
// from canonical authority. A worktree base may predate task registration or a
// dependency projection; retries may reuse an older lifecycle record. A
// reviewer instead receives an immutable source snapshot: injecting canonical
// state there would both hide the reviewed source and make an otherwise
// read-only workspace dirty.
func seedCanonicalV7TaskForPreparedWorkspace(canonicalVault string, workspace WorkspacePrepareResult, strategy WorkspaceStrategy, lane, taskID string) error {
	if strategy == WorkspaceStrategyShared || lane == runLaneReview {
		return nil
	}
	return syncCanonicalV7TaskIntoWorkspace(canonicalVault, workspace.Path, taskID)
}
