package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedCanonicalV7TaskIntoWorkspace(t *testing.T) {
	canonicalVault, workspace, taskID := frozenTaskWorktreeSeedFixture(t)
	canonicalPath := filepath.Join(canonicalVault, "work", "tasks", taskID+".md")
	targetPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	canonical := mustReadIndexTest(t, canonicalPath)

	t.Run("missing record is seeded without lifecycle records", func(t *testing.T) {
		if fileExists(targetPath) {
			t.Fatalf("fixture worktree unexpectedly already has %s", taskID)
		}
		if err := seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, canonical, mustReadIndexTest(t, targetPath), "exact canonical task seed")
		if fileExists(filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "attempts", taskID)) {
			t.Fatal("task seed must not materialize lifecycle attempt records")
		}
	})

	t.Run("existing identical record is a no-op", func(t *testing.T) {
		if err := seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID); err != nil {
			t.Fatal(err)
		}
		assertEqual(t, canonical, mustReadIndexTest(t, targetPath), "identical seed remains unchanged")
	})

	t.Run("runner lifecycle resolves seeded task", func(t *testing.T) {
		attemptID, err := ensureDispatchedV7Attempt(canonicalVault, taskID, "01KXGPRUNTIME0000000000000", runLaneExecute, "codex_exec", workspace, "task/app-t-0001")
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, taskID+"-A-0001", attemptID, "seeded task gets bound runtime attempt")
		if _, err := resolveV7Note(runnerWorktreeVaultPath(workspace, canonicalVault), taskID, "task"); err != nil {
			t.Fatalf("runner worktree cannot resolve seeded task: %v", err)
		}
	})
}

func TestSeedCanonicalV7TaskIntoWorkspaceRefusesConflictingRecord(t *testing.T) {
	canonicalVault, workspace, taskID := frozenTaskWorktreeSeedFixture(t)
	targetPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	if err := ensureDir(filepath.Dir(targetPath)); err != nil {
		t.Fatal(err)
	}
	data, body, err := parseFrontmatterMustRead(filepath.Join(canonicalVault, "work", "tasks", taskID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	data["title"] = "Conflicting frozen task"
	data["state_rev"] = v7StateRev(data, body)
	conflict, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(conflict), 0o644); err != nil {
		t.Fatal(err)
	}

	err = seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID)
	if err == nil || !strings.Contains(err.Error(), "differs from the canonical") {
		t.Fatalf("expected canonical conflict refusal, got %v", err)
	}
	assertEqual(t, conflict, mustReadIndexTest(t, targetPath), "conflicting record must not be overwritten")
}

func TestSeedCanonicalV7TaskForPreparedWorkspaceDoesNotOverwriteReusedLifecycleState(t *testing.T) {
	canonicalVault, workspace, taskID := frozenTaskWorktreeSeedFixture(t)
	prepared := WorkspacePrepareResult{Path: workspace, NewlyMaterialized: true}
	if err := seedCanonicalV7TaskForPreparedWorkspace(canonicalVault, prepared, WorkspaceStrategyWorktree, runLaneExecute, taskID); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	data, body, err := parseFrontmatterMustRead(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	data["status"] = "review"
	data["readiness"] = "waiting_on_review"
	data["next_owner"] = "reviewer"
	data["state_rev"] = v7StateRev(data, body)
	localReview, err := serializeDocument(data, body, v7FrontmatterOrder["task"])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte(localReview), 0o644); err != nil {
		t.Fatal(err)
	}

	// A review/rework dispatch reuses the same workspace. It must retain the
	// execute lane's local lifecycle output instead of comparing it to the
	// canonical ready record or seeding again.
	prepared.NewlyMaterialized = false
	if err := seedCanonicalV7TaskForPreparedWorkspace(canonicalVault, prepared, WorkspaceStrategyWorktree, runLaneReview, taskID); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, localReview, mustReadIndexTest(t, targetPath), "reused review workspace retains local task state")
}

func TestSyncCanonicalV7TaskIntoWorkspaceRefreshesExecuteSnapshot(t *testing.T) {
	canonicalVault, workspace, taskID := frozenTaskWorktreeSeedFixture(t)
	if err := seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID); err != nil {
		t.Fatal(err)
	}
	canonicalPath := filepath.Join(canonicalVault, "work", "tasks", taskID+".md")
	canonicalData, canonicalBody, err := parseFrontmatterMustRead(canonicalPath)
	if err != nil {
		t.Fatal(err)
	}
	canonicalData["readiness"] = "ready"
	canonicalData["next_owner"] = "agent"
	canonicalData["next_source"] = "task"
	canonicalData["next_ref"] = taskID
	if _, err := saveV7DocumentCAS(canonicalPath, canonicalData, canonicalBody, v7FrontmatterOrder["task"], stringField(canonicalData, "state_rev")); err != nil {
		t.Fatal(err)
	}
	want := mustReadIndexTest(t, canonicalPath)

	targetPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	localData, localBody, err := parseFrontmatterMustRead(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	localData["status"] = "review"
	localData["readiness"] = "waiting_on_review"
	if _, err := saveV7DocumentCAS(targetPath, localData, localBody, v7FrontmatterOrder["task"], stringField(localData, "state_rev")); err != nil {
		t.Fatal(err)
	}

	if err := syncCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID); err != nil {
		t.Fatal(err)
	}
	assertEqual(t, want, mustReadIndexTest(t, targetPath), "execute snapshot refreshed from canonical authority")
}

func TestSeedCanonicalV7TaskForPreparedWorkspaceLeavesFreshReviewSnapshotUntouched(t *testing.T) {
	canonicalVault, workspace, taskID := frozenTaskWorktreeSeedFixture(t)
	targetPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	prepared := WorkspacePrepareResult{Path: workspace, NewlyMaterialized: true}
	if err := seedCanonicalV7TaskForPreparedWorkspace(canonicalVault, prepared, WorkspaceStrategyWorktree, runLaneReview, taskID); err != nil {
		t.Fatal(err)
	}
	if fileExists(targetPath) {
		t.Fatal("fresh review workspace must not receive canonical task state")
	}
}

func TestSeedCanonicalV7TaskIntoWorkspaceRejectsInvalidTaskID(t *testing.T) {
	canonicalVault, workspace, _ := frozenTaskWorktreeSeedFixture(t)
	err := seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, "../../escape")
	if err == nil || !strings.Contains(err.Error(), "valid V7 task identity") {
		t.Fatalf("expected invalid task identity refusal, got %v", err)
	}
}

func frozenTaskWorktreeSeedFixture(t *testing.T) (canonicalVault, workspace, taskID string) {
	t.Helper()
	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	canonicalVault = filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": canonicalVault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(canonicalVault); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": canonicalVault, "quiet": "true", "acronym": "APP", "title": "App", "summary": "App work.", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".tusker")
	runGitDir(t, repo, "commit", "-m", "frozen task base")
	if err := newV7Task(Args{"vault": canonicalVault, "quiet": "true", "epic": "APP", "title": "Imported after freeze", "risk": "low", "priority": "p1", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	taskID = "APP-T-0001"
	workspace = filepath.Join(t.TempDir(), "worker")
	runGitDir(t, repo, "worktree", "add", "-b", "task/app-t-0001", workspace, "HEAD")
	t.Cleanup(func() { runGitDir(t, repo, "worktree", "remove", "--force", workspace) })
	return canonicalVault, workspace, taskID
}
