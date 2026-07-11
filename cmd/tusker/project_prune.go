package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type projectPruneReport struct {
	DryRun          bool     `json:"dry_run"`
	RemovedProjects []string `json:"removed_projects"`
	RemovedMounts   []string `json:"removed_mounts"`
}

// projectsPruneCmd removes registrations whose tracker roots no longer exist.
// It also clears matching workspace configuration and only removes a vault
// mount when that mount is a symlink to the vanished tracker root.
func projectsPruneCmd(args Args) error {
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()

	report, err := pruneMissingRegisteredProjects(store, args.Bool("dry-run"))
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
	return nil
}

func pruneMissingRegisteredProjects(store *RuntimeStore, dryRun bool) (projectPruneReport, error) {
	report := projectPruneReport{DryRun: dryRun}
	projects, err := store.ListProjects()
	if err != nil {
		return report, err
	}

	missing := make([]RegisteredProject, 0)
	for _, project := range projects {
		isMissing, err := registeredProjectTrackerRootMissing(project)
		if err != nil {
			return report, err
		}
		if isMissing {
			missing = append(missing, project)
			report.RemovedProjects = append(report.RemovedProjects, project.ProjectID)
		}
	}
	if len(missing) == 0 {
		return report, nil
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
		if err := store.RemoveProject(project.ProjectID); err != nil {
			return report, err
		}
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
