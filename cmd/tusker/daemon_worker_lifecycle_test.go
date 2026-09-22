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

func TestWorkerSubmitAcceptsCanonicalWorkspaceAlias(t *testing.T) {
	store := fairDispatchTestStore(t)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	alias := filepath.Join(root, "alias")
	if err := os.Mkdir(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(workspace, alias); err != nil {
		t.Fatal(err)
	}
	run := fairDispatchTestRun("project-1", "APP-T-0001")
	run.ActiveAttemptID, run.LeaseState, run.LeaseGeneration = "attempt-1", string(LeaseStateRunning), 2
	run.WorkspacePath, run.StatusPath = workspace, filepath.Join(workspace, "status.json")
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	req := daemonControlRequest{Command: "worker_lifecycle", ProjectID: run.ProjectID, Identity: run.ActiveAttemptID, Worker: &daemonWorkerLifecycleRequest{
		Action: "submit", AttemptID: run.ActiveAttemptID, RecordID: run.RecordID, Lane: run.Lane,
		Workspace: alias, StatusPath: filepath.Join(alias, "status.json"), LeaseGeneration: run.LeaseGeneration,
		WorkRevision: run.WorkRevision, Deliverable: "implemented", Verification: "passed",
	}}
	if err := queueWorkerLifecycle(store, req); err != nil {
		t.Fatal(err)
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

func TestExternalReviewSharedSubmission(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	shared := project.RepoRoot
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		dir := filepath.Join(shared, "owned", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "implementation.go"), []byte("package owned\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitDir(t, shared, "add", "owned")
	runGitDir(t, shared, "commit", "-m", "seed shared checkout submissions")
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:a"); err != nil {
		t.Fatal(err)
	}
	if err := startWorkSessionTest(t, vault, "APP-T-0002", "agent:b"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, notifyWake: make(chan string, 1)}
	t.Cleanup(daemon.stopNotifyTimers)
	statusDir := t.TempDir()
	runs := map[string]RunStatus{}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		run, err := store.FindRunScoped(project.ProjectID, id)
		if err != nil || run == nil {
			t.Fatalf("run %s unavailable: %#v err=%v", id, run, err)
		}
		run.WorkspacePath = shared
		run.StatusPath = filepath.Join(statusDir, id+".status.json")
		if ok, err := store.UpdateRunIfLease(*run, run.LeaseOwner, run.LeaseGeneration); err != nil || !ok {
			t.Fatalf("bind %s to shared checkout: ok=%v err=%v", id, ok, err)
		}
		if err := store.SaveRunAuthorization(RunAuthorization{Source: "daemon_auto", Actor: "agent:tusker-daemon",
			ProjectID: run.ProjectID, RecordID: run.RecordID, LeaseGeneration: run.LeaseGeneration, AttemptID: run.ActiveAttemptID}); err != nil {
			t.Fatal(err)
		}
		runs[id] = *run
	}
	queue := func(run RunStatus) {
		t.Helper()
		req := daemonControlRequest{Command: "worker_lifecycle", ProjectID: run.ProjectID, Identity: run.ActiveAttemptID,
			Worker: &daemonWorkerLifecycleRequest{Action: "submit", AttemptID: run.ActiveAttemptID, RecordID: run.RecordID,
				Lane: run.Lane, Workspace: run.WorkspacePath, StatusPath: run.StatusPath,
				LeaseGeneration: run.LeaseGeneration, WorkRevision: run.WorkRevision,
				Deliverable: "implemented " + run.RecordID, Verification: "A1 checked", GateVerdicts: "A1=pass"}}
		if err := queueWorkerLifecycle(store, req); err != nil {
			t.Fatalf("queue %s: %v", run.RecordID, err)
		}
	}
	requestPath := workerLifecycleRequestPath(shared)

	// First reconciliation order: the other member's run reconciles while the
	// queued submission belongs to APP-T-0002. It must neither apply nor drop it.
	queue(runs["APP-T-0002"])
	if _, consumed, err := daemon.consumeWorkerLifecycleRequest(runs["APP-T-0001"]); err != nil || consumed {
		t.Fatalf("foreign submission consumed by APP-T-0001 reconciler: consumed=%v err=%v", consumed, err)
	}
	if !fileExists(requestPath) {
		t.Fatal("APP-T-0002 submission was removed by the wrong reconciler")
	}
	updated, consumed, err := daemon.consumeWorkerLifecycleRequest(runs["APP-T-0002"])
	if err != nil || !consumed || updated == nil {
		t.Fatalf("matching owner did not consume its submission: consumed=%v updated=%#v err=%v", consumed, updated, err)
	}
	if fileExists(requestPath) {
		t.Fatal("consumed submission was left in the shared checkout")
	}
	stored, err := store.FindRunScoped(project.ProjectID, "APP-T-0002")
	if err != nil || stored == nil || stored.AttemptOutcome != string(AttemptOutcomeSucceeded) {
		t.Fatalf("matched submission did not reach terminal state: %#v err=%v", stored, err)
	}

	// Reverse order: APP-T-0001 queues while the now-terminal APP-T-0002 run
	// reconciles first. The stale foreign request still cannot be consumed.
	queue(runs["APP-T-0001"])
	if _, consumed, err := daemon.consumeWorkerLifecycleRequest(*stored); err != nil || consumed {
		t.Fatalf("foreign submission consumed by APP-T-0002 reconciler: consumed=%v err=%v", consumed, err)
	}
	if !fileExists(requestPath) {
		t.Fatal("APP-T-0001 submission was removed by the wrong reconciler")
	}
	if _, consumed, err := daemon.consumeWorkerLifecycleRequest(runs["APP-T-0001"]); err != nil || !consumed {
		t.Fatalf("second submission lost after foreign reconciliation: consumed=%v err=%v", consumed, err)
	}
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		stored, err := store.FindRunScoped(project.ProjectID, id)
		if err != nil || stored == nil || stored.AttemptOutcome != string(AttemptOutcomeSucceeded) {
			t.Fatalf("submission for %s did not survive reconciliation: %#v err=%v", id, stored, err)
		}
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
