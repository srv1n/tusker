package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func registryDefaultsProjectFixture(t *testing.T) (string, string) {
	t.Helper()
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	return repo, vault
}

func TestRegistryDefaultsProjectsAddDisabled(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	repo, vault := registryDefaultsProjectFixture(t)
	if err := projectsAddCmd(Args{"repo": repo, "vault": vault}); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered projects: %#v err=%v", projects, err)
	}
	if projects[0].Enabled || projects[0].Health != projectHealthDisabled {
		t.Fatalf("projects add must default to disabled: %#v", projects[0])
	}
}

func TestRegistryDefaultsProjectsEnableAndDisableAutomation(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", filepath.Join(t.TempDir(), "state"))
	repo, vault := registryDefaultsProjectFixture(t)
	if err := projectsAddCmd(Args{"repo": repo, "vault": vault}); err != nil {
		t.Fatal(err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("registered projects: %#v err=%v", projects, err)
	}
	project := projects[0]
	_ = store.Close()

	if err := projectsEnableCmd(Args{"id": project.ProjectID}); err != nil {
		t.Fatal(err)
	}
	assertRegistryAutomationState(t, project, true)
	if err := projectsDisableCmd(Args{"id": project.ProjectID}); err != nil {
		t.Fatal(err)
	}
	assertRegistryAutomationState(t, project, false)
}

func assertRegistryAutomationState(t *testing.T, project RegisteredProject, enabled bool) {
	t.Helper()
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].Enabled != enabled {
		t.Fatalf("registry enabled=%t: %#v", enabled, projects)
	}
	report, err := configResolveForPaths(project.RepoRoot, project.VaultRoot, true, "automation.enabled")
	if err != nil {
		t.Fatal(err)
	}
	if boolFromAny(report.Value) != enabled || report.Source != configSourceLocal {
		t.Fatalf("automation.enabled=%t report=%#v", enabled, report)
	}
}

// A run directive is deliberate human execution authority: it dispatches even
// when project automation is disabled. automation.enabled gates autonomous
// daemon pickup only.
func TestDaemonHonorsDirectiveWithAutomationOff(t *testing.T) {
	vault := automationTestVault(t)
	setAllEligibleDispatchScopeForAutomationTest(t, vault)
	mustRunPickupTest(t, Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Directed", "risk": "low", "priority": "p0", "v7": "true"}, newV7Task)
	makeV7TaskDispatchableForTest(t, vault, "APP-T-0001")
	initializeOrchestrationGitRepo(t, filepath.Dir(vault))
	installFakeCodexExec(t, filepath.Dir(vault))
	project := registerAutomationTestProject(t, vault)
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.enabled", false); err != nil {
		t.Fatal(err)
	}

	daemon, err := NewDaemon(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	diskConfig := defaultDiskPressureConfig()
	diskConfig.Enabled = false
	if err := daemon.store.SetDiskPressureConfig(diskConfig); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queued, err := daemon.store.QueueRunDirective(RunDirective{
		ProjectID: project.ProjectID, RecordID: "APP-T-0001", Actor: "human:test",
		CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano),
	})
	if err != nil || !queued {
		t.Fatalf("queue directive: queued=%t err=%v", queued, err)
	}
	if err := daemon.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := latestRunForRecord(t, daemon.store, project.ProjectID, "APP-T-0001")
	if run.AttemptCount != 1 || run.ActiveAttemptID == "" {
		t.Fatalf("expected the directive to dispatch one attempt, got %#v", run)
	}
	if run.ProcessPGID > 0 {
		t.Cleanup(func() { _ = syscall.Kill(-run.ProcessPGID, syscall.SIGKILL) })
	}
	directive, err := daemon.store.RunDirective(project.ProjectID, "APP-T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if directive == nil || directive.State != "consumed" {
		t.Fatalf("expected consumed directive, got %#v", directive)
	}
}

func TestProjectsListFreshStateDoesNotCreateRuntimeState(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "fresh-state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	output := captureStdout(t, func() {
		if err := projectsListCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "No runtime state yet") {
		t.Fatalf("unexpected fresh-state output: %q", output)
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("projects list created daemon.db: err=%v", err)
	}
}

func TestProjectsListQuarantinesBrokenRegistration(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "state")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	store, err := OpenRuntimeStore(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	project := RegisteredProject{ProjectID: "broken", ProjectKey: "broken", Name: "Broken", RepoRoot: t.TempDir(), VaultRoot: filepath.Join(t.TempDir(), "missing"), Enabled: true, Health: projectHealthHealthy}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	output := captureStdout(t, func() {
		if err := projectsListCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Projects []RegisteredProject `json:"projects"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Projects) != 1 || payload.Projects[0].Health != projectHealthError || payload.Projects[0].LastError == "" {
		t.Fatalf("broken registration was not quarantined: %#v", payload)
	}
}
