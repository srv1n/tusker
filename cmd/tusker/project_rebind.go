package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const projectRebindSchema = "tusker.projects-rebind/v1"

type projectRebindReport struct {
	Schema     string            `json:"schema"`
	ProjectID  string            `json:"project_id"`
	DryRun     bool              `json:"dry_run"`
	Changed    bool              `json:"changed"`
	Idempotent bool              `json:"idempotent"`
	Before     RegisteredProject `json:"before"`
	After      RegisteredProject `json:"after"`
}

// projectsRebindCmd is intentionally registry-only. It preserves the durable
// project identity and every ProjectID-keyed runtime record; it does not touch
// workspace mounts, local config, task files, or the target checkout.
func projectsRebindCmd(args Args) error {
	projectID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	repoRoot, err := requireArg(args, "repo")
	if err != nil {
		return err
	}
	vaultRoot, err := requireArg(args, "vault")
	if err != nil {
		return err
	}
	repoRoot, vaultRoot, err = validateProjectRebindTarget(repoRoot, vaultRoot)
	if err != nil {
		return err
	}
	if err := refuseProjectRebindWorkspaceMount(projectID, vaultRoot); err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()

	before, err := projectByID(store, projectID)
	if err != nil {
		return err
	}
	preview := before
	preview.RepoRoot, preview.VaultRoot, preview.WorkflowPath = repoRoot, vaultRoot, workflowPath(vaultRoot)
	preview.Health, preview.LastError = projectHealthDisabled, ""
	if args.Bool("dry-run") {
		report := projectRebindReport{Schema: projectRebindSchema, ProjectID: projectID, DryRun: true, Idempotent: sameCanonicalProjectPath(before.RepoRoot, repoRoot) && sameCanonicalProjectPath(before.VaultRoot, vaultRoot), Before: before, After: preview}
		if err := validateProjectRebindPreconditions(store, before, repoRoot, vaultRoot); err != nil {
			return err
		}
		return emitProjectRebindReport(args, report)
	}
	before, after, changed, err := store.RebindProjectRegistration(projectID, repoRoot, vaultRoot)
	if err != nil {
		return err
	}
	return emitProjectRebindReport(args, projectRebindReport{Schema: projectRebindSchema, ProjectID: projectID, Changed: changed, Idempotent: !changed, Before: before, After: after})
}

func emitProjectRebindReport(args Args, report projectRebindReport) error {
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "rebind": report})
		return nil
	}
	if report.DryRun {
		fmt.Printf("Would rebind project %s from %s to %s\n", report.ProjectID, report.Before.RepoRoot, report.After.RepoRoot)
		return nil
	}
	if report.Idempotent {
		fmt.Printf("Project %s is already rebound to %s\n", report.ProjectID, report.After.RepoRoot)
		return nil
	}
	fmt.Printf("Rebound project %s from %s to %s\n", report.ProjectID, report.Before.RepoRoot, report.After.RepoRoot)
	return nil
}

func validateProjectRebindTarget(repoRoot, vaultRoot string) (string, string, error) {
	repoRoot, err := canonicalExistingProjectDirectory(repoRoot, "repo")
	if err != nil {
		return "", "", err
	}
	vaultRoot, err = canonicalExistingProjectDirectory(vaultRoot, "vault")
	if err != nil {
		return "", "", err
	}
	if err := validateProjectStorageBoundary(repoRoot, vaultRoot); err != nil {
		return "", "", err
	}
	if _, err := loadWorkflow(vaultRoot); err != nil {
		return "", "", err
	}
	if err := requireCleanGitRepository(repoRoot); err != nil {
		return "", "", err
	}
	return repoRoot, vaultRoot, nil
}

func canonicalExistingProjectDirectory(path, label string) (string, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", tuskerError(errorNotFound, "project rebind "+label+" path must exist and resolve canonically: "+abs, withPath(abs))
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", tuskerError(errorConfigInvalid, "project rebind "+label+" path must be a directory", withPath(resolved))
	}
	return filepath.Clean(resolved), nil
}

func requireCleanGitRepository(repoRoot string) error {
	check := exec.Command("git", "-C", repoRoot, "rev-parse", "--is-inside-work-tree")
	if out, err := check.Output(); err != nil || strings.TrimSpace(string(out)) != "true" {
		return tuskerError(errorConfigInvalid, "project rebind repo must be a Git worktree", withPath(repoRoot))
	}
	status := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	out, err := status.Output()
	if err != nil {
		return fmt.Errorf("read Git status for project rebind: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return tuskerError(errorInvalidTransition, "project rebind target repository must be clean", withPath(repoRoot))
	}
	return nil
}

func refuseProjectRebindWorkspaceMount(projectID, vaultRoot string) error {
	workspace, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	for _, mount := range workspace.Projects {
		if mount.ProjectID != projectID && sameCanonicalProjectPath(mount.TrackerRoot, vaultRoot) {
			return tuskerError(errorInvalidTransition, "project rebind target vault is already mounted by another project", withContext(map[string]any{"project_id": projectID, "conflicting_project_id": mount.ProjectID, "mount_path": mount.MountPath, "target_vault_root": vaultRoot}), withHint("unmount or repair the conflicting workspace mount first; v1 rebind changes the runtime registry only"))
		}
		if mount.ProjectID == projectID && !sameCanonicalProjectPath(mount.TrackerRoot, vaultRoot) {
			return tuskerError(errorInvalidTransition, "project rebind refuses a mounted workspace target that requires cross-filesystem mutation", withContext(map[string]any{"project_id": projectID, "mount_path": mount.MountPath, "tracker_root": mount.TrackerRoot, "target_vault_root": vaultRoot}), withHint("unmount or repair the workspace mount first; v1 rebind changes the runtime registry only"))
		}
	}
	return nil
}

func projectByID(store *RuntimeStore, projectID string) (RegisteredProject, error) {
	projects, err := store.ListProjects()
	if err != nil {
		return RegisteredProject{}, err
	}
	for _, project := range projects {
		if project.ProjectID == projectID {
			return project, nil
		}
	}
	return RegisteredProject{}, tuskerError(errorNotFound, "project not found: "+projectID)
}

func validateProjectRebindPreconditions(store *RuntimeStore, before RegisteredProject, repoRoot, vaultRoot string) error {
	if before.Enabled {
		return tuskerError(errorInvalidTransition, "project must be disabled before rebind: "+before.ProjectID)
	}
	if sameCanonicalProjectPath(before.RepoRoot, repoRoot) && sameCanonicalProjectPath(before.VaultRoot, vaultRoot) {
		return nil
	}
	if sameCanonicalProjectPath(before.RepoRoot, repoRoot) || sameCanonicalProjectPath(before.VaultRoot, vaultRoot) {
		return tuskerError(errorInvalidArg, "project rebind must change repo_root and vault_root together")
	}
	active, err := store.CountProjectNonTerminalRuns(before.ProjectID)
	if err != nil {
		return err
	}
	if active != 0 {
		return tuskerError(errorInvalidTransition, fmt.Sprintf("project rebind requires zero non-terminal runs; found %d", active))
	}
	projects, err := store.ListProjects()
	if err != nil {
		return err
	}
	for _, other := range projects {
		if other.ProjectID != before.ProjectID && (sameCanonicalProjectPath(other.RepoRoot, repoRoot) || sameCanonicalProjectPath(other.VaultRoot, vaultRoot)) {
			return tuskerError(errorInvalidTransition, "project rebind target is already claimed by project "+other.ProjectID)
		}
	}
	return nil
}
