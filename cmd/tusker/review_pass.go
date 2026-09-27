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
// the task on its own: mode authoritative, the default. disabled (and the old
// shadow) leave landing and closing to the owner.
func reviewPassHandlerEnabled(wf Workflow) bool {
	switch completionReactorMode(strings.TrimSpace(wf.CompletionReactor.Effective)) {
	case completionReactorModeDisabled, completionReactorModeShadow:
		return false
	}
	return true
}

// reconcileReviewCompletion is the pass handler. For every task in review it
// takes the result of the latest review attempt and acts on it:
//
//   - pass: the daemon already ran the task's Verification commands in the task
//     worktree before it recorded the pass (validateReviewProposal). Check the
//     diff stays inside owned_paths, land to the wave's integration branch,
//     then close the task.
//   - changes_requested: send the findings back to the worker.
//
// Every step is idempotent: a landed source is already an ancestor of the
// integration branch, and a closed task is no longer in review. A crash between
// land and close therefore finishes on the next poll. One task's failure never
// stops the others.
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
		result, ok := d.latestReviewResult(project.ProjectID, taskID, byTask[taskID])
		if !ok || result.WorkRevision != intField(task.Data, "work_revision") {
			continue
		}
		if err := d.handleReviewResult(project, task, result); err != nil {
			log.Printf("review pass handler %s/%s: %v", project.ProjectKey, taskID, err)
		}
	}
	return nil
}

// latestReviewResult picks the result of the most recent review attempt. Older
// results describe work the worker has since changed.
func (d *Daemon) latestReviewResult(projectID, taskID string, results []ReviewResult) (ReviewResult, bool) {
	if len(results) == 0 {
		return ReviewResult{}, false
	}
	attempts := map[string]RunAttempt{}
	if rows, err := d.store.ListAttemptsForRun(projectID, taskID); err == nil {
		for _, attempt := range rows {
			attempts[attempt.AttemptID] = attempt
		}
	}
	latest := results[0]
	for _, candidate := range results[1:] {
		if completionReviewAttemptAfter(attempts, candidate.AttemptID, latest.AttemptID, candidate.CreatedAt, latest.CreatedAt) {
			latest = candidate
		}
	}
	return latest, true
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
		// A live attempt owns the task, or an earlier landing failure is
		// waiting for the owner. Either way there is nothing to do yet.
		return nil
	}
	if stray, err := reviewPassStrayPaths(project, task, result.ImplementationSHA); err != nil {
		return err
	} else if len(stray) > 0 {
		finding := "The reviewed change touches files outside the task's owned_paths: " + strings.Join(stray, ", ") +
			". Move the change inside owned_paths, or ask the architect to widen them, then request review again."
		return returnReviewerFindingToImplementer(project.VaultRoot, taskID, finding, "daemon:review-pass")
	}
	if err := landV7CmdAsWaveDrain(Args{
		"vault": project.VaultRoot,
		"quiet": "true",
		"_pos0": taskID,
		"from":  d.latestExecuteWorkspace(project.ProjectID, taskID),
	}); err != nil {
		return d.holdReviewPassForLanding(run, err)
	}
	if _, err := closeV7Task(project.VaultRoot, taskID, reviewPassActor, Args{
		"vault": project.VaultRoot, "quiet": "true", "local": "true", "by": reviewPassActor,
		"reason": "review passed (" + result.AttemptID + "); landed and closed by the daemon",
	}, true); err != nil {
		return fmt.Errorf("close after landing: %w", err)
	}
	return nil
}

// reviewPassStrayPaths lists files the reviewed commit changes outside the
// task's owned_paths, generated_outputs, and the Tusker vault. A task that
// declares no paths has nothing to enforce.
func reviewPassStrayPaths(project RegisteredProject, task Note, source string) ([]string, error) {
	repoRoot := project.RepoRoot
	source = strings.TrimSpace(source)
	if source == "" || !v7GitRepo(repoRoot) {
		return nil, nil
	}
	scope, err := canonicalTaskMaterialScope(project.VaultRoot, task)
	if err != nil {
		return nil, err
	}
	generated, err := taskGeneratedOutputScope(task)
	if err != nil {
		return nil, err
	}
	scope = append(scope, generated...)
	if len(scope) == 0 {
		return nil, nil
	}
	if vaultRel, relErr := filepath.Rel(repoRoot, project.VaultRoot); relErr == nil && vaultRel != "." && !strings.HasPrefix(vaultRel, "..") {
		scope = append(scope, filepath.ToSlash(vaultRel))
	}
	target := "HEAD"
	if wave, ok := completionWaveForReviewedTask(project.VaultRoot, task); ok {
		if branch := v7WaveIntegrationBranch(wave); gitRefExists(repoRoot, "refs/heads/"+branch) {
			target = branch
		}
	}
	base, err := gitOutputTrim(repoRoot, "merge-base", target, source)
	if err != nil {
		return nil, fmt.Errorf("owned_paths check cannot find the merge base: %w", err)
	}
	changed, err := gitOutputTrim(repoRoot, "diff", "--name-only", base, source)
	if err != nil {
		return nil, err
	}
	var stray []string
	for _, path := range strings.Split(changed, "\n") {
		if path = strings.TrimSpace(path); path != "" && !workspaceMaterialScopeContains(scope, path) {
			stray = append(stray, path)
		}
	}
	return stray, nil
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

// holdReviewPassForLanding records a landing failure on the run so the task
// shows In review with the reason, and the handler stops retrying until the
// owner lands by hand or the task goes back to the worker.
func (d *Daemon) holdReviewPassForLanding(run *RunStatus, landErr error) error {
	if run == nil {
		return landErr
	}
	summary := firstActionableLine("", landErr.Error())
	code := RunFailureLandingFailed
	if strings.Contains(strings.ToLower(landErr.Error()), "conflict") {
		code = RunFailureMergeConflict
	}
	updated := *run
	updated.ReasonCode = string(code)
	updated.LastError = limitLandingSummary(summary, 500)
	if err := d.upsertRunWithStream(*run, updated); err != nil {
		return err
	}
	return landErr
}
