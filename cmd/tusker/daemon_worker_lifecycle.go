package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	// A successful submitted worker still has to traverse the normal terminal
	// status path below. That path captures its immutable end state, projects
	// the work revision, and schedules independent review. Applying submit here
	// would release the run early and skip those daemon-owned transitions.
	if req.Worker != nil && req.Worker.Action == "submit" {
		run, err := validateWorkerLifecycle(d.store, req)
		if err != nil {
			return nil, false, err
		}
		verdicts, err := parseGateVerdicts(req.Worker.GateVerdicts)
		if err != nil {
			return nil, false, err
		}
		verdictJSON, _ := json.Marshal(verdicts)
		materialScope, err := canonicalRunMaterialScope(d.store, *run)
		if err != nil {
			return nil, false, err
		}
		endState, err := captureRunEndStateForMaterialScope(run.WorkspacePath, materialScope, string(verdictJSON), "", "", time.Now().UTC())
		if err != nil {
			return nil, false, err
		}
		if err := d.applyWorkerLifecycle(req); err != nil {
			return nil, false, err
		}
		if err := saveAttemptEndStateForRun(d.store, *run, endState); err != nil {
			return nil, false, err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return nil, false, err
		}
		return run, true, nil
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
	run, err := validateWorkerLifecycle(store, req)
	if err != nil {
		return err
	}
	w := req.Worker
	args := Args{"id": run.RecordID, "project": run.ProjectID, "owner": run.LeaseOwner, "revision": fmt.Sprintf("%d", run.WorkRevision),
		"deliverable": w.Deliverable, "verification": w.Verification, "gate-verdicts": w.GateVerdicts, "reason": w.Reason,
		"actor": "agent:" + w.AttemptID, "quiet": "true"}
	return runsLifecycleWithStore(store, args, w.Action, false)
}

func validateWorkerLifecycle(store *RuntimeStore, req daemonControlRequest) (*RunStatus, error) {
	if store == nil || req.Worker == nil {
		return nil, fmt.Errorf("worker lifecycle request is incomplete")
	}
	w := req.Worker
	if req.ProjectID == "" || w.AttemptID == "" || w.RecordID == "" || w.Workspace == "" || w.StatusPath == "" || w.LeaseGeneration <= 0 || w.WorkRevision < 0 {
		return nil, fmt.Errorf("worker lifecycle identity is incomplete")
	}
	run, err := store.FindRunScoped(req.ProjectID, w.RecordID)
	if err != nil || run == nil {
		return nil, firstNonNil(err, fmt.Errorf("worker lifecycle run not found"))
	}
	if req.Identity != w.AttemptID || run.ActiveAttemptID != w.AttemptID || run.ProjectID != req.ProjectID ||
		run.RecordID != w.RecordID || run.Lane != w.Lane || run.WorkspacePath != w.Workspace ||
		run.StatusPath != w.StatusPath || run.LeaseGeneration != w.LeaseGeneration || run.WorkRevision != w.WorkRevision ||
		!isDispatchingLeaseState(run.LeaseState) {
		return nil, fmt.Errorf("worker lifecycle identity does not match the daemon-owned active run")
	}
	if len(w.Deliverable) > 4096 || len(w.Verification) > 4096 || len(w.GateVerdicts) > 4096 || len(w.Reason) > 4096 {
		return nil, fmt.Errorf("worker lifecycle payload is oversized")
	}
	if w.Action == "submit" && (strings.TrimSpace(w.Deliverable) == "" || strings.TrimSpace(w.Verification) == "") {
		return nil, fmt.Errorf("worker submit requires deliverable and verification summaries")
	}
	return run, nil
}
