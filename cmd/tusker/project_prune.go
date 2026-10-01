package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

type projectPruneReport struct {
	DryRun          bool                                `json:"dry_run"`
	RemovedProjects []string                            `json:"removed_projects"`
	RemovedMounts   []string                            `json:"removed_mounts"`
	Registry        registeredProjectRegistryInspection `json:"registry"`
}

// projectsPruneCmd previews registrations whose tracker roots no longer exist.
// --apply is required to mutate the registry or workspace mounts.
func projectsPruneCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()

	dryRun := !args.Bool("apply") || args.Bool("dry-run")
	report, err := pruneMissingRegisteredProjects(store, dryRun)
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "prune": report})
		return nil
	}
	verb := "Pruned"
	if report.DryRun {
		verb = "Would prune"
	}
	fmt.Printf("%s %d dead project registration(s) and %d dangling mount(s).\n", verb, len(report.RemovedProjects), len(report.RemovedMounts))
	for _, projectID := range report.RemovedProjects {
		fmt.Printf("  project %s\n", projectID)
	}
	for _, ids := range report.Registry.RelatedWorktreeGroups {
		fmt.Printf("  related worktrees (kept separate): %s\n", strings.Join(ids, ", "))
	}
	for _, projectID := range report.Registry.MissingRegistrations {
		fmt.Printf("  missing registration: %s\n", projectID)
	}
	return nil
}

func pruneMissingRegisteredProjects(store *RuntimeStore, dryRun bool) (projectPruneReport, error) {
	report := projectPruneReport{DryRun: dryRun}
	projects, err := store.ListProjects()
	if err != nil {
		return report, err
	}
	report.Registry = inspectRegisteredProjectRegistry(projects)

	missing := make([]RegisteredProject, 0)
	for _, project := range projects {
		// A root that cannot be statted (a macOS privacy block) is not proof
		// the project is gone, and must not stop the other registrations.
		if isMissing, err := registeredProjectTrackerRootMissing(project); err == nil && isMissing {
			missing = append(missing, project)
		}
	}
	if len(missing) == 0 {
		return report, nil
	}
	// A tracker root that is gone can host no new work, so only a run with a
	// live owner or process holds its registration. Leftover workspace paths
	// and directives of finished runs do not, and one held project must not
	// keep the rest of the registry dead.
	removable := missing[:0]
	for _, project := range missing {
		live, err := missingProjectHasLiveRun(store, project.ProjectID)
		if err != nil {
			return report, err
		}
		if !live {
			removable = append(removable, project)
		}
	}
	missing = removable
	if dryRun {
		for _, project := range missing {
			report.RemovedProjects = append(report.RemovedProjects, project.ProjectID)
		}
	}

	workspace, err := loadWorkspaceVaultConfig()
	if err != nil {
		return report, err
	}
	hasWorkspaceVault := strings.TrimSpace(workspace.ObsidianVault) != ""
	if hasWorkspaceVault {
		workspace.refreshMountPaths()
	}
	retained := make([]WorkspaceProject, 0, len(workspace.Projects))
	workspaceChanged := false
	for _, mount := range workspace.Projects {
		project, matched := missingProjectForWorkspaceMount(missing, mount)
		if !matched {
			retained = append(retained, mount)
			continue
		}
		stillMissing, err := registeredProjectTrackerRootMissing(project)
		if err != nil {
			return report, err
		}
		if !stillMissing {
			retained = append(retained, mount)
			continue
		}
		workspaceChanged = true
		if hasWorkspaceVault {
			removed, err := removeDanglingWorkspaceMount(mount.MountPath, project.VaultRoot, dryRun)
			if err != nil {
				return report, err
			}
			if removed {
				report.RemovedMounts = append(report.RemovedMounts, mount.MountPath)
			}
		}
	}
	if !dryRun && workspaceChanged {
		workspace.Projects = retained
		if err := saveWorkspaceVaultConfig(workspace); err != nil {
			return report, err
		}
	}
	if dryRun {
		return report, nil
	}
	for _, project := range missing {
		stillMissing, err := registeredProjectTrackerRootMissing(project)
		if err != nil {
			return report, err
		}
		if !stillMissing {
			continue
		}
		if err := store.UnregisterProject(project.ProjectID); err != nil {
			return report, err
		}
		report.RemovedProjects = append(report.RemovedProjects, project.ProjectID)
	}
	return report, nil
}

func registeredProjectTrackerRootMissing(project RegisteredProject) (bool, error) {
	root := strings.TrimSpace(project.VaultRoot)
	if root == "" {
		return false, tuskerError(errorConfigInvalid, "registered project tracker root is empty", withContext(map[string]any{"project_id": project.ProjectID}))
	}
	_, err := os.Stat(root)
	if err == nil {
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, fmt.Errorf("stat registered project tracker root %s: %w", root, err)
}

func missingProjectForWorkspaceMount(missing []RegisteredProject, mount WorkspaceProject) (RegisteredProject, bool) {
	for _, project := range missing {
		if strings.TrimSpace(mount.ProjectID) != "" && mount.ProjectID == project.ProjectID {
			return project, true
		}
		if sameCleanPath(mount.TrackerRoot, project.VaultRoot) {
			return project, true
		}
	}
	return RegisteredProject{}, false
}

func removeDanglingWorkspaceMount(mountPath, trackerRoot string, dryRun bool) (bool, error) {
	if _, err := os.Stat(trackerRoot); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat registered project tracker root %s: %w", trackerRoot, err)
	}
	info, err := os.Lstat(mountPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	target, err := os.Readlink(mountPath)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(mountPath), target)
	}
	if !sameCleanPath(target, trackerRoot) {
		return false, nil
	}
	if !dryRun {
		if err := os.Remove(mountPath); err != nil {
			return false, err
		}
	}
	return true, nil
}

// sweepRegisteredProjects keeps the registry to projects that still exist:
// a registration whose tracker root is gone is removed (re-run `tusker init`
// to bring it back), and a load error that no longer reproduces is cleared.
// It never enables a project or schedules work.
func sweepRegisteredProjects(store *RuntimeStore) {
	report, err := pruneMissingRegisteredProjects(store, false)
	if err != nil {
		log.Printf("project registry sweep: prune: %v", err)
	} else if len(report.RemovedProjects) > 0 {
		log.Printf("project registry sweep: removed=%s", strings.Join(report.RemovedProjects, ","))
	}
	if err := healRegisteredProjects(store); err != nil {
		log.Printf("project registry sweep: heal: %v", err)
	}
}

// healRegisteredProjects clears a recorded load error once the project loads
// again. Disabled projects are never reloaded by the daemon, so without this a
// transient failure (a repo reset mid-flight, a sandboxed read) sticks forever.
func healRegisteredProjects(store *RuntimeStore) error {
	loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{LoadDisabled: true})
	if err != nil {
		return err
	}
	for _, item := range loaded {
		if !item.Loadable() || item.Project.Health != projectHealthError {
			continue
		}
		health := projectHealthHealthy
		if !item.Project.Enabled {
			health = projectHealthDisabled
		}
		if _, err := store.SetProjectHealth(item.Project.ProjectID, item.Project.VaultRoot, health, ""); err != nil {
			return err
		}
	}
	return nil
}

func missingProjectHasLiveRun(store *RuntimeStore, projectID string) (bool, error) {
	runs, err := store.ListProjectNonTerminalRuns(projectID)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if isDispatchingLeaseState(run.Status) || strings.TrimSpace(run.AttemptID) != "" ||
			strings.TrimSpace(run.LeaseOwner) != "" || run.ProcessPID != 0 || run.ProcessPGID != 0 {
			return true, nil
		}
	}
	return false, nil
}
