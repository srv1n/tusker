package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func TestSharedCheckoutLifecycleRequestsArePerAttempt(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".tusker"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(workspace, ".tusker", "workspace.json"), `{"strategy":"shared"}`); err != nil {
		t.Fatal(err)
	}
	one := workerLifecycleRequestPath(workspace, "attempt-one")
	two := workerLifecycleRequestPath(workspace, "attempt-two")
	if one == two || one == workerLifecycleRequestPath(workspace) {
		t.Fatalf("shared lifecycle slots collide: %q %q", one, two)
	}
	if err := writeText(one, "one"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(two, "two"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := readText(one); raw != "one" {
		t.Fatalf("first slot changed: %q", raw)
	}
	if got := workerLifecycleRequestPath(t.TempDir(), "attempt-one"); !strings.HasSuffix(got, workerLifecycleRequestFile) {
		t.Fatalf("worktree slot changed: %s", got)
	}
}

func TestSharedCheckoutStraysAndFailureCleanup(t *testing.T) {
	vault, project := workSessionFixture(t, 2)
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		if err := startWorkSessionTest(t, vault, id, "agent:"+id); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runs := make(map[string]RunStatus)
	for _, id := range []string{"APP-T-0001", "APP-T-0002"} {
		run, err := store.FindRunScoped(project.ProjectID, id)
		if err != nil || run == nil {
			t.Fatalf("missing run %s: %v", id, err)
		}
		run.WorkspacePath = project.RepoRoot
		run.ActiveAttemptID = "attempt-" + id
		run.HandRun = false // simulate daemon-dispatched workers
		run.Lane = runLaneExecute
		run.LeaseState = string(LeaseStateRunning)
		if err := store.UpsertRun(*run); err != nil {
			t.Fatal(err)
		}
		runs[id] = *run
	}
	if err := writeText(filepath.Join(vault, "workspace.json"), `{"strategy":"shared"}`); err != nil {
		t.Fatal(err)
	}
	trackedPath := filepath.Join(project.RepoRoot, "owned/APP-T-0001/tracked.txt")
	if err := os.MkdirAll(filepath.Dir(trackedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeText(trackedPath, "before"); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, project.RepoRoot, "add", "owned/APP-T-0001/tracked.txt")
	runGitDir(t, project.RepoRoot, "commit", "-m", "seed tracked scope")
	if err := writeText(trackedPath, "after"); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"owned/APP-T-0001/own.txt": "own", "owned/APP-T-0002/other.txt": "other", "stray.txt": "stray",
	} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(project.RepoRoot, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeText(filepath.Join(project.RepoRoot, path), content); err != nil {
			t.Fatal(err)
		}
	}
	one := runs["APP-T-0001"]
	scope, err := canonicalRunAuthoredScope(store, one)
	if err != nil {
		t.Fatal(err)
	}
	stray, overlaps, err := sharedCheckoutStrays(store, one, scope)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(stray, "stray.txt") || overlaps["owned/APP-T-0002/other.txt"] != "APP-T-0002" {
		t.Fatalf("stray=%v overlaps=%v", stray, overlaps)
	}
	// The socket submit path passes commitDirty=false; a shared checkout must
	// still get Tusker's scope commit because the worker cannot make one.
	endState, err := captureSubmissionEndState(store, one, `{"A1":"pass"}`, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := gitRevParse(project.RepoRoot, "HEAD^{commit}"); endState.HeadSHA == "" || endState.HeadSHA == head {
		t.Fatalf("shared submission was not committed: head=%s end=%s", head, endState.HeadSHA)
	}
	if got, err := gitOutputTrim(project.RepoRoot, "show", endState.HeadSHA+":owned/APP-T-0001/own.txt"); err != nil || got != "own" {
		t.Fatalf("shared submission commit lacks owned file: %q %v", got, err)
	}
	commit, err := materializeWorkerSubmissionCommit(one, append(scope, stray...))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := gitOutputTrim(project.RepoRoot, "show", commit+":stray.txt"); err != nil || got != "stray" {
		t.Fatalf("unclaimed stray omitted: %q %v", got, err)
	}
	if _, err := gitOutputTrim(project.RepoRoot, "show", commit+":owned/APP-T-0002/other.txt"); err == nil {
		t.Fatal("other run's path was staged")
	}
	if err := abandonSharedCheckoutScope(store, one); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(project.RepoRoot, "owned/APP-T-0001/own.txt")) {
		t.Fatal("failed run's untracked file survived cleanup")
	}
	if got, _ := readText(trackedPath); got != "before" {
		t.Fatalf("failed run's tracked file was not restored: %q", got)
	}
	if !fileExists(filepath.Join(project.RepoRoot, "owned/APP-T-0002/other.txt")) || !fileExists(filepath.Join(project.RepoRoot, "stray.txt")) {
		t.Fatal("cleanup touched paths outside failed run's scope")
	}
	ref := "refs/tusker/abandoned/" + one.ItemID + "-" + one.ActiveAttemptID
	if got, err := gitOutputTrim(project.RepoRoot, "show", ref+":owned/APP-T-0001/own.txt"); err != nil || got != "own" {
		t.Fatalf("abandoned ref missing work: %q %v", got, err)
	}
	if got, err := gitOutputTrim(project.RepoRoot, "show", ref+":owned/APP-T-0001/tracked.txt"); err != nil || got != "after" {
		t.Fatalf("abandoned ref missing tracked edit: %q %v", got, err)
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

// The daemon lifecycle actor exemption is provenance, not a claim: the
// in-process applyWorkerLifecycle path submits for the bound worker, while a
// CLI-style call carrying the same actor string and lease generation through
// parsed Args faces the normal authorization-actor check and is refused.
func TestDaemonLifecycleActorExemptionRequiresInProcessDaemon(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
	owned := filepath.Join(project.RepoRoot, "owned", "APP-T-0001")
	if err := os.MkdirAll(owned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, "implementation.go"), []byte("package owned\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, project.RepoRoot, "add", "owned/APP-T-0001")
	runGitDir(t, project.RepoRoot, "commit", "-m", "seed owned implementation")
	if err := startWorkSessionTest(t, vault, "APP-T-0001", "agent:worker"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	run, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || run == nil {
		t.Fatalf("run unavailable: %#v err=%v", run, err)
	}
	run.WorkspacePath = project.RepoRoot
	run.StatusPath = filepath.Join(t.TempDir(), "APP-T-0001.status.json")
	if ok, err := store.UpdateRunIfLease(*run, run.LeaseOwner, run.LeaseGeneration); err != nil || !ok {
		t.Fatalf("bind run to workspace: ok=%v err=%v", ok, err)
	}
	// The dispatch authorization records the dispatcher's identity, which
	// differs from the lifecycle actor: only the in-process daemon seam may
	// submit under agent:tusker-daemon here.
	if err := store.SaveRunAuthorization(RunAuthorization{Source: "daemon_auto", Actor: "daemon",
		ProjectID: run.ProjectID, RecordID: run.RecordID, LeaseGeneration: run.LeaseGeneration,
		AttemptID: run.ActiveAttemptID}); err != nil {
		t.Fatal(err)
	}
	req := daemonControlRequest{Command: "worker_lifecycle", ProjectID: run.ProjectID, Identity: run.ActiveAttemptID,
		Worker: &daemonWorkerLifecycleRequest{Action: "submit", AttemptID: run.ActiveAttemptID, RecordID: run.RecordID,
			Lane: run.Lane, Workspace: run.WorkspacePath, StatusPath: run.StatusPath,
			LeaseGeneration: run.LeaseGeneration, WorkRevision: run.WorkRevision,
			Deliverable: "implemented", Verification: "A1 checked", GateVerdicts: "A1=pass"}}
	if err := applyWorkerLifecycle(store, req); err != nil {
		t.Fatalf("in-process daemon lifecycle submit refused: %v", err)
	}
	stored, err := store.FindRunScoped(project.ProjectID, "APP-T-0001")
	if err != nil || stored == nil || stored.AttemptOutcome != string(AttemptOutcomeSucceeded) {
		t.Fatalf("daemon lifecycle submit did not reach terminal state: %#v err=%v", stored, err)
	}
	data, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", "APP-T-0001.md"))
	if err != nil || stringField(data, "status") != "review" {
		t.Fatalf("daemon lifecycle submit did not move task to review: status=%q err=%v", stringField(data, "status"), err)
	}

	spoof := Args{"vault": vault, "id": "APP-T-0001", "status": "review", "by": daemonLifecycleActor,
		"normalized-work-submit": "true", "lease-generation": strconv.Itoa(run.LeaseGeneration),
		"local": "true", "quiet": "true"}
	if err := statusCmd(spoof); workSessionErrorCode(err) != "WORK_SESSION_REQUIRED" {
		t.Fatalf("CLI-style daemon lifecycle actor claim = %v, want WORK_SESSION_REQUIRED", err)
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
