package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// seedCanonicalV7TaskIntoWorkspace makes one canonical task record visible to
// a newly-created isolated workspace whose Git base predates task import.
//
// The task record is control-plane input, not worker-owned state: this helper
// copies its already-canonical bytes once, never updates the canonical vault,
// and refuses any pre-existing non-identical local record. Runtime attempts,
// evidence and other mutable lifecycle records stay workspace-local and are
// created by their normal commands after this seed succeeds.
func seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspacePath, taskID string) error {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	if !v7TaskIDPattern.MatchString(taskID) {
		return tuskerError(errorInvalidField, "worktree task seed requires a valid V7 task identity")
	}
	canonicalVault = canonicalProjectPath(canonicalVault)
	workspacePath = canonicalProjectPath(workspacePath)
	if canonicalVault == "" || workspacePath == "" {
		return tuskerError(errorConfigInvalid, "worktree task seed requires canonical vault and workspace paths")
	}
	worktreeVault := runnerWorktreeVaultPath(workspacePath, canonicalVault)
	if worktreeVault == "" {
		return tuskerError(errorConfigInvalid, "cannot resolve worktree vault for canonical task seed")
	}
	if workspacePathsCompatible(worktreeVault, canonicalVault) {
		return nil
	}

	canonicalPath := filepath.Join(canonicalVault, "work", "tasks", taskID+".md")
	canonicalRaw, err := os.ReadFile(canonicalPath)
	if err != nil {
		return fmt.Errorf("read canonical task for worktree seed: %w", err)
	}
	canonicalData, canonicalBody, err := parseFrontmatter(string(canonicalRaw))
	if err != nil {
		return tuskerError(errorInvalidTransition, "canonical task seed is malformed: "+err.Error(), withPath(canonicalPath))
	}
	if effectiveV7Kind(canonicalData) != "task" || stringField(canonicalData, "id") != taskID ||
		!v7StateRevMatches(canonicalData, canonicalBody, stringField(canonicalData, "state_rev")) {
		return tuskerError(errorInvalidTransition, "canonical task seed identity or state revision mismatch", withPath(canonicalPath))
	}

	targetPath := filepath.Join(worktreeVault, "work", "tasks", taskID+".md")
	if existing, readErr := os.ReadFile(targetPath); readErr == nil {
		return validateSeededWorktreeTask(targetPath, existing, canonicalRaw, taskID)
	} else if !os.IsNotExist(readErr) {
		return fmt.Errorf("read worktree task seed target: %w", readErr)
	}
	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		return err
	}
	// O_EXCL keeps a concurrent worker from silently replacing a record between
	// our absence check and write. On a race, inspect the winner and accept only
	// the exact canonical bytes.
	f, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		_, writeErr := f.Write(canonicalRaw)
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		return closeErr
	}
	if !os.IsExist(err) {
		return fmt.Errorf("create worktree task seed: %w", err)
	}
	existing, readErr := os.ReadFile(targetPath)
	if readErr != nil {
		return fmt.Errorf("read raced worktree task seed target: %w", readErr)
	}
	return validateSeededWorktreeTask(targetPath, existing, canonicalRaw, taskID)
}

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

func validateSeededWorktreeTask(path string, existing, canonical []byte, taskID string) error {
	data, body, err := parseFrontmatter(string(existing))
	if err != nil {
		return tuskerError(errorInvalidTransition, "existing worktree task seed is malformed: "+err.Error(), withPath(path))
	}
	if effectiveV7Kind(data) != "task" || stringField(data, "id") != taskID ||
		!v7StateRevMatches(data, body, stringField(data, "state_rev")) {
		return tuskerError(errorInvalidTransition, "existing worktree task seed identity or state revision mismatch", withPath(path))
	}
	if string(existing) != string(canonical) {
		return tuskerError("CAS_CONFLICT", "existing worktree task seed differs from the canonical task record", withPath(path))
	}
	return nil
}
