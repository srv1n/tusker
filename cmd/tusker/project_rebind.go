package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const projectRebindSchema = "tusker.projects-rebind/v1"

type projectRebindReport struct {
	Schema              string            `json:"schema"`
	ProjectID           string            `json:"project_id"`
	DryRun              bool              `json:"dry_run"`
	AllowDirty          bool              `json:"allow_dirty"`
	RetainedQueuedCount int               `json:"retained_queued_count"`
	Changed             bool              `json:"changed"`
	Idempotent          bool              `json:"idempotent"`
	Before              RegisteredProject `json:"before"`
	After               RegisteredProject `json:"after"`
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
	allowDirty := args.Bool("allow-dirty")
	repoRoot, vaultRoot, err = validateProjectRebindTarget(repoRoot, vaultRoot, allowDirty)
	if err != nil {
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
	if err := refuseProjectRebindWorkspaceMount(before, vaultRoot); err != nil {
		return err
	}
	if err := validateProjectRebindIdentity(repoRoot, vaultRoot, before); err != nil {
		return err
	}
	if err := validateProjectRebindGitCommonDir(before.RepoRoot, repoRoot); err != nil {
		return err
	}
	preview := before
	preview.RepoRoot, preview.VaultRoot, preview.WorkflowPath = repoRoot, vaultRoot, workflowPath(vaultRoot)
	preview.Health, preview.LastError = projectHealthDisabled, ""
	runs, err := store.ListProjectNonTerminalRuns(projectID)
	if err != nil {
		return err
	}
	directives, err := store.ListActiveRunDirectives(projectID, time.Now().UTC())
	if err != nil {
		return err
	}
	if len(directives) != 0 {
		return projectRebindActiveDirectivesError(projectID, directives)
	}
	retainedQueuedCount := len(projectRebindRetainedQueueRuns(runs))
	if err := validateProjectRebindPreconditions(store, before, repoRoot, vaultRoot, directives); err != nil {
		return err
	}
	if args.Bool("dry-run") {
		report := projectRebindReport{Schema: projectRebindSchema, ProjectID: projectID, DryRun: true, AllowDirty: allowDirty, RetainedQueuedCount: retainedQueuedCount, Idempotent: sameCanonicalProjectPath(before.RepoRoot, repoRoot) && sameCanonicalProjectPath(before.VaultRoot, vaultRoot), Before: before, After: preview}
		return emitProjectRebindReport(args, report)
	}
	before, after, changed, err := store.RebindProjectRegistration(projectID, repoRoot, vaultRoot)
	if err != nil {
		return err
	}
	return emitProjectRebindReport(args, projectRebindReport{Schema: projectRebindSchema, ProjectID: projectID, AllowDirty: allowDirty, RetainedQueuedCount: retainedQueuedCount, Changed: changed, Idempotent: !changed, Before: before, After: after})
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

func validateProjectRebindTarget(repoRoot, vaultRoot string, allowDirty bool) (string, string, error) {
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
	if err := requireGitRepository(repoRoot, !allowDirty); err != nil {
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

func requireGitRepository(repoRoot string, requireClean bool) error {
	check := exec.Command("git", "-C", repoRoot, "rev-parse", "--show-toplevel")
	out, err := check.Output()
	if err != nil || !sameCanonicalProjectPath(strings.TrimSpace(string(out)), repoRoot) {
		return tuskerError(errorConfigInvalid, "project rebind repo must be a Git worktree", withPath(repoRoot))
	}
	if !requireClean {
		return nil
	}
	status := exec.Command("git", "-C", repoRoot, "status", "--porcelain=v1", "--untracked-files=all")
	out, err = status.Output()
	if err != nil {
		return fmt.Errorf("read Git status for project rebind: %w", err)
	}
	if strings.TrimSpace(string(out)) != "" {
		return tuskerError(errorInvalidTransition, "project rebind target repository must be clean", withPath(repoRoot))
	}
	return nil
}

func refuseProjectRebindWorkspaceMount(project RegisteredProject, vaultRoot string) error {
	workspace, err := loadWorkspaceVaultConfig()
	if err != nil {
		return err
	}
	projectID := project.ProjectID
	for _, mount := range workspace.Projects {
		mountBelongsToProject := registeredProjectConfigIdentityMatches(project, mount.ProjectID)
		if !mountBelongsToProject && sameCanonicalProjectPath(mount.TrackerRoot, vaultRoot) {
			return tuskerError(errorInvalidTransition, "project rebind target vault is already mounted by another project", withContext(map[string]any{"project_id": projectID, "conflicting_project_id": mount.ProjectID, "mount_path": mount.MountPath, "target_vault_root": vaultRoot}), withHint("unmount or repair the conflicting workspace mount first; v1 rebind changes the runtime registry only"))
		}
		if mountBelongsToProject && !sameCanonicalProjectPath(mount.TrackerRoot, vaultRoot) {
			return tuskerError(errorInvalidTransition, "project rebind refuses a mounted workspace target that requires cross-filesystem mutation", withContext(map[string]any{"project_id": projectID, "mount_path": mount.MountPath, "tracker_root": mount.TrackerRoot, "target_vault_root": vaultRoot}), withHint("unmount or repair the workspace mount first; v1 rebind changes the runtime registry only"))
		}
	}
	return nil
}

func projectByID(store *RuntimeStore, projectID string) (RegisteredProject, error) {
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true, ProjectID: projectID})
	if err != nil {
		return RegisteredProject{}, err
	}
	for _, project := range loadedRegisteredProjects(loaded) {
		if project.ProjectID == projectID {
			return project, nil
		}
	}
	return RegisteredProject{}, tuskerError(errorNotFound, "project not found: "+projectID)
}

func validateProjectRebindIdentity(repoRoot, vaultRoot string, before RegisteredProject) error {
	resolved, err := resolveTuskerConfigForPaths(repoRoot, vaultRoot, true)
	if err != nil {
		return err
	}
	projectConfigPresent := false
	for _, layer := range resolved.Layers {
		if layer.Name == configSourceProject && layer.Present {
			projectConfigPresent = true
			break
		}
	}
	identity := strings.TrimSpace(resolved.Config.ProjectID)
	if projectConfigPresent && identity != "" && registeredProjectConfigIdentityMatches(before, identity) {
		return nil
	}
	if identity == "" {
		identity = "<missing>"
	}
	return tuskerError(errorInvalidTransition, "project rebind target is missing or belongs to a different project: "+identity, withContext(map[string]any{
		"project_id":        before.ProjectID,
		"project_key":       before.ProjectKey,
		"project_name":      before.Name,
		"target_project_id": identity,
		"target_repo_root":  repoRoot,
		"target_vault_root": vaultRoot,
	}), withHint("rebind to the registered project's canonical vault, or update the target project_id before retrying"))
}

func validateProjectRebindGitCommonDir(sourceRepo, targetRepo string) error {
	sourceCommon, sourceErr := gitCommonDirectory(sourceRepo)
	if sourceErr != nil || strings.TrimSpace(sourceCommon) == "" {
		// A stale registration is the repair case: logical project identity is
		// the remaining authority when the old checkout no longer exists.
		return nil
	}
	targetCommon, targetErr := gitCommonDirectory(targetRepo)
	if targetErr != nil || strings.TrimSpace(targetCommon) == "" {
		return tuskerError(errorConfigInvalid, "project rebind target Git common directory could not be resolved", withPath(targetRepo))
	}
	if sameCanonicalProjectPath(sourceCommon, targetCommon) {
		return nil
	}
	return tuskerError(errorInvalidTransition, "project rebind target is a separate Git repository", withContext(map[string]any{
		"source_repo_root":  sourceRepo,
		"source_common_dir": sourceCommon,
		"target_repo_root":  targetRepo,
		"target_common_dir": targetCommon,
	}), withHint("use a checkout from the registered repository, or repair a stale registration only after confirming the target project identity"))
}

func validateProjectRebindPreconditions(store *RuntimeStore, before RegisteredProject, repoRoot, vaultRoot string, directives []RunDirective) error {
	if before.Enabled {
		return tuskerError(errorInvalidTransition, "project must be disabled before rebind: "+before.ProjectID)
	}
	if sameCanonicalProjectPath(before.RepoRoot, repoRoot) && sameCanonicalProjectPath(before.VaultRoot, vaultRoot) {
		return nil
	}
	if sameCanonicalProjectPath(before.RepoRoot, repoRoot) || sameCanonicalProjectPath(before.VaultRoot, vaultRoot) {
		return tuskerError(errorInvalidArg, "project rebind must change repo_root and vault_root together")
	}
	if len(directives) != 0 {
		return projectRebindActiveDirectivesError(before.ProjectID, directives)
	}
	active, err := store.ListProjectNonTerminalRuns(before.ProjectID)
	if err != nil {
		return err
	}
	if blocking := projectRebindBlockingRuns(active); len(blocking) != 0 {
		return projectRebindNonTerminalRunsError(before.ProjectID, blocking)
	}
	if err := validateProjectRebindQueuedTasks(vaultRoot, active); err != nil {
		return err
	}
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{MetadataOnly: true, LoadDisabled: true})
	if err != nil {
		return err
	}
	for _, other := range loadedRegisteredProjects(loaded) {
		if other.ProjectID != before.ProjectID && (sameCanonicalProjectPath(other.RepoRoot, repoRoot) || sameCanonicalProjectPath(other.VaultRoot, vaultRoot)) {
			return tuskerError(errorInvalidTransition, "project rebind target is already claimed by project "+other.ProjectID)
		}
	}
	return nil
}

func projectRebindRetainedQueueRuns(runs []ProjectNonTerminalRun) []ProjectNonTerminalRun {
	retained := make([]ProjectNonTerminalRun, 0, len(runs))
	for _, run := range runs {
		if !projectRebindRunBlocks(run) {
			retained = append(retained, run)
		}
	}
	return retained
}

func validateProjectRebindQueuedTasks(vaultRoot string, runs []ProjectNonTerminalRun) error {
	for _, run := range projectRebindRetainedQueueRuns(runs) {
		taskID := strings.ToUpper(strings.TrimSpace(run.TaskID))
		if taskID == "" {
			return tuskerError(errorInvalidTransition, "project rebind target is missing the queued task identity", withContext(map[string]any{"run_id": run.RunID, "project_id": run.ProjectID}))
		}
		taskPath := filepath.Join(vaultRoot, "work", "tasks", taskID+".md")
		if !fileExists(taskPath) {
			return tuskerError(errorInvalidTransition, "project rebind target vault is missing queued task "+taskID, withPath(taskPath), withContext(map[string]any{"project_id": run.ProjectID, "run_id": run.RunID, "task_id": taskID}))
		}
	}
	return nil
}

func projectRebindActiveDirectivesError(projectID string, directives []RunDirective) error {
	ids := make([]string, 0, len(directives))
	for _, directive := range directives {
		if id := strings.TrimSpace(directive.RecordID); id != "" {
			ids = append(ids, id)
		}
	}
	return tuskerError(errorInvalidTransition,
		fmt.Sprintf("project rebind requires no non-expired queued run directives; found %d (%s)", len(directives), strings.Join(ids, ", ")),
		withHint("let each queued directive expire or be consumed before retrying the rebind"),
		withContext(map[string]any{"project_id": projectID, "active_directive_count": len(directives), "blocking_directive_ids": ids}))
}

// projectRebindBlockingRuns keeps settled queue rows attached to their
// ProjectID while fencing any run that may still have a worker or resumable
// attempt in flight. Unclaimed rows are safe to carry only when they have no
// ownership or runtime artifacts attached.
func projectRebindBlockingRuns(runs []ProjectNonTerminalRun) []ProjectNonTerminalRun {
	blocking := make([]ProjectNonTerminalRun, 0, len(runs))
	for _, run := range runs {
		if projectRebindRunBlocks(run) {
			blocking = append(blocking, run)
		}
	}
	return blocking
}

func projectRebindRunBlocks(run ProjectNonTerminalRun) bool {
	return isDispatchingLeaseState(run.Status) || strings.TrimSpace(run.AttemptID) != "" ||
		strings.TrimSpace(run.LeaseOwner) != "" || strings.TrimSpace(run.WorkspacePath) != "" ||
		strings.TrimSpace(run.SessionRef) != "" || run.ProcessPID != 0 || run.ProcessPGID != 0
}
