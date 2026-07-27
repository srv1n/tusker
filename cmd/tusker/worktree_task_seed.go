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

// seedCanonicalV7TaskForPreparedWorkspace intentionally seeds only the first
// materialization. Later lanes reuse the worker's local task state (for example
// an execute lane moving ready -> review), which is lifecycle output and must
// never be compared with or overwritten by canonical control-plane input.
func seedCanonicalV7TaskForPreparedWorkspace(canonicalVault string, workspace WorkspacePrepareResult, strategy WorkspaceStrategy, taskID string) error {
	if strategy == WorkspaceStrategyShared || !workspace.NewlyMaterialized {
		return nil
	}
	return seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace.Path, taskID)
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
