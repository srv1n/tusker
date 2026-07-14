package main

import (
	"path/filepath"
	"testing"
)

func TestServeProjectRegistrationDefaultsAutomationOffAndSettingsCanEnableIt(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := ensureDir(vault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}

	var registered serveActionResult
	servePost(t, server, "/api/projects", `{"repoRoot":"`+repo+`"}`, &registered)
	if !registered.OK || registered.Refused || registered.ProjectID == "" {
		t.Fatalf("expected registration, got %#v", registered)
	}

	projects, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	var added *RegisteredProject
	for i := range projects {
		if projects[i].ProjectID == registered.ProjectID {
			added = &projects[i]
			break
		}
	}
	if added == nil {
		t.Fatalf("registered project missing from store: %#v", projects)
	}
	assertEqual(t, false, added.Enabled, "registration does not opt into daemon automation")
	assertEqual(t, projectHealthDisabled, added.Health, "registered-only project health")

	var summaries []serveProjectSummary
	serveDecode(t, server, "/api/projects", &summaries)
	if len(summaries) != 2 {
		t.Fatalf("expected both registered projects, got %#v", summaries)
	}

	var duplicate serveActionResult
	servePost(t, server, "/api/projects", `{"repoRoot":"`+repo+`"}`, &duplicate)
	if !duplicate.OK || duplicate.Refused || duplicate.ProjectID != registered.ProjectID {
		t.Fatalf("expected idempotent duplicate registration, got %#v", duplicate)
	}

	var enabled serveActionResult
	servePost(t, server, "/api/projects/"+registered.ProjectID+"/automation", `{"enabled":true}`, &enabled)
	if !enabled.OK || enabled.Refused {
		t.Fatalf("expected automation enable, got %#v", enabled)
	}
	projects, err = server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range projects {
		if project.ProjectID == registered.ProjectID {
			assertEqual(t, true, project.Enabled, "settings persisted automation choice")
			assertEqual(t, projectHealthHealthy, project.Health, "enabled project health")
			return
		}
	}
	t.Fatal("enabled project disappeared")
}

func TestServeProjectRegistrationRejectsInvalidPathsWithoutPersisting(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	before, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	var result serveActionResult
	servePost(t, server, "/api/projects", `{"repoRoot":"/path/that/does/not/exist"}`, &result)
	if result.OK || !result.Refused {
		t.Fatalf("expected invalid registration refusal, got %#v", result)
	}
	after, err := server.store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, len(before), len(after), "invalid registration leaves no phantom project")
}

func TestServeProjectSettingsPersistWorkspaceAndConcurrency(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	project := projects[0]
	var workspace serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/settings", `{"workspaceMode":"worktree"}`, &workspace)
	if !workspace.OK {
		t.Fatalf("workspace setting failed: %#v", workspace)
	}
	var concurrency serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/settings", `{"maxActiveRunsPerProject":3}`, &concurrency)
	if !concurrency.OK {
		t.Fatalf("concurrency setting failed: %#v", concurrency)
	}
	wf, err := loadWorkflow(project.VaultRoot)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(WorkspaceStrategyWorktree), wf.Data.Workspace.Strategy, "workspace setting readback")
	assertEqual(t, 3, wf.Data.Runtime.MaxActiveRunsPerProject, "concurrency setting readback")
}
