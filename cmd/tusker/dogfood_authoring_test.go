package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDogfoodAuthoringPlanRefs(t *testing.T) {
	vault := automationTestVault(t)
	if err := writeText(filepath.Join(v7RepoRoot(vault), "docs/plans/x.md"), "# Plan\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(v7RepoRoot(vault), "docs/system/proposals/broken.md"), "# Broken\n"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ ref, want, hint string }{
		{"docs/plans/x.md", "outside docs/system", "docs/system/proposals/x.md"},
		{"docs/system/proposals/broken.md", "missing target", "kind: spec|proposal|decision"},
		{"docs/plans/missing.md", "missing target", ""},
		{"../outside.md", "path escapes", ""},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			message, hint := v7SpecRefError(vault, tc.ref, nil)
			if !strings.Contains(message, tc.want) {
				t.Fatalf("want %q, got %q", tc.want, message)
			}
			if tc.hint != "" && !strings.Contains(hint, tc.hint) {
				t.Fatalf("want hint %q, got %q", tc.hint, hint)
			}
		})
	}
}

func TestDogfoodAuthoringOwnedPaths(t *testing.T) {
	for _, status := range []string{"idea", "backlog", "ready", "rework"} {
		t.Run(status, func(t *testing.T) {
			vault := automationTestVault(t)
			err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Unscoped task", "risk": "low", "status": status})
			if status == "ready" || status == "rework" {
				if err == nil || !strings.Contains(err.Error(), "task needs at least one owned path") {
					t.Fatalf("ready creation = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			err = editCmd(Args{"vault": vault, "id": "APP-T-0001", "status": "ready", "by": "agent:author"})
			issue, ok := err.(*TuskerError)
			if !ok || !strings.Contains(issue.Error(), "task needs at least one owned path") {
				t.Fatalf("ready edit = %v", err)
			}
			if !strings.Contains(issue.Hint, "tusker edit APP-T-0001 --owned-paths <csv> --by <actor>") {
				t.Fatalf("hint = %q", issue.Hint)
			}
		})
	}
}

func TestDogfoodAuthoringStartBacklog(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
	setAutomationV7TaskFields(t, vault, "APP-T-0001", map[string]any{"status": "backlog", "readiness": "held"})
	t.Chdir(project.RepoRoot)
	t.Setenv("CODEX_THREAD_ID", "dogfood-authoring")
	t.Setenv("TUSKER_ATTEMPT_ID", "")
	captureStdout(t, func() {
		if err := startCmd(Args{"vault": vault, "id": "APP-T-0001", "by": "agent:author", "source": "codex", "current-workspace": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	note, err := resolveV7Note(vault, "APP-T-0001", "task")
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "backlog", stringField(note.Data, "status"), "canonical backlog status")
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || run == nil || run.LeaseState != string(LeaseStateClaimed) || run.WorkspacePath != project.RepoRoot {
		t.Fatalf("current workspace claim: run=%#v err=%v", run, err)
	}
}
