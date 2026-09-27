package main

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
)

// reviewPassActor closes tasks after a passing review. The reviewer namespace
// satisfies the close policy's acceptor rule; the name says who acted.
const reviewPassActor = "reviewer:daemon-review-pass"

// reviewPassHandlerEnabled reports whether a passing review lands and closes
// the task on its own. Only an explicit authoritative mode turns it on.
func reviewPassHandlerEnabled(wf Workflow) bool {
	return completionReactorMode(strings.TrimSpace(wf.CompletionReactor.Effective)) == completionReactorModeAuthoritative
}

// reconcileReviewCompletion is the pass handler. For every task in review it
// takes the result of the task's latest review attempt (none if that attempt
// recorded no result) and acts on it:
//
//   - pass: the daemon already ran the task's Verification commands in the task
//     worktree before it recorded the pass (validateReviewProposal). Recheck
//     that the pass still matches the task, its source, proof and material;
//     check wave membership, owned_paths, and close eligibility; land exactly
//     the reviewed commit; then close.
//   - changes_requested: send the findings back to the worker.
//
// The reviewed commit is durable in the stored result. After a crash between
// land and close, the next poll finds that commit already on the integration
// branch and only closes. One task's failure never stops the others.
func (d *Daemon) reconcileReviewCompletion(project RegisteredProject, wf Workflow) error {
	if d == nil || d.store == nil || !reviewPassHandlerEnabled(wf) {
		return nil
	}
	rows, err := d.store.ListReviewResults(project.ProjectID)
	if err != nil {
		return err
	}
	byTask := map[string][]ReviewResult{}
	for _, row := range rows {
		if row.Repair == nil {
			byTask[row.TaskID] = append(byTask[row.TaskID], row.Result)
		}
	}
	taskIDs := make([]string, 0, len(byTask))
	for taskID := range byTask {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Strings(taskIDs)
	for _, taskID := range taskIDs {
		task, err := resolveV7Note(project.VaultRoot, taskID, "task")
		if err != nil || stringField(task.Data, "status") != "review" {
			continue
		}
		result, ok := d.currentReviewResult(project.ProjectID, taskID, byTask[taskID])
		if !ok || result.WorkRevision != intField(task.Data, "work_revision") {
			continue
		}
		if err := d.handleReviewResult(project, task, result); err != nil {
			log.Printf("review pass handler %s/%s: %v", project.ProjectKey, taskID, err)
		}
	}
	return nil
}

// currentReviewResult returns the result recorded by the task's latest review
// attempt. When that attempt recorded nothing (for example a refused
// proposal), an older pass is not current and nothing is returned.
func (d *Daemon) currentReviewResult(projectID, taskID string, results []ReviewResult) (ReviewResult, bool) {
	rows, err := d.store.ListAttemptsForRun(projectID, taskID)
	if err != nil {
		return ReviewResult{}, false
	}
	attempts := map[string]RunAttempt{}
	latest := ""
	for _, attempt := range rows {
		attempts[attempt.AttemptID] = attempt
	}
	for _, attempt := range rows {
		if attempt.Lane == runLaneReview && (latest == "" || completionReviewAttemptAfter(attempts, attempt.AttemptID, latest, "", "")) {
			latest = attempt.AttemptID
		}
	}
	for _, result := range results {
		if latest != "" && result.AttemptID == latest {
			return result, true
		}
	}
	return ReviewResult{}, false
}

func (d *Daemon) handleReviewResult(project RegisteredProject, task Note, result ReviewResult) error {
	taskID := stringField(task.Data, "id")
	switch result.Verdict {
	case "changes_requested":
		findings := strings.TrimSpace(strings.Join(result.Findings, "\n"))
		if findings == "" {
			findings = strings.TrimSpace(result.Summary)
		}
		return returnReviewerFindingToImplementer(project.VaultRoot, taskID, findings, "daemon:review-pass")
	case "pass":
		return d.landAndClosePassingReview(project, task, result)
	}
	return nil
}

func (d *Daemon) landAndClosePassingReview(project RegisteredProject, task Note, result ReviewResult) error {
	taskID := stringField(task.Data, "id")
	run, err := d.store.FindRunScoped(project.ProjectID, taskID)
	if err != nil {
		return err
	}
	if run != nil && (isDispatchingLeaseState(run.LeaseState) || reviewPassHoldCode(run.ReasonCode)) {
		// A live attempt owns the task, or an earlier landing problem is
		// waiting for the owner. Either way there is nothing to do yet.
		return nil
	}
	wave, member := reviewPassWave(project.VaultRoot, task)
	if !member {
		// The task is not (or no longer) a member of the wave it points at.
		// Leave it for the owner's Land, which creates or repairs the unit.
		return nil
	}
	source := strings.TrimSpace(result.ImplementationSHA)
	integration := v7WaveIntegrationBranch(wave)
	landed := source != "" && gitRefExists(project.RepoRoot, "refs/heads/"+integration) && gitMergeBaseAncestor(project.RepoRoot, source, integration)
	if !landed {
		if reason := reviewPassDrift(d.store, project.VaultRoot, task, result); reason != "" {
			return d.holdReviewPass(run, RunFailureLandingFailed, "the review pass is stale ("+reason+"); review again or land by hand")
		}
		stray, undeclared, err := reviewPassStrayPaths(project, task, wave, source)
		if err != nil {
			return err
		}
		if undeclared {
			return d.holdReviewPass(run, RunFailureLandingFailed, "the task declares no owned_paths, so the daemon will not land its changes; check them and land by hand")
		}
		if len(stray) > 0 {
			finding := "The reviewed change touches files outside the task's owned_paths: " + strings.Join(stray, ", ") +
				". Move the change inside owned_paths, or ask the architect to widen them, then request review again."
			return returnReviewerFindingToImplementer(project.VaultRoot, taskID, finding, "daemon:review-pass")
		}
		if err := reviewPassClosePreflight(project.VaultRoot, taskID); err != nil {
			return d.holdReviewPass(run, RunFailureLandingFailed, "close would be refused, so nothing was landed: "+firstActionableLine("", err.Error()))
		}
		// Land exactly the reviewed commit, not whatever the task branch or
		// worktree points at now. The land path uses it under its lock.
		if err := landV7CmdAsReviewPass(Args{
			"vault": project.VaultRoot,
			"quiet": "true",
			"_pos0": taskID,
			"from":  d.latestExecuteWorkspace(project.ProjectID, taskID),
		}, map[string]string{taskID: source}); err != nil {
			code := RunFailureLandingFailed
			if strings.Contains(strings.ToLower(err.Error()), "conflict") {
				code = RunFailureMergeConflict
			}
			return d.holdReviewPass(run, code, firstActionableLine("", err.Error()))
		}
		if injectReviewPassCrash != nil {
			if err := injectReviewPassCrash("after_land"); err != nil {
				return err
			}
		}
	}
	if _, err := closeV7Task(project.VaultRoot, taskID, reviewPassActor, Args{
		"vault": project.VaultRoot, "quiet": "true", "local": "true", "by": reviewPassActor,
		"reason": "review passed (" + result.AttemptID + "); landed " + source + " and closed by the daemon",
	}, true); err != nil {
		return fmt.Errorf("close after landing: %w", err)
	}
	return nil
}

// injectReviewPassCrash is a test seam that stops the handler between the ref
// update and the close.
var injectReviewPassCrash func(point string) error

// reviewPassClosePreflight checks close eligibility before anything lands, so
// a refused close never leaves reviewed work on the integration branch alone.
func reviewPassClosePreflight(vaultPath, taskID string) error {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return err
	}
	task, ok := idx.Tasks[taskID]
	if !ok {
		return tuskerError(errorNotFound, "V7 task not found: "+taskID)
	}
	_, err = v7ClosePreflight(vaultPath, task, idx, v7ClosePreflightRequest{
		Args: Args{"vault": vaultPath, "local": "true", "by": reviewPassActor}, Actor: reviewPassActor, Action: "close",
		RequireReview: true, ExpectedTaskID: taskID, SkipCommandVerification: true,
	})
	return err
}

// reviewPassDrift reports why a stored pass no longer describes the task: a
// new task revision, a different implementation source, changed proof or
// gates, or implementation material that changed after review.
func reviewPassDrift(store *RuntimeStore, vaultPath string, task Note, result ReviewResult) string {
	if stringField(task.Data, "state_rev") != result.TaskStateRev {
		return "the task changed after review"
	}
	source, err := reviewImplementationSource(store, RunStatus{ProjectID: result.ProjectID, RecordID: result.TaskID, WorkRevision: result.WorkRevision}, task)
	if err != nil || source != result.ImplementationSHA {
		return "the implementation source changed after review"
	}
	proof, gates, err := reviewObjectiveSnapshots(vaultPath, task)
	if err == nil && (proof != result.ProofFingerprint || gates != result.GateFingerprint) && result.MaterialFingerprint != "" {
		proof, gates, err = reviewObjectiveSnapshotsForMaterial(vaultPath, task, result.MaterialFingerprint)
	}
	if err != nil {
		return "proof is unavailable: " + err.Error()
	}
	if proof != result.ProofFingerprint || gates != result.GateFingerprint {
		return "proof or gates changed after review"
	}
	if !reviewMaterialFingerprintValid(result.MaterialFingerprint) {
		return "the review has no implementation material fingerprint"
	}
	_, expected, err := reviewImplementationParent(store, vaultPath, result.ProjectID, result.TaskID, result.WorkRevision, result.ImplementationSHA, task)
	if err != nil {
		return "the implementation is unavailable: " + err.Error()
	}
	material, err := reviewAttemptMaterialFingerprint(store, result.ProjectID, result.TaskID, result.AttemptID, result.WorkRevision, result.ImplementationSHA)
	if err != nil || material != result.MaterialFingerprint || material != expected {
		return "the implementation material changed after review"
	}
	return ""
}

// reviewPassStrayPaths lists files the reviewed commit changes outside the
// task's owned_paths, its generated_outputs, and the task's own Tusker records
// (files in the vault named for the task). Renames count as a delete and an
// add, so both paths are checked. undeclared is true when the task declares no
// owned_paths or generated_outputs but the commit changes repository files:
// the daemon then leaves the landing to the owner.
func reviewPassStrayPaths(project RegisteredProject, task Note, wave Note, source string) ([]string, bool, error) {
	repoRoot := project.RepoRoot
	if source == "" || !v7GitRepo(repoRoot) {
		return nil, false, nil
	}
	taskID := strings.ToUpper(strings.TrimSpace(stringField(task.Data, "id")))
	owned, err := normalizeWorkspaceMaterialScope(normalizeList(task.Data["owned_paths"]))
	if err != nil {
		return nil, false, err
	}
	generated, err := taskGeneratedOutputScope(task)
	if err != nil {
		return nil, false, err
	}
	scope := append(owned, generated...)
	vaultRel, relErr := filepath.Rel(repoRoot, project.VaultRoot)
	if relErr != nil || vaultRel == "." || strings.HasPrefix(vaultRel, "..") {
		vaultRel = ""
	}
	target := "HEAD"
	if branch := v7WaveIntegrationBranch(wave); gitRefExists(repoRoot, "refs/heads/"+branch) {
		target = branch
	}
	base, err := gitOutputTrim(repoRoot, "merge-base", target, source)
	if err != nil {
		return nil, false, fmt.Errorf("owned_paths check cannot find the merge base: %w", err)
	}
	changed, err := gitCombined(repoRoot, "diff", "--no-renames", "--name-only", "-z", base, source)
	if err != nil {
		return nil, false, err
	}
	var stray []string
	for _, path := range strings.Split(changed, "\x00") {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path == "" || reviewPassTaskRecord(vaultRel, taskID, path) {
			continue
		}
		if len(scope) == 0 {
			return nil, true, nil
		}
		if !workspaceMaterialScopeContains(scope, path) {
			stray = append(stray, path)
		}
	}
	return stray, false, nil
}

// reviewPassTaskRecord reports whether path is one of the task's own Tusker
// records: a file inside the vault named for the task, or inside a folder
// named for it.
func reviewPassTaskRecord(vaultRel, taskID, path string) bool {
	if vaultRel == "" || taskID == "" || !strings.HasPrefix(path, filepath.ToSlash(vaultRel)+"/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, filepath.ToSlash(vaultRel)+"/"), "/") {
		name := strings.ToUpper(strings.TrimSuffix(part, filepath.Ext(part)))
		if name == taskID || strings.HasPrefix(name, taskID+"-") || strings.HasPrefix(name, taskID+".") || strings.HasPrefix(name, taskID+"_") {
			return true
		}
	}
	return false
}

func (d *Daemon) latestExecuteWorkspace(projectID, taskID string) string {
	attempts, err := d.store.ListAttemptsForRun(projectID, taskID)
	if err != nil {
		return ""
	}
	workspace, latest := "", ""
	for _, attempt := range attempts {
		if attempt.Lane == runLaneExecute && attempt.WorkspacePath != "" && attempt.StartedAt >= latest {
			workspace, latest = attempt.WorkspacePath, attempt.StartedAt
		}
	}
	return workspace
}

func reviewPassHoldCode(code string) bool {
	switch RunFailureReasonCode(code) {
	case RunFailureMergeConflict, RunFailureLandingFailed:
		return true
	}
	return false
}

// holdReviewPass records why a passing review did not land, so the task shows
// In review with the reason, and the handler stops retrying until the owner
// lands by hand or the task goes back to the worker.
func (d *Daemon) holdReviewPass(run *RunStatus, code RunFailureReasonCode, reason string) error {
	if run == nil {
		return fmt.Errorf("%s", reason)
	}
	updated := *run
	updated.ReasonCode = string(code)
	updated.LastError = limitLandingSummary(reason, 500)
	if err := d.upsertRunWithStream(*run, updated); err != nil {
		return err
	}
	return fmt.Errorf("%s", reason)
}

// reviewPassWave is the wave (or singleton delivery unit) the task lands into.
// Both directions must agree: the task points at the wave and the wave lists
// the task (or names it as its delivery task). Arming governs dispatch, not
// landing reviewed work.
func reviewPassWave(vaultPath string, task Note) (Note, bool) {
	taskID := strings.ToUpper(strings.TrimSpace(stringField(task.Data, "id")))
	waveID := stringField(task.Data, "wave")
	if waveID == "" || taskID == "" {
		return Note{}, false
	}
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return Note{}, false
	}
	wave, ok := idx.Waves[waveID]
	if !ok {
		return Note{}, false
	}
	if v7ImplicitDeliveryUnit(wave) {
		return wave, strings.EqualFold(stringField(wave.Data, "delivery_task"), taskID)
	}
	for _, member := range normalizeList(wave.Data["members"]) {
		if strings.EqualFold(strings.TrimSpace(wikiTarget(member)), taskID) {
			return wave, true
		}
	}
	return Note{}, false
}
