package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectCompletedWorktreeReviewToCanonical(t *testing.T) {
	t.Run("projects exact reviewed source", func(t *testing.T) {
		canonicalVault, workspace, taskID, run, sourceSHA := preparedWorktreeReviewProjection(t)
		projected, err := projectCompletedWorktreeReviewToCanonical(canonicalVault, run)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, sourceSHA, projected, "projected implementation source")
		canonical, err := resolveV7Note(canonicalVault, taskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, "review", stringField(canonical.Data, "status"), "canonical review status")
		assertEqual(t, "satisfied", stringField(canonical.Data, "proof_status"), "canonical proof state")
		assertEqual(t, sourceSHA, stringField(canonical.Data, "source_sha"), "canonical exact source")
		if !strings.Contains(canonical.Body, "worker verification") {
			t.Fatal("canonical projection lost worktree verification")
		}
		if branch, err := currentGitBranchIn(workspace); err != nil || !strings.EqualFold(branch, v7TaskBranchName(taskID)) {
			t.Fatalf("fixture implementation branch drifted: %q %v", branch, err)
		}
	})

	t.Run("refuses canonical drift", func(t *testing.T) {
		canonicalVault, _, taskID, run, _ := preparedWorktreeReviewProjection(t)
		canonical, err := resolveV7Note(canonicalVault, taskID, "task")
		if err != nil {
			t.Fatal(err)
		}
		data, body, err := parseFrontmatterMustRead(canonical.AbsolutePath)
		if err != nil {
			t.Fatal(err)
		}
		data["title"] = "Human changed this after dispatch"
		if _, err := saveV7DocumentCAS(canonical.AbsolutePath, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
			t.Fatal(err)
		}
		if _, err := projectCompletedWorktreeReviewToCanonical(canonicalVault, run); err == nil || !strings.Contains(err.Error(), "changed after dispatch") {
			t.Fatalf("expected canonical drift refusal, got %v", err)
		}
	})
}

func preparedWorktreeReviewProjection(t *testing.T) (canonicalVault, workspace, taskID string, run RunStatus, sourceSHA string) {
	t.Helper()
	canonicalVault, workspace, taskID = frozenTaskWorktreeSeedFixture(t)
	if err := seedCanonicalV7TaskIntoWorkspace(canonicalVault, workspace, taskID); err != nil {
		t.Fatal(err)
	}
	localTaskPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "work", "tasks", taskID+".md")
	localData, localBody, err := parseFrontmatterMustRead(localTaskPath)
	if err != nil {
		t.Fatal(err)
	}
	startRevision := stringField(localData, "state_rev")
	runtimeAttemptID := "01KYREVIEWPROJECTION000000000"
	if err := attemptV7StartCmd(Args{
		"vault": runnerWorktreeVaultPath(workspace, canonicalVault), "quiet": "true", "id": taskID,
		"attempt-id": taskID + "-A-0001", "runtime-attempt-id": runtimeAttemptID, "task-state-rev": startRevision,
		"lane": runLaneExecute, "runner": "codex", "workspace-kind": "git_worktree", "workspace-path": workspace, "branch": v7TaskBranchName(taskID),
	}); err != nil {
		t.Fatal(err)
	}
	localData["status"] = "review"
	localData["readiness"] = "waiting_on_review"
	localData["next_owner"] = "reviewer:agent"
	localData["proof_status"] = "satisfied"
	localBody += "\nworker verification\n"
	if _, err := saveV7DocumentCAS(localTaskPath, localData, localBody, v7FrontmatterOrder["task"], startRevision); err != nil {
		t.Fatal(err)
	}
	attemptPath := filepath.Join(runnerWorktreeVaultPath(workspace, canonicalVault), "attempts", taskID, taskID+"-A-0001.md")
	attemptData, attemptBody, err := parseFrontmatterMustRead(attemptPath)
	if err != nil {
		t.Fatal(err)
	}
	attemptData["status"] = "handoff"
	if _, err := saveV7DocumentCAS(attemptPath, attemptData, attemptBody, v7FrontmatterOrder["attempt"], stringField(attemptData, "state_rev")); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, workspace, "add", ".tusker")
	runGitDir(t, workspace, "commit", "-m", "complete "+taskID)
	sourceSHA, ok := gitRevParse(workspace, "HEAD^{commit}")
	if !ok {
		t.Fatal("missing committed source SHA")
	}
	run = RunStatus{RecordID: taskID, ItemID: taskID, Lane: runLaneExecute, ActiveAttemptID: runtimeAttemptID, WorkspacePath: workspace}
	return canonicalVault, workspace, taskID, run, sourceSHA
}
