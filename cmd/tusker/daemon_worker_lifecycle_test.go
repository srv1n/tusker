package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxedWorkerLifecycleUsesDaemonBoundaryWithoutRuntimeStore(t *testing.T) {
	stateRoot, err := os.MkdirTemp("/tmp", "twl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	requests := make(chan daemonControlRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/capability" {
			_ = json.NewEncoder(w).Encode(map[string]string{"capability": "test-token"})
			return
		}
		var req daemonControlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests <- req
		_ = json.NewEncoder(w).Encode(serveActionResult{OK: true})
	}))
	defer server.Close()
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	t.Setenv("TUSKER_PROJECT_ID", "project-1")
	t.Setenv("TUSKER_RECORD_ID", "APP-T-0001")
	t.Setenv("TUSKER_RUN_LANE", runLaneExecute)
	t.Setenv("TUSKER_WORKSPACE", t.TempDir())
	t.Setenv("TUSKER_STATUS_PATH", filepath.Join(t.TempDir(), "status.json"))
	t.Setenv("TUSKER_LEASE_GENERATION", "2")
	t.Setenv("TUSKER_WORK_REVISION", "0")
	req, err := workerLifecycleRequest(Args{"deliverable": "implemented", "verification": "test passed", "gate-verdicts": "A1=pass"}, "submit")
	if err != nil {
		t.Fatal(err)
	}
	if err := workerLifecycleControlAt(server.URL, req); err != nil {
		t.Fatal(err)
	}
	got := <-requests
	if got.Worker == nil || got.Worker.AttemptID != "attempt-1" || got.Worker.Action != "submit" {
		t.Fatalf("worker lifecycle request = %#v", got)
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !os.IsNotExist(err) {
		t.Fatalf("worker opened global runtime store: %v", err)
	}
}

func TestWorkerSubmitQueuesUntilTerminalReconciliation(t *testing.T) {
	store := fairDispatchTestStore(t)
	workspace := t.TempDir()
	run := fairDispatchTestRun("project-1", "APP-T-0001")
	run.ActiveAttemptID = "attempt-1"
	run.LeaseState = string(LeaseStateRunning)
	run.LeaseGeneration = 2
	run.WorkspacePath = workspace
	run.StatusPath = filepath.Join(workspace, "status.json")
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	req := daemonControlRequest{Command: "worker_lifecycle", ProjectID: run.ProjectID, Identity: run.ActiveAttemptID, Worker: &daemonWorkerLifecycleRequest{
		Action: "submit", AttemptID: run.ActiveAttemptID, RecordID: run.RecordID, Lane: run.Lane,
		Workspace: workspace, StatusPath: run.StatusPath, LeaseGeneration: run.LeaseGeneration,
		WorkRevision: run.WorkRevision, Deliverable: "implemented", Verification: "passed",
	}}
	if err := queueWorkerLifecycle(store, req); err != nil {
		t.Fatal(err)
	}
	if !fileExists(workerLifecycleRequestPath(workspace)) {
		t.Fatal("submit was not queued in the worker workspace")
	}
	stored, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil || stored == nil || stored.LeaseState != string(LeaseStateRunning) {
		t.Fatalf("submit bypassed terminal reconciliation: run=%#v err=%v", stored, err)
	}
}

func TestDaemonMaterializesSandboxedWorkerSubmissionCommit(t *testing.T) {
	repo := t.TempDir()
	initializeOrchestrationGitRepo(t, repo)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("owned/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "add", ".gitignore")
	runGitDir(t, repo, "commit", "-m", "ignore generated output")
	if err := os.MkdirAll(filepath.Join(repo, "owned"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "owned", "result.txt"), []byte("result\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head, ok := gitRevParse(repo, "HEAD^{commit}")
	if !ok {
		t.Fatal("fixture HEAD is missing")
	}
	commit, err := materializeWorkerSubmissionCommit(RunStatus{RecordID: "APP-T-0001", WorkspacePath: repo}, []string{"owned"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := gitRevParse(repo, "HEAD^{commit}"); got != head {
		t.Fatalf("daemon projection moved shared HEAD: got %s want %s", got, head)
	}
	if parent, _ := gitRevParse(repo, commit+"^"); parent != head {
		t.Fatalf("projection parent = %s want %s", parent, head)
	}
	content, err := gitOutputTrim(repo, "show", commit+":owned/result.txt")
	if err != nil || content != "result" {
		t.Fatalf("projected material = %q err=%v", content, err)
	}
}

func TestDispatchedWorkerStartRefusesWithoutRuntimeStore(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "must-not-exist")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	err := workSessionStartCmd(Args{"id": "APP-T-0001", "by": "agent:worker"})
	if err == nil || !strings.Contains(err.Error(), "already holds daemon attempt") {
		t.Fatalf("worker re-claim refusal = %v", err)
	}
	if _, statErr := os.Stat(runtimeStoreDBPath(stateRoot)); !os.IsNotExist(statErr) {
		t.Fatalf("worker re-claim opened runtime store: %v", statErr)
	}
}
