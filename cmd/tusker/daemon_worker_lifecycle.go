package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const workerLifecycleRequestFile = ".tusker-worker-lifecycle.json"

func (d *Daemon) applyWorkerLifecycle(req daemonControlRequest) error {
	if d == nil {
		return fmt.Errorf("worker lifecycle request is incomplete")
	}
	return applyWorkerLifecycle(d.store, req)
}

func workerLifecycleRequestPath(workspace string) string {
	return filepath.Join(workspace, workerLifecycleRequestFile)
}

func (d *Daemon) consumeWorkerLifecycleRequest(run RunStatus) (*RunStatus, bool, error) {
	path := workerLifecycleRequestPath(run.WorkspacePath)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(raw) > 32<<10 {
		return nil, false, fmt.Errorf("worker lifecycle request is oversized")
	}
	var req daemonControlRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, false, fmt.Errorf("decode worker lifecycle request: %w", err)
	}
	if err := d.applyWorkerLifecycle(req); err != nil {
		return nil, false, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	updated, err := d.store.FindRunScoped(run.ProjectID, run.RecordID)
	return updated, true, err
}

func applyWorkerLifecycle(store *RuntimeStore, req daemonControlRequest) error {
	if store == nil || req.Worker == nil {
		return fmt.Errorf("worker lifecycle request is incomplete")
	}
	w := req.Worker
	if req.ProjectID == "" || w.AttemptID == "" || w.RecordID == "" || w.Workspace == "" || w.StatusPath == "" || w.LeaseGeneration <= 0 || w.WorkRevision < 0 {
		return fmt.Errorf("worker lifecycle identity is incomplete")
	}
	run, err := store.FindRunScoped(req.ProjectID, w.RecordID)
	if err != nil || run == nil {
		return firstNonNil(err, fmt.Errorf("worker lifecycle run not found"))
	}
	if req.Identity != w.AttemptID || run.ActiveAttemptID != w.AttemptID || run.ProjectID != req.ProjectID ||
		run.RecordID != w.RecordID || run.Lane != w.Lane || run.WorkspacePath != w.Workspace ||
		run.StatusPath != w.StatusPath || run.LeaseGeneration != w.LeaseGeneration || run.WorkRevision != w.WorkRevision ||
		!isDispatchingLeaseState(run.LeaseState) {
		return fmt.Errorf("worker lifecycle identity does not match the daemon-owned active run")
	}
	if len(w.Deliverable) > 4096 || len(w.Verification) > 4096 || len(w.GateVerdicts) > 4096 || len(w.Reason) > 4096 {
		return fmt.Errorf("worker lifecycle payload is oversized")
	}
	if w.Action == "submit" && (strings.TrimSpace(w.Deliverable) == "" || strings.TrimSpace(w.Verification) == "") {
		return fmt.Errorf("worker submit requires deliverable and verification summaries")
	}
	args := Args{"id": run.RecordID, "project": run.ProjectID, "owner": run.LeaseOwner, "revision": fmt.Sprintf("%d", run.WorkRevision),
		"deliverable": w.Deliverable, "verification": w.Verification, "gate-verdicts": w.GateVerdicts, "reason": w.Reason,
		"actor": run.LeaseOwner, "quiet": "true"}
	return runsLifecycleWithStore(store, args, w.Action, false)
}
