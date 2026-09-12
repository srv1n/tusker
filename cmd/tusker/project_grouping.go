package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// registeredProjectGroup is a navigation-only view. The registrations inside
// it remain independent runtime identities and keep their own vaults/history.
type registeredProjectGroup struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	IdentityKey string              `json:"identity_key"`
	Checkouts   []RegisteredProject `json:"checkouts"`
}

type registeredCheckoutInspection struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	RepoRoot   string `json:"repo_root"`
	VaultRoot  string `json:"vault_root"`
	Branch     string `json:"branch,omitempty"`
	Head       string `json:"head,omitempty"`
	Git        bool   `json:"git"`
	Detached   bool   `json:"detached"`
	Available  bool   `json:"available"`
	Activity   string `json:"activity"`
	ActiveRuns int    `json:"active_runs"`
	Health     string `json:"health"`
	Error      string `json:"error,omitempty"`
}

type registeredProjectRegistryInspection struct {
	Groups                []registeredProjectGroup `json:"groups"`
	DuplicatePathAliases  []string                 `json:"duplicate_path_aliases"`
	RelatedWorktreeGroups [][]string               `json:"related_worktree_groups"`
	MissingRegistrations  []string                 `json:"missing_registrations"`
	SeparateRepositoryIDs []string                 `json:"separate_repository_ids"`
}

func groupRegisteredProjects(projects []RegisteredProject) []registeredProjectGroup {
	groups := make([]registeredProjectGroup, 0, len(projects))
	byKey := make(map[string]int, len(projects))
	for _, project := range projects {
		key := registeredProjectIdentityKey(project)
		index, ok := byKey[key]
		if !ok {
			byKey[key] = len(groups)
			groups = append(groups, registeredProjectGroup{
				ID:          project.ProjectID,
				Name:        firstNonEmpty(project.Name, project.ProjectKey, filepath.Base(project.RepoRoot)),
				IdentityKey: key,
				Checkouts:   []RegisteredProject{project},
			})
			continue
		}
		groups[index].Checkouts = append(groups[index].Checkouts, project)
	}
	return groups
}

func registeredProjectIdentityKey(project RegisteredProject) string {
	if project.RepositoryKey != "" {
		return project.RepositoryKey
	}
	return registeredProjectRepositoryKey(project.RepoRoot)
}

func registeredProjectRepositoryKey(repoRoot string) string {
	if common, err := gitCommonDirectory(repoRoot); err == nil && common != "" {
		return "git:" + common
	}
	return "path:" + canonicalProjectPath(repoRoot)
}

func inspectRegisteredCheckout(project RegisteredProject, label string, activeRuns int) registeredCheckoutInspection {
	inspection := registeredCheckoutInspection{
		ID:         project.ProjectID,
		Label:      firstNonEmpty(label, filepath.Base(project.RepoRoot), project.ProjectID),
		RepoRoot:   project.RepoRoot,
		VaultRoot:  project.VaultRoot,
		ActiveRuns: activeRuns,
		Health:     string(project.Health),
		Activity:   "Idle",
	}
	if activeRuns > 0 {
		inspection.Activity = "Active"
	}
	if _, err := os.Stat(project.RepoRoot); err != nil {
		inspection.Available = false
		inspection.Activity = "Unavailable"
		inspection.Error = err.Error()
		return inspection
	}
	if _, err := os.Stat(project.VaultRoot); err != nil {
		inspection.Available = false
		inspection.Activity = "Unavailable"
		inspection.Error = err.Error()
		return inspection
	}
	inspection.Available = true
	if _, err := gitFactOutput(project.RepoRoot, "rev-parse", "--is-inside-work-tree"); err != nil {
		return inspection
	}
	inspection.Git = true
	if branch, err := gitFactOutput(project.RepoRoot, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && branch != "" {
		inspection.Branch = branch
		return inspection
	}
	if head, err := gitFactOutput(project.RepoRoot, "rev-parse", "--short", "HEAD"); err == nil {
		inspection.Head = head
		inspection.Detached = true
	}
	return inspection
}

func inspectRegisteredProjectRegistry(projects []RegisteredProject) registeredProjectRegistryInspection {
	groups := groupRegisteredProjects(projects)
	report := registeredProjectRegistryInspection{Groups: groups}
	pathOwners := map[string][]string{}
	nameGroups := map[string][]string{}
	for _, project := range projects {
		pathOwners[canonicalProjectPath(project.RepoRoot)] = append(pathOwners[canonicalProjectPath(project.RepoRoot)], project.ProjectID)
	}
	for path, ids := range pathOwners {
		if path != "" && len(ids) > 1 {
			report.DuplicatePathAliases = append(report.DuplicatePathAliases, strings.Join(ids, ","))
		}
	}
	for _, group := range groups {
		if len(group.Checkouts) > 1 && strings.HasPrefix(group.IdentityKey, "git:") {
			ids := make([]string, 0, len(group.Checkouts))
			for _, project := range group.Checkouts {
				ids = append(ids, project.ProjectID)
			}
			report.RelatedWorktreeGroups = append(report.RelatedWorktreeGroups, ids)
		}
		for _, project := range group.Checkouts {
			if _, err := os.Stat(project.RepoRoot); err != nil || func() bool { _, statErr := os.Stat(project.VaultRoot); return statErr != nil }() {
				report.MissingRegistrations = append(report.MissingRegistrations, project.ProjectID)
			}
		}
		name := strings.ToLower(strings.TrimSpace(group.Name))
		if name != "" {
			nameGroups[name] = append(nameGroups[name], group.ID)
		}
	}
	for _, ids := range nameGroups {
		if len(ids) > 1 {
			report.SeparateRepositoryIDs = append(report.SeparateRepositoryIDs, ids...)
		}
	}
	sort.Strings(report.DuplicatePathAliases)
	sort.Strings(report.MissingRegistrations)
	sort.Strings(report.SeparateRepositoryIDs)
	return report
}

func formatRegisteredCheckout(inspection registeredCheckoutInspection) string {
	branch := inspection.Branch
	if inspection.Detached {
		branch = fmt.Sprintf("Detached · %s", firstNonEmpty(inspection.Head, "unknown"))
	}
	if branch == "" && !inspection.Git {
		branch = "Non-Git"
	}
	return fmt.Sprintf("%s (%s) %s", inspection.ID, firstNonEmpty(branch, "unknown"), inspection.Activity)
}
