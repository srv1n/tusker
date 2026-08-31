package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeProjectsExposeAdaptiveReconciliationStatus(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	now := time.Date(2026, 7, 15, 6, 0, 0, 0, time.UTC)
	server.reconcileStatus = func(projectID string) adaptiveProjectReconcileStatus {
		return adaptiveProjectReconcileStatus{Tier: "warm", CadenceMS: reconcileWarmCadence.Milliseconds(), LastActivityAt: now.Format(time.RFC3339Nano), NextDueAt: now.Add(reconcileWarmCadence).Format(time.RFC3339Nano)}
	}
	var summaries []serveProjectSummary
	serveDecode(t, server, "/api/projects", &summaries)
	if len(summaries) == 0 {
		t.Fatal("expected project summaries")
	}
	if summaries[0].Reconciliation.Tier != "warm" || summaries[0].Reconciliation.CadenceMS != reconcileWarmCadence.Milliseconds() {
		t.Fatalf("adaptive reconciliation status missing: %#v", summaries[0].Reconciliation)
	}
	if summaries[0].DispatchScope.Effective != string(automationDispatchScopeArmedWaves) || summaries[0].DispatchScope.Provenance == "" {
		t.Fatalf("dispatch scope projection missing from Serve: %#v", summaries[0].DispatchScope)
	}
}

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

func TestServeProjectRebindUsesCanonicalCommandAndDefaultsVault(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, _ := projectRebindFixtureRepo(t, "new")
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	if err := server.store.RemoveProject(project.ProjectID); err != nil {
		t.Fatal(err)
	}
	project.ProjectID = "project-rebind"
	project.RepoRoot, project.VaultRoot, project.WorkflowPath = oldRepo, oldVault, workflowPath(oldVault)
	project.Enabled, project.Health = false, projectHealthDisabled
	if err := server.store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := server.loadSnapshotForProject(project.ProjectID); err != nil || snapshot.project.VaultRoot != oldVault {
		t.Fatalf("warm snapshot=%#v err=%v", snapshot.project, err)
	}
	projectRebindMarkSourceStale(t, oldRepo)

	var preview serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/rebind", `{"repoRoot":"`+newRepo+`","dryRun":true}`, &preview)
	if !preview.OK || preview.Refused || preview.Rebind == nil || !preview.Rebind.DryRun {
		t.Fatalf("expected structured rebind preview, got %#v", preview)
	}
	if stored, err := projectByID(server.store, project.ProjectID); err != nil || stored.RepoRoot != oldRepo {
		t.Fatalf("preview changed registry project=%#v err=%v", stored, err)
	}

	var result serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/rebind", `{"repoRoot":"`+newRepo+`"}`, &result)
	if !result.OK || result.Refused || result.ProjectID != project.ProjectID || result.Rebind == nil {
		t.Fatalf("expected structured rebind success, got %#v", result)
	}
	if result.Rebind.After.RepoRoot != newRepo || result.Rebind.After.VaultRoot != filepath.Join(newRepo, ".tusker") {
		t.Fatalf("rebind defaults not reflected: %#v", result.Rebind)
	}
	if snapshot, err := server.loadSnapshotForProject(project.ProjectID); err != nil || snapshot.project.VaultRoot != filepath.Join(newRepo, ".tusker") {
		t.Fatalf("snapshot did not follow rebound project=%#v err=%v", snapshot.project, err)
	}
}

func TestServeProjectRebindRefusesDirtyTargetByDefault(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	oldRepo, oldVault := projectRebindFixtureRepo(t, "old")
	newRepo, _ := projectRebindFixtureRepo(t, "new")
	if err := writeText(filepath.Join(newRepo, "uncommitted.txt"), "keep me\n"); err != nil {
		t.Fatal(err)
	}
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture projects=%#v err=%v", projects, err)
	}
	project := projects[0]
	if err := server.store.RemoveProject(project.ProjectID); err != nil {
		t.Fatal(err)
	}
	project.ProjectID = "project-rebind"
	project.RepoRoot, project.VaultRoot, project.WorkflowPath = oldRepo, oldVault, workflowPath(oldVault)
	project.Enabled, project.Health = false, projectHealthDisabled
	if err := server.store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	projectRebindMarkSourceStale(t, oldRepo)

	var result serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/rebind", `{"repoRoot":"`+newRepo+`"}`, &result)
	if result.OK || !result.Refused || result.Issue == nil || !strings.Contains(result.Issue.Message, "must be clean") {
		t.Fatalf("dirty target refusal was not structured: %#v", result)
	}
	result = serveActionResult{}
	servePost(t, server, "/api/projects/"+project.ProjectID+"/rebind", `{"repoRoot":"`+newRepo+`","allowDirty":true}`, &result)
	if result.OK || !result.Refused || result.Issue == nil || !strings.Contains(result.Issue.Message, "exact confirmation token") {
		t.Fatalf("dirty confirmation refusal was not structured: %#v", result)
	}
	result = serveActionResult{}
	servePost(t, server, "/api/projects/"+project.ProjectID+"/rebind", `{"repoRoot":"`+newRepo+`","allowDirty":true,"confirm":"ALLOW DIRTY"}`, &result)
	if !result.OK || result.Refused || result.Rebind == nil || !result.Rebind.AllowDirty {
		t.Fatalf("explicit dirty opt-in failed: %#v", result)
	}
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
