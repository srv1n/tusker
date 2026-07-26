package main

import (
	"fmt"
	"strings"
)

// completionWorkerSafety is intentionally stricter than generic automation:
// completion consumes a reviewer verdict as lifecycle authority, so every
// profile participating in that flow must have an enforceable filesystem
// boundary.  A friendly name or a denylist is not a sandbox.
func completionWorkerSafety(stateRoot, workspace string, profile ResolvedRunnerProfile) error {
	mode := strings.TrimSpace(profile.Definition.Sandbox.Mode)
	if mode != "workspace-write" && mode != "read-only" {
		return fmt.Errorf("completion authority refuses profile %q: enforceable sandbox must be workspace-write or read-only", profile.Name)
	}
	if strings.TrimSpace(profile.Definition.PermissionPreset) == "danger-full-access" || mode == "danger-full-access" {
		return fmt.Errorf("completion authority refuses profile %q: danger-full-access is not admissible", profile.Name)
	}
	// Only codex_exec currently translates the resolved policy into Codex's
	// detached-command sandbox flags.  Codex's generic/app-server paths and
	// Claude merely receive metadata (Claude defaults to bypassPermissions), so
	// treating their sandbox strings as authority would be security theatre.
	if RunnerName(strings.TrimSpace(profile.Definition.Harness)) != RunnerCodexExec {
		return fmt.Errorf("completion authority refuses profile %q: runner does not mechanically enforce the resolved sandbox", profile.Name)
	}
	if strings.TrimSpace(stateRoot) == "" || strings.TrimSpace(workspace) == "" || pathWithin(workspace, stateRoot) {
		return fmt.Errorf("completion authority requires daemon state root outside worker-writable workspace")
	}
	return nil
}

func (d *Daemon) validateCompletionWorkerAuthority(project RegisteredProject, wf Workflow, note Note, result ReviewResult) error {
	if d == nil || d.store == nil {
		return fmt.Errorf("completion authority requires the resident daemon store")
	}
	review, err := resolveRunProfileForLane(note, wf, runLaneReview, result.Runner)
	if err != nil {
		return err
	}
	if review.Name == "" || review.Name != result.RunnerProfile {
		return fmt.Errorf("completion authority refuses review profile drift")
	}
	execute, err := resolveRunProfileForLane(note, wf, runLaneExecute, "")
	if err != nil {
		return err
	}
	if execute.Name == "" || review.Name == "" {
		return fmt.Errorf("completion authority requires explicit named execute and review profiles")
	}
	if err := completionWorkerSafety(d.stateRoot, workspaceForCompletionSafety(project, result.TaskID), execute); err != nil {
		return err
	}
	if err := completionWorkerSafety(d.stateRoot, workspaceForCompletionSafety(project, result.TaskID), review); err != nil {
		return err
	}
	runs, err := d.store.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		if run.ProjectID != project.ProjectID || run.RecordID != result.TaskID || strings.TrimSpace(run.WorkspacePath) == "" {
			continue
		}
		if pathWithin(run.WorkspacePath, d.stateRoot) {
			return fmt.Errorf("completion authority requires daemon state root outside every worker workspace")
		}
	}
	return nil
}

func workspaceForCompletionSafety(project RegisteredProject, taskID string) string {
	// The exact run rows are checked separately.  This fallback is only for
	// resolving profile safety before a row has retained a workspace path.
	return firstNonEmpty(project.RepoRoot, taskID)
}
