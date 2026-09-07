package main

import (
	"context"
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
	server, err := startDaemonControlServer(stateRoot, func(_ context.Context, req daemonControlRequest) daemonControlResponse {
		requests <- req
		return daemonControlResponse{OK: true}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	t.Setenv("TUSKER_ATTEMPT_ID", "attempt-1")
	t.Setenv("TUSKER_PROJECT_ID", "project-1")
	t.Setenv("TUSKER_RECORD_ID", "APP-T-0001")
	t.Setenv("TUSKER_RUN_LANE", runLaneExecute)
	t.Setenv("TUSKER_WORKSPACE", t.TempDir())
	t.Setenv("TUSKER_STATUS_PATH", filepath.Join(t.TempDir(), "status.json"))
	t.Setenv("TUSKER_LEASE_GENERATION", "2")
	t.Setenv("TUSKER_WORK_REVISION", "1")
	if err := workerLifecycleControlAt(stateRoot, Args{"deliverable": "implemented", "verification": "test passed", "gate-verdicts": "A1=pass"}, "submit"); err != nil {
		t.Fatal(err)
	}
	req := <-requests
	if req.Worker == nil || req.Worker.AttemptID != "attempt-1" || req.Worker.Action != "submit" {
		t.Fatalf("worker lifecycle request = %#v", req)
	}
	if _, err := os.Stat(runtimeStoreDBPath(stateRoot)); !os.IsNotExist(err) {
		t.Fatalf("worker opened global runtime store: %v", err)
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
