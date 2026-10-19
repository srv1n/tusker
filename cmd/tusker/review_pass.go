package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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
func (d *Daemon) reconcileReviewCompletion(ctx context.Context, project RegisteredProject, wf Workflow) error {
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
	if len(taskIDs) == 0 {
		return nil
	}
	// A pass lands through the project's full gate, which can run far longer
	// than a poll. Off the poll, one worker lands the passes one at a time and
	// rereads each task first; passes found while it runs wait for a later poll.
	// One vault load serves every candidate; resolving each task separately
	// reloaded the whole vault per review result.
	idx, err := loadV7Index(project.VaultRoot)
	if err != nil {
		return err
	}
	landing := d.reviewPassAsync && d.reviewPassLanding.CompareAndSwap(false, true)
	var passes []string
	skipped := false
	for _, taskID := range taskIDs {
		task, result, ok := d.reviewPassCandidate(project, idx, taskID, byTask[taskID])
		if !ok {
			continue
		}
		if result.Verdict == "pass" && d.reviewPassAsync {
			if landing {
				passes = append(passes, taskID)
			} else {
				skipped = true
			}
			continue
		}
		if err := d.handleReviewResult(ctx, project, task, result); err != nil {
			log.Printf("review pass handler %s/%s: %v", project.ProjectKey, taskID, err)
		}
	}
	if skipped {
		d.deferReviewPass(project.ProjectID)
	}
	if !landing {
		return nil
	}
	if len(passes) == 0 {
		d.releaseReviewPassLanding()
		return nil
	}
	go func() {
		changed := false
		defer func() {
			// Wake only when a landing rewrote a task. A refused landing
			// (drifted base, unauthorized wave) changes nothing, and waking
			// on it re-runs the same refused pass in a tight loop.
			if changed {
				d.releaseReviewPassLanding()
				d.scheduleProjectReconcile(project.ProjectID)
				return
			}
			d.releaseReviewPassLandingExcept(project.ProjectID)
		}()
		for _, taskID := range passes {
			idx, err := loadV7Index(project.VaultRoot)
			if err != nil {
				log.Printf("review pass handler %s/%s: %v", project.ProjectKey, taskID, err)
				continue
			}
			task, result, ok := d.reviewPassCandidate(project, idx, taskID, byTask[taskID])
			if !ok {
				continue
			}
			before, _ := os.Stat(task.AbsolutePath)
			if err := d.handleReviewResult(ctx, project, task, result); err != nil {
				log.Printf("review pass handler %s/%s: %v", project.ProjectKey, taskID, err)
			}
			if after, err := os.Stat(task.AbsolutePath); err != nil || before == nil ||
				!after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
				changed = true
			}
		}
	}()
	return nil
}

// reviewPassDeferred holds, per daemon, the projects whose passes waited
// because another project's landing held the single landing worker. The lock
// orders a deferral against the worker's release, so none is lost.
var reviewPassDeferred = struct {
	sync.Mutex
	projects map[*Daemon]map[string]bool
}{projects: map[*Daemon]map[string]bool{}}

func (d *Daemon) deferReviewPass(projectID string) {
	reviewPassDeferred.Lock()
	if d.reviewPassLanding.Load() {
		if reviewPassDeferred.projects[d] == nil {
			reviewPassDeferred.projects[d] = map[string]bool{}
		}
		reviewPassDeferred.projects[d][projectID] = true
		reviewPassDeferred.Unlock()
		return
	}
	reviewPassDeferred.Unlock()
	// The worker freed after this poll lost the race for it.
	d.scheduleProjectReconcile(projectID)
}

// releaseReviewPassLanding frees the landing worker and wakes every project
// that waited for it.
func (d *Daemon) releaseReviewPassLanding() {
	d.releaseReviewPassLandingExcept("")
}

// releaseReviewPassLandingExcept frees the worker without waking skip: that
// project's own landing just ran and changed nothing, so another pass would
// only repeat it.
func (d *Daemon) releaseReviewPassLandingExcept(skip string) {
	reviewPassDeferred.Lock()
	d.reviewPassLanding.Store(false)
	waiting := reviewPassDeferred.projects[d]
	delete(reviewPassDeferred.projects, d)
	reviewPassDeferred.Unlock()
	for projectID := range waiting {
		if projectID != skip {
			d.scheduleProjectReconcile(projectID)
		}
	}
}

// reviewPassCandidate returns the task and its current review result when the
// task is in review and the result matches its work revision.
func (d *Daemon) reviewPassCandidate(project RegisteredProject, idx v7Index, taskID string, results []ReviewResult) (Note, ReviewResult, bool) {
	task, err := resolveV7NoteFromIndex(idx, taskID, "task")
	if err != nil || stringField(task.Data, "status") != "review" {
		return Note{}, ReviewResult{}, false
	}
	result, ok := d.currentReviewResult(project.ProjectID, taskID, results)
	if !ok || result.WorkRevision != intField(task.Data, "work_revision") {
		return Note{}, ReviewResult{}, false
	}
	return task, result, true
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
	latestExecute := ""
	for _, attempt := range rows {
		if attempt.Lane == runLaneReview && (latest == "" || completionReviewAttemptAfter(attempts, attempt.AttemptID, latest, "", "")) {
			latest = attempt.AttemptID
		}
		if attempt.Lane == runLaneExecute && (latestExecute == "" || completionReviewAttemptAfter(attempts, attempt.AttemptID, latestExecute, "", "")) {
			latestExecute = attempt.AttemptID
		}
	}
	// A rework resubmission keeps the work revision, so until its own review
	// runs, the previous review's verdict still looked current and was applied
	// again to the new commit.
	if latest != "" && latestExecute != "" && completionReviewAttemptAfter(attempts, latestExecute, latest, "", "") {
		return ReviewResult{}, false
	}
	for _, result := range results {
		if latest != "" && result.AttemptID == latest {
			return result, true
		}
	}
	return ReviewResult{}, false
}

func (d *Daemon) handleReviewResult(ctx context.Context, project RegisteredProject, task Note, result ReviewResult) error {
	taskID := stringField(task.Data, "id")
	switch result.Verdict {
	case "changes_requested":
		findings := strings.TrimSpace(strings.Join(result.Findings, "\n"))
		if findings == "" {
			findings = strings.TrimSpace(result.Summary)
		}
		return returnReviewerFindingToImplementer(project.VaultRoot, taskID, findings, "daemon:review-pass")
	case "pass":
		return d.landAndClosePassingReview(ctx, project, task, result)
	}
	return nil
}

func (d *Daemon) landAndClosePassingReview(ctx context.Context, project RegisteredProject, task Note, result ReviewResult) error {
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
	// Every check runs on every poll, including a resume after a stop between
	// landing and close: a pass that went stale while the handler was stopped
	// must not close the task.
	if reason := reviewPassDrift(d.store, project.VaultRoot, task, result); reason != "" {
		return d.holdReviewPass(run, RunFailureLandingFailed, "the review pass is stale ("+reason+"); review again or land by hand")
	}
	stray, undeclared, err := reviewPassStrayPaths(project, task, wave, source)
	if err != nil {
		return err
	}
	if parent, _, parentErr := reviewAttemptImplementation(d.store, project.ProjectID, taskID, result.AttemptID, result.WorkRevision, source); parentErr == nil && !parent.EndStateInvalid {
		for _, path := range parent.EndState.StrayPaths {
			for i := 0; i < len(stray); i++ {
				if stray[i] == path {
					stray = append(stray[:i], stray[i+1:]...)
					break
				}
			}
		}
		// EndState.Overlaps names other tasks' dirty files left out of this
		// commit. They are not dependencies: the review tested this commit
		// alone in its own worktree, and waiting on them deadlocked parallel
		// tasks that each saw the other's files.
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
		return d.holdReviewPass(run, RunFailureLandingFailed, "close would be refused, so nothing more was landed or closed: "+firstActionableLine("", err.Error()))
	}
	integration := v7WaveIntegrationBranch(wave)
	if source != "" && gitRefExists(project.RepoRoot, "refs/heads/"+integration) && gitMergeBaseAncestor(project.RepoRoot, source, integration) {
		// Resume after a stop: the ref already moved. Close only once this
		// task's own landing receipt is verified and its audit row is written.
		if err := recoverV7ReviewPassLanding(project.VaultRoot, taskID, stringField(wave.Data, "id"), source); err != nil {
			return d.holdReviewPass(run, RunFailureLandingFailed, firstActionableLine("", err.Error())+"; land by hand")
		}
	} else {
		// Restarts that keep interrupting a landing open the crash-loop
		// breaker; then the gate must not start again until the operator
		// resumes. The reason stays on the run, with no hold code, so the
		// landing resumes on its own once the breaker closes.
		if reason, err := d.crashLoopDispatchBlocker(); err != nil {
			return err
		} else if reason != "" {
			if run != nil {
				updated := *run
				updated.LastError = "landing paused: " + reason
				if run.LastError != updated.LastError {
					if err := d.upsertRunWithStream(*run, updated); err != nil {
						return err
					}
				}
			}
			return nil
		}
		// Land exactly the reviewed commit, not whatever the task branch or
		// worktree points at now. The land path uses it under its lock.
		daemonSubmissionSources := map[string]string{}
		if parent, _, err := reviewAttemptImplementation(d.store, project.ProjectID, taskID, result.AttemptID, result.WorkRevision, source); err == nil &&
			parent.EndState.Dirty && !parent.EndStateInvalid {
			daemonSubmissionSources[taskID] = source
		}
		stopBeat := func() {}
		if !d.reviewPassAsync {
			stopBeat = d.feedWatchdogDuring(reviewPassLandingWatchdogCap)
		}
		landErr := landV7CmdAsReviewPass(ctx, Args{
			"vault": project.VaultRoot,
			"quiet": "true",
			"_pos0": taskID,
			"from":  d.latestExecuteWorkspace(project.ProjectID, taskID),
		}, map[string]string{taskID: source}, daemonSubmissionSources)
		stopBeat()
		if ctx != nil && ctx.Err() != nil {
			// The daemon is shutting down and cancelled the gate; that is not a
			// landing failure. The next daemon lands it again.
			return ctx.Err()
		}
		if err := landErr; err != nil {
			// Only the merge step reports a conflict. A gate command's
			// output can mention one (a test that exercises conflicts).
			// A real conflict with current main is the task's to resolve,
			// so it goes back to the worker with the conflict named.
			if strings.Contains(err.Error(), "gate failed: merge ") && strings.Contains(strings.ToLower(err.Error()), "conflict") {
				finding := "The reviewed commit conflicts with current " + integration + ": " +
					firstActionableLine("", err.Error()) + ". Bring the task onto current " + integration + ", resolve the conflict, then request review again."
				return returnReviewerFindingToImplementer(project.VaultRoot, taskID, finding, "daemon:review-pass")
			}
			// A failing test is the implementation's to fix, so it goes back
			// to the worker. A broken host waits for the owner.
			if strings.Contains(err.Error(), "--- FAIL: ") {
				finding := "The landing gate failed on the reviewed commit merged into " + integration + ": " +
					firstActionableLine("", err.Error()) + ". Fix the failing tests, run them, then request review again."
				return returnReviewerFindingToImplementer(project.VaultRoot, taskID, finding, "daemon:review-pass")
			}
			return d.holdReviewPass(run, RunFailureLandingFailed, firstActionableLine("", err.Error()))
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
	}, true, source); err != nil {
		return fmt.Errorf("close after landing: %w", err)
	}
	return nil
}

// reviewPassLandingWatchdogCap bounds how long an inline landing gate (a
// one-shot daemon, or a test) may keep the watchdog fed. The resident daemon
// lands off the poll, so its beat never needs feeding.
const reviewPassLandingWatchdogCap = 60 * time.Minute

var reviewPassWatchdogFeedInterval = 30 * time.Second

// feedWatchdogDuring keeps the watchdog beat fresh while a known long step
// runs, up to max. A step that outlives max lets the watchdog fire as a hang.
func (d *Daemon) feedWatchdogDuring(max time.Duration) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(reviewPassWatchdogFeedInterval)
		defer ticker.Stop()
		deadline := time.After(max)
		for {
			select {
			case <-done:
				return
			case <-deadline:
				return
			case now := <-ticker.C:
				_ = d.feedWatchdogBeat(now.UTC())
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
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
	base, err := gitOutputTrim(repoRoot, "merge-base", "HEAD", source)
	if err != nil {
		return nil, false, fmt.Errorf("owned_paths check cannot find the merge base: %w", err)
	}
	// Compare against the nearer of HEAD and the wave's integration branch.
	// A shared checkout lands on HEAD and never advances the integration
	// branch, so diffing from its older base blamed the task for commits
	// already on HEAD, such as the one that authored the wave.
	if branch := v7WaveIntegrationBranch(wave); gitRefExists(repoRoot, "refs/heads/"+branch) {
		if waveBase, waveErr := gitOutputTrim(repoRoot, "merge-base", branch, source); waveErr == nil && waveBase != base && gitMergeBaseAncestor(repoRoot, base, waveBase) {
			base = waveBase
		}
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
	// The landing may run off the poll, so the row can have moved since run
	// was read; write the hold onto the current row.
	if current, err := d.store.FindRunScoped(run.ProjectID, run.RecordID); err == nil && current != nil {
		run = current
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
