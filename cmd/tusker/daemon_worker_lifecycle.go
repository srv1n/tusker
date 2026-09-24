package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const workerLifecycleRequestFile = ".tusker-worker-lifecycle.json"

// daemonLifecycleActor is the actor the daemon stamps on lifecycle mutations it
// applies on behalf of a bound worker (normalized submit/fail/release). The run
// authorization that admitted the dispatch records the dispatcher's identity
// (daemon_auto or the directive actor), so work-session checks must recognize
// this actor explicitly rather than comparing it to the authorization actor.
// That recognition is gated on an in-process signal set by applyWorkerLifecycle
// alone; the actor string on its own proves nothing, so a CLI caller passing
// --by agent:tusker-daemon still faces the normal authorization-actor check.
const daemonLifecycleActor = "agent:tusker-daemon"

func (d *Daemon) applyWorkerLifecycle(req daemonControlRequest) error {
	if d == nil {
		return fmt.Errorf("worker lifecycle request is incomplete")
	}
	return applyWorkerLifecycle(d.store, req)
}

func queueWorkerLifecycle(store *RuntimeStore, req daemonControlRequest) error {
	run, err := validateWorkerLifecycle(store, req)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return writeConfigTextAtomically(workerLifecycleRequestPath(run.WorkspacePath), string(payload))
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
	// The workspace slot holds a single request. Until requests are
	// attempt-scoped, a shared checkout can expose another run's request to
	// this reconciler: leave it untouched for the matching owner instead of
	// applying or dropping it.
	if req.Worker != nil && (req.ProjectID != run.ProjectID || req.Worker.RecordID != run.RecordID ||
		req.Worker.AttemptID != run.ActiveAttemptID || req.Worker.LeaseGeneration != run.LeaseGeneration) {
		return nil, false, nil
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
		materialScope, generatedOutputScope, err := canonicalRunMaterialScopeWithGeneratedOutputs(d.store, *run)
		if err != nil {
			return nil, false, err
		}
		endState, err := captureRunEndStateForMaterialScope(run.WorkspacePath, materialScope, string(verdictJSON), "", "", time.Now().UTC(), generatedOutputScope)
		if err != nil {
			return nil, false, err
		}
		if endState.Dirty {
			commitScope, scopeErr := canonicalRunAuthoredScope(d.store, *run)
			if scopeErr != nil {
				return nil, false, scopeErr
			}
			endState.HeadSHA, err = materializeWorkerSubmissionCommit(*run, commitScope)
			if err != nil {
				return nil, false, err
			}
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

func materializeWorkerSubmissionCommit(run RunStatus, materialScope []string) (string, error) {
	tmp, err := os.MkdirTemp("", "tusker-worker-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(tmp, "index"))
	runGit := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", run.WorkspacePath}, args...)...)
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", tuskerError("WORKER_PROJECTION_FAILED", firstActionableLine(string(out), err.Error()))
		}
		return strings.TrimSpace(string(out)), nil
	}
	parent, err := runGit("rev-parse", "HEAD^{commit}")
	if err != nil {
		return "", err
	}
	if _, err := runGit("read-tree", parent); err != nil {
		return "", err
	}
	if _, err := runGit(append([]string{"add", "-f", "-A", "--"}, materialScope...)...); err != nil {
		return "", err
	}
	tree, err := runGit("write-tree")
	if err != nil {
		return "", err
	}
	return runGit("-c", "commit.gpgsign=false", "commit-tree", tree, "-p", parent, "-m", "Tusker worker submission "+run.RecordID)
}

func applyWorkerLifecycle(store *RuntimeStore, req daemonControlRequest) error {
	run, err := validateWorkerLifecycle(store, req)
	if err != nil {
		return err
	}
	w := req.Worker
	args := Args{"id": run.RecordID, "project": run.ProjectID, "owner": run.LeaseOwner, "revision": fmt.Sprintf("%d", run.WorkRevision),
		"deliverable": w.Deliverable, "verification": w.Verification, "gate-verdicts": w.GateVerdicts, "reason": w.Reason,
		"actor": daemonLifecycleActor, "quiet": "true"}
	return runsLifecycleWithStore(store, args, w.Action, false, true)
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
		run.RecordID != w.RecordID || run.Lane != w.Lane || !sameCanonicalProjectPath(run.WorkspacePath, w.Workspace) ||
		filepath.Base(run.StatusPath) != filepath.Base(w.StatusPath) || !sameCanonicalProjectPath(filepath.Dir(run.StatusPath), filepath.Dir(w.StatusPath)) ||
		run.LeaseGeneration != w.LeaseGeneration || run.WorkRevision != w.WorkRevision ||
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
