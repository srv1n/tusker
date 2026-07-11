package main

import (
	"strings"
)

type registeredProjectLoadOptions struct {
	Notes        bool
	MetadataOnly bool
	LoadDisabled bool
	ProjectID    string
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
		if opts.ProjectID != "" && project.ProjectID != opts.ProjectID {
			continue
		}
		loaded := loadedRegisteredProject{Project: project}
		if (!project.Enabled && !opts.LoadDisabled) || opts.MetadataOnly {
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

// loadProjectContents keeps every project-backed workflow/note read behind the
// same loader boundary. Registered projects retain quarantine semantics; the
// implicit single-repo project used by `tusker serve --repo` remains loadable.
func loadProjectContents(store *RuntimeStore, project RegisteredProject, notes bool) (loadedRegisteredProject, error) {
	if strings.TrimSpace(project.ProjectID) != "" {
		loaded, err := loadRegisteredProjects(store, registeredProjectLoadOptions{
			Notes:        notes,
			LoadDisabled: true,
			ProjectID:    project.ProjectID,
		})
		if err != nil {
			return loadedRegisteredProject{}, err
		}
		if len(loaded) == 0 {
			return loadedRegisteredProject{}, tuskerError(errorConfigInvalid, "registered project was not found")
		}
		if loaded[0].LoadError != nil {
			return loaded[0], loaded[0].LoadError
		}
		return loaded[0], nil
	}

	loaded := loadedRegisteredProject{Project: project}
	wfFile, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		return loaded, err
	}
	loaded.Workflow = wfFile
	if notes {
		loaded.Notes, err = listAllNotes(project.VaultRoot)
		if err != nil {
			return loaded, err
		}
	}
	return loaded, nil
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
