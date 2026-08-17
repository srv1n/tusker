package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectsAddTierOneAllowsMissingWorkflow(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 1); err != nil {
		t.Fatal(err)
	}
	if fileExists(workflowPath(vault)) {
		t.Fatal("test fixture unexpectedly contains WORKFLOW.md")
	}

	output := captureStdout(t, func() {
		if err := projectsAddCmd(Args{"repo": repo, "vault": vault}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "WORKFLOW.md absent or invalid") {
		t.Fatalf("tier-one add did not note missing workflow:\n%s", output)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 || projects[0].RepoRoot != resolvedRepo {
		t.Fatalf("tier-one add did not register project: %#v", projects)
	}
}

func TestProjectsAddAbsentTierKeepsMissingWorkflowStrict(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}

	err := projectsAddCmd(Args{"repo": repo, "vault": vault})
	if err == nil || !strings.Contains(err.Error(), "WORKFLOW.md not found") {
		t.Fatalf("absent tier should keep missing workflow strict, got %v", err)
	}

	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	projects, err := store.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("strict add persisted a project: %#v", projects)
	}
}

func TestProjectsAddTierTwoKeepsMissingWorkflowStrict(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "tier", 2); err != nil {
		t.Fatal(err)
	}

	err := projectsAddCmd(Args{"repo": repo, "vault": vault})
	if err == nil || !strings.Contains(err.Error(), "WORKFLOW.md not found") {
		t.Fatalf("tier-two add should keep missing workflow strict, got %v", err)
	}
}
