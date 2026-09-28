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
	return writeConfigTextAtomically(workerLifecycleRequestPath(run.WorkspacePath, run.ActiveAttemptID), string(payload))
}

func workerLifecycleRequestPath(workspace string, attempt ...string) string {
	if len(attempt) > 0 && attempt[0] != "" && sharedWorkspaceMetadata(workspace) {
		return filepath.Join(workspace, ".tusker-worker-lifecycle-"+filepath.Base(attempt[0])+".json")
	}
	return filepath.Join(workspace, workerLifecycleRequestFile)
}

func sharedWorkspaceMetadata(workspace string) bool {
	raw, err := os.ReadFile(filepath.Join(workspace, ".tusker", "workspace.json"))
	if err != nil {
		return false
	}
	var metadata WorkspaceMetadata
	return json.Unmarshal(raw, &metadata) == nil && normalizeWorkspaceStrategy(WorkspaceStrategy(metadata.Strategy)) == WorkspaceStrategyShared
}

func (d *Daemon) consumeWorkerLifecycleRequest(run RunStatus) (*RunStatus, bool, error) {
	path := workerLifecycleRequestPath(run.WorkspacePath, run.ActiveAttemptID)
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
	// Keep the identity check even with attempt-scoped files: the request is
	// untrusted worker input.
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
		commitScope, err := canonicalRunAuthoredScope(d.store, *run)
		if err != nil {
			return nil, false, err
		}
		var stray []string
		var overlaps map[string]string
		if sharedWorkspaceMetadata(run.WorkspacePath) && run.Lane == runLaneExecute {
			stray, overlaps, err = sharedCheckoutStrays(d.store, *run, commitScope)
			if err != nil {
				return nil, false, err
			}
			materialScope = append(materialScope, stray...)
			commitScope = append(commitScope, stray...)
		}
		endState, err := captureRunEndStateForMaterialScope(run.WorkspacePath, materialScope, string(verdictJSON), "", "", time.Now().UTC(), generatedOutputScope)
		if err != nil {
			return nil, false, err
		}
		if endState.Dirty {
			endState.HeadSHA, err = materializeWorkerSubmissionCommit(*run, commitScope)
			if err != nil {
				return nil, false, err
			}
		}
		endState.StrayPaths, endState.Overlaps = stray, overlaps
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

func sharedCheckoutStrays(store *RuntimeStore, run RunStatus, own []string) ([]string, map[string]string, error) {
	dirty, err := inPlaceDirtyPaths(run.WorkspacePath)
	if err != nil {
		return nil, nil, err
	}
	runs, err := store.ListRuns()
	if err != nil {
		return nil, nil, err
	}
	projects, err := loadRegisteredProjects(store, registeredProjectLoadOptions{LoadDisabled: true, ProjectID: run.ProjectID})
	if err != nil || len(projects) != 1 || projects[0].LoadError != nil {
		return nil, nil, firstNonNil(err, fmt.Errorf("shared checkout project unavailable"))
	}
	idx, err := loadV7Index(projects[0].Project.VaultRoot)
	if err != nil {
		return nil, nil, err
	}
	others := make(map[string][]string)
	for _, other := range runs {
		if other.ProjectID != run.ProjectID || other.RecordID == run.RecordID || !sharedCheckoutRunHoldsWork(other) || !sameCanonicalProjectPath(other.WorkspacePath, run.WorkspacePath) {
			continue
		}
		scope, err := canonicalRunAuthoredScope(store, other)
		if err != nil {
			return nil, nil, err
		}
		others[other.ItemID] = scope
	}
	var stray []string
	overlaps := make(map[string]string)
	for _, path := range dirty {
		if workspaceMaterialScopeContains(own, path) {
			continue
		}
		if sharedCheckoutSubmittedBlob(run.WorkspacePath, path, idx) {
			continue
		}
		owner := ""
		for taskID, scope := range others {
			if workspaceMaterialScopeContains(scope, path) {
				owner = taskID
				break
			}
		}
		if owner != "" {
			overlaps[path] = owner
		} else {
			stray = append(stray, path)
		}
	}
	return stray, overlaps, nil
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

func abandonSharedCheckoutScope(store *RuntimeStore, run RunStatus) error {
	if run.Lane != runLaneExecute || !sharedWorkspaceMetadata(run.WorkspacePath) || run.ActiveAttemptID == "" {
		return nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	for _, attempt := range attempts {
		if attempt.AttemptID == run.ActiveAttemptID && attempt.EndState.Schema != "" {
			return nil
		}
	}
	scope, err := canonicalRunAuthoredScope(store, run)
	if err != nil {
		return err
	}
	args := append([]string{"status", "--porcelain", "--untracked-files=all", "--"}, scope...)
	dirty, err := gitOutputTrim(run.WorkspacePath, args...)
	if err != nil || dirty == "" {
		return err
	}
	commit, err := materializeWorkerSubmissionCommit(run, scope)
	if err != nil {
		return err
	}
	ref := "refs/tusker/abandoned/" + run.ItemID + "-" + run.ActiveAttemptID
	if _, err := gitOutputTrim(run.WorkspacePath, "update-ref", ref, commit); err != nil {
		return err
	}
	untracked, err := gitCombined(run.WorkspacePath, append([]string{"ls-files", "--others", "--exclude-standard", "-z", "--"}, scope...)...)
	if err != nil {
		return err
	}
	tracked, err := gitCombined(run.WorkspacePath, append([]string{"ls-files", "-z", "--"}, scope...)...)
	if err != nil {
		return err
	}
	if tracked != "" {
		paths := strings.Split(strings.TrimSuffix(tracked, "\x00"), "\x00")
		if _, err := gitOutputTrim(run.WorkspacePath, append([]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, paths...)...); err != nil {
			return err
		}
	}
	for _, path := range strings.Split(untracked, "\x00") {
		if path != "" && workspaceMaterialScopeContains(scope, path) {
			if err := os.Remove(filepath.Join(run.WorkspacePath, filepath.FromSlash(path))); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func applyWorkerLifecycle(store *RuntimeStore, req daemonControlRequest) error {
	run, err := validateWorkerLifecycle(store, req)
	if err != nil {
		return err
	}
	w := req.Worker
	if w.Action == "fail" || w.Action == "release" {
		if err := abandonSharedCheckoutScope(store, *run); err != nil {
			return err
		}
	}
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
