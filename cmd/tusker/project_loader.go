package main

import (
	"strings"
)

type registeredProjectLoadOptions struct {
	Notes bool
}

type loadedRegisteredProject struct {
	Project   RegisteredProject
	Workflow  WorkflowFile
	Notes     []Note
	LoadError error
}

func (p loadedRegisteredProject) Loadable() bool {
	return p.Project.Enabled && p.LoadError == nil
}

func loadRegisteredProjects(store *RuntimeStore, opts registeredProjectLoadOptions) ([]loadedRegisteredProject, error) {
	if store == nil {
		return nil, tuskerError(errorConfigInvalid, "runtime store is required")
	}
	projects, err := store.ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]loadedRegisteredProject, 0, len(projects))
	for _, project := range projects {
		loaded := loadedRegisteredProject{Project: project}
		if !project.Enabled {
			out = append(out, loaded)
			continue
		}
		wfFile, err := loadWorkflow(project.VaultRoot)
		if err != nil {
			loaded.Project, err = quarantineRegisteredProjectLoadError(store, project, err)
			if err != nil {
				return nil, err
			}
			loaded.LoadError = loaded.Project.LastErrorAsError()
			out = append(out, loaded)
			continue
		}
		loaded.Workflow = wfFile
		if opts.Notes {
			notes, err := listAllNotes(project.VaultRoot)
			if err != nil {
				loaded.Project, err = quarantineRegisteredProjectLoadError(store, project, err)
				if err != nil {
					return nil, err
				}
				loaded.LoadError = loaded.Project.LastErrorAsError()
				out = append(out, loaded)
				continue
			}
			loaded.Notes = notes
		}
		out = append(out, loaded)
	}
	return out, nil
}

func quarantineRegisteredProjectLoadError(store *RuntimeStore, project RegisteredProject, loadErr error) (RegisteredProject, error) {
	if loadErr == nil {
		return project, nil
	}
	project.Health = projectHealthError
	project.LastError = loadErr.Error()
	if store == nil {
		return project, nil
	}
	if err := store.UpsertProject(project); err != nil {
		return project, err
	}
	return project, nil
}

func loadedRegisteredProjects(loaded []loadedRegisteredProject) []RegisteredProject {
	projects := make([]RegisteredProject, 0, len(loaded))
	for _, item := range loaded {
		projects = append(projects, item.Project)
	}
	return projects
}

func loadableRegisteredProjects(loaded []loadedRegisteredProject) []RegisteredProject {
	projects := make([]RegisteredProject, 0, len(loaded))
	for _, item := range loaded {
		if item.Loadable() {
			projects = append(projects, item.Project)
		}
	}
	return projects
}

func projectQuarantinedError(project RegisteredProject) error {
	label := firstNonEmpty(registeredProjectLabel(project), project.ProjectID, project.RepoRoot)
	message := "registered project is quarantined: " + label
	if strings.TrimSpace(project.LastError) != "" {
		message += ": " + project.LastError
	}
	return tuskerError(errorConfigInvalid, message, withContext(map[string]any{
		"project_id": project.ProjectID,
		"repo_root":  project.RepoRoot,
		"vault_root": project.VaultRoot,
		"last_error": project.LastError,
	}))
}

func (p RegisteredProject) LastErrorAsError() error {
	if strings.TrimSpace(p.LastError) == "" {
		return nil
	}
	return projectQuarantinedError(p)
}
