package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The daemon on the default address may belong to another state root and
// refuse an attempt it does not own, so a refusal still queues the request for
// the owning daemon. A submit missing a summary is refused before any handoff.
func TestWorkerSubmitRefusalStillQueuesForOwningDaemon(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/capability" {
			_ = json.NewEncoder(w).Encode(map[string]string{"capability": "test-token"})
			return
		}
		serveJSON(w, http.StatusConflict, serveActionResult{OK: false, Refused: true, Reason: "worker lifecycle identity does not match the daemon-owned active run"})
	}))
	defer server.Close()
	workspace := t.TempDir()
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	t.Setenv("TUSKER_PROJECT_ID", "project-1")
	t.Setenv("TUSKER_RECORD_ID", "APP-T-0001")
	t.Setenv("TUSKER_RUN_LANE", runLaneExecute)
	t.Setenv("TUSKER_WORKSPACE", workspace)
	t.Setenv("TUSKER_STATUS_PATH", filepath.Join(t.TempDir(), "status.json"))
	t.Setenv("TUSKER_LEASE_GENERATION", "1")
	t.Setenv("TUSKER_WORK_REVISION", "0")

	if err := workerLifecycleHandoff(server.URL, Args{"deliverable": "implemented", "verification": "tests passed"}, "submit"); err != nil {
		t.Fatalf("handoff after a foreign daemon's refusal: %v", err)
	}
	if !fileExists(workerLifecycleRequestPath(workspace, "attempt-1")) {
		t.Fatal("a submit refused by the default-address daemon was not queued for the owning daemon")
	}
	if _, err := workerLifecycleRequest(Args{"deliverable": "implemented"}, "submit"); err == nil {
		t.Fatal("a submit without verification must be refused before it is handed off")
	}
}

// The incident: the worker's queued submit lacked a verification summary, so
// the daemon's terminal reconciliation failed on it every poll and the exited
// run sat "running" until its lease went dead. Reconciliation must finish the
// attempt and record why the request was refused.
func TestRefusedWorkspaceSubmitRequestEndsAttemptWithReason(t *testing.T) {
	vault, project := workSessionFixture(t, 1)
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
		t.Fatalf("bind run: ok=%v err=%v", ok, err)
	}
	req := daemonControlRequest{Command: "worker_lifecycle", ProjectID: run.ProjectID, Identity: run.ActiveAttemptID,
		Worker: &daemonWorkerLifecycleRequest{Action: "submit", AttemptID: run.ActiveAttemptID, RecordID: run.RecordID,
			Lane: run.Lane, Workspace: run.WorkspacePath, StatusPath: run.StatusPath,
			LeaseGeneration: run.LeaseGeneration, WorkRevision: run.WorkRevision, Deliverable: "implemented"}}
	raw, _ := json.Marshal(req)
	if err := os.WriteFile(workerLifecycleRequestPath(run.WorkspacePath, run.ActiveAttemptID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	status, _ := json.Marshal(runnerProcessStatus{ExitCode: 0, CompletedAt: time.Now().UTC().Format(time.RFC3339)})
	if err := os.WriteFile(run.StatusPath, status, 0o600); err != nil {
		t.Fatal(err)
	}
	projects, err := store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %#v err=%v", projects, err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	daemon := &Daemon{stateRoot: DefaultStateRoot(), store: store, notifyWake: make(chan string, 1)}
	t.Cleanup(daemon.stopNotifyTimers)
	updated, changed, err := daemon.reconcileRun(context.Background(), projects[0], wf, *run)
	if err != nil || !changed {
		t.Fatalf("exited run with a refused submit wedged reconciliation: changed=%v err=%v", changed, err)
	}
	if isDispatchingLeaseState(updated.LeaseState) {
		t.Fatalf("exited run still holds its lease: %#v", updated)
	}
	if !strings.Contains(updated.LastError, "worker submission refused") || !strings.Contains(updated.LastError, "verification") {
		t.Fatalf("run does not say why the submission was refused: %q", updated.LastError)
	}
}

func TestInterruptedDeadReservationSchedulesRetry(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		store := fairDispatchTestStore(t)
		now := time.Now().UTC()
		dead := selfServiceDeadRunFixture("project-retry", "RET-T-0001", now)
		dead.AttemptCount, dead.SessionRef = 1, "session-1"
		if err := store.UpsertRun(dead); err != nil {
			t.Fatal(err)
		}
		changed, _, _, err := autoRepairDeadReservation(store, enabled, dead, now)
		if err != nil || !changed {
			t.Fatalf("enabled=%v: repair changed=%v err=%v", enabled, changed, err)
		}
		current, err := store.FindRunScoped("project-retry", "RET-T-0001")
		if err != nil || current == nil {
			t.Fatalf("run unavailable: %v", err)
		}
		if !enabled {
			// A disabled project's reclaim only frees capacity; nothing redispatches it.
			if current.LeaseState != string(LeaseStateInterrupted) {
				t.Fatalf("disabled project's dead reservation was requeued: %#v", current)
			}
			continue
		}
		if current.LeaseState != string(LeaseStateRetryQueued) || current.NextRetryAt == "" || current.ActiveAttemptID != "" || current.SessionRef != "session-1" {
			t.Fatalf("enabled=%v: dead reservation left no retry: %#v", enabled, current)
		}
		if !shouldDispatchRun(*current, now) {
			t.Fatalf("enabled=%v: requeued run is not due", enabled)
		}
		d := &Daemon{}
		if !d.runEventDue(*current, nil, now) {
			t.Fatalf("enabled=%v: heartbeat sees no due event for the requeued run", enabled)
		}
	}
}

func TestShowRunAcceptsProjectName(t *testing.T) {
	store := fairDispatchTestStore(t)
	if err := store.UpsertProject(RegisteredProject{ProjectID: "01PROJECTID", ProjectKey: "key-1", Name: "e2e-shared3", RepoRoot: t.TempDir(), VaultRoot: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRun(fairDispatchTestRun("01PROJECTID", "ALP-T-0001")); err != nil {
		t.Fatal(err)
	}
	for _, selector := range []string{"01PROJECTID", "key-1", "e2e-shared3"} {
		run, err := findRunScopedOrAmbiguous(store, selector, "ALP-T-0001")
		if err != nil || run == nil || run.ProjectID != "01PROJECTID" {
			t.Fatalf("--project %s: run=%#v err=%v", selector, run, err)
		}
	}
	if run, err := findRunScopedOrAmbiguous(store, "other", "ALP-T-0001"); err != nil || run != nil {
		t.Fatalf("unknown project must not match: run=%#v err=%v", run, err)
	}
}
