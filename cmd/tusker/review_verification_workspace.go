package main

import (
	"os/exec"
	"strings"
)

// Resolve the exact execute parent rather than accepting a caller-controlled
// working directory or silently testing the unchanged base checkout.
func reviewCommandVerificationWorkspace(store *RuntimeStore, vault string, note Note, run RunStatus) (*v7VerificationWorkspace, error) {
	hasCommand := false
	for _, row := range parseV7VerificationRows(note.Body) {
		if _, ok := v7VerificationCommand(row.Check); ok {
			hasCommand = true
			break
		}
	}
	if !hasCommand {
		return nil, nil
	}
	source, err := reviewImplementationSource(store, run, note)
	if err != nil {
		return nil, err
	}
	parent, material, err := reviewAttemptImplementation(store, run.ProjectID, run.RecordID, run.ActiveAttemptID, run.WorkRevision, source)
	if err != nil {
		return nil, err
	}
	baseline := implementationMaterialGitBaseline(parent)
	reviewPath, err := sharedReviewWorkspace(store, run)
	if err != nil {
		return nil, err
	}
	// bind re-checks everything but the material, which was just hashed;
	// verify adds the material, decided by git where git can see it.
	bind := func() error {
		attempt, err := store.ReviewAttempt(run.ActiveAttemptID)
		if err != nil {
			return err
		}
		active, err := activeReviewRunForAttempt(store, run.ProjectID, run.RecordID, attempt)
		if err != nil {
			return err
		}
		if active.LeaseGeneration != run.LeaseGeneration || active.LeaseOwner != run.LeaseOwner {
			return tuskerError(errorInvalidTransition, "review verification lease changed during command execution")
		}
		current, err := resolveV7Note(vault, run.RecordID, "task")
		if err != nil {
			return err
		}
		currentSource, err := reviewImplementationSource(store, run, current)
		if err != nil {
			return err
		}
		scope, err := canonicalTaskMaterialScope(vault, current)
		if err != nil {
			return err
		}
		generatedOutputScope, err := taskGeneratedOutputScope(current)
		if err != nil {
			return err
		}
		if !submittedMaterialScopeMatches(scope, parent) ||
			strings.Join(generatedOutputScope, "\x00") != strings.Join(parent.EndState.GeneratedOutputScope, "\x00") ||
			currentSource != source ||
			intField(current.Data, "work_revision") != run.WorkRevision {
			return tuskerError(errorInvalidTransition, "review verification implementation scope or source changed")
		}
		var parentID string
		if err := store.queryRowScan(`SELECT parent_attempt_id FROM attempts WHERE attempt_id=? AND project_id=? AND record_id=?`, []any{run.ActiveAttemptID, run.ProjectID, run.RecordID}, &parentID); err != nil {
			return err
		}
		bound, ok, err := storedExecuteSubmission(store, run.ProjectID, run.RecordID, func(a RunAttempt) bool { return a.AttemptID == parentID })
		if err != nil {
			return err
		}
		if !ok || !sameExecuteSubmission(bound, parent, run.WorkRevision, source) {
			return tuskerError(errorInvalidTransition, "review verification implementation binding changed")
		}
		if reviewPath != "" {
			return reviewWorktreeMatchesSubmission(reviewPath, parent)
		}
		return nil
	}
	verify := func() error {
		if err := bind(); err != nil {
			return err
		}
		return implementationMaterialFresh(parent, baseline)
	}
	if err := bind(); err != nil {
		return nil, err
	}
	return &v7VerificationWorkspace{Path: firstNonEmpty(reviewPath, parent.WorkspacePath), Verify: verify, MaterialFingerprint: material, Submission: &parent}, nil
}

// sharedReviewWorkspace returns the detached review worktree of the run's
// active review attempt, or "" when the reviewer runs in the run's own
// workspace. The run itself stays on the shared checkout so its submitted
// files remain owned there until they land.
func sharedReviewWorkspace(store *RuntimeStore, run RunStatus) (string, error) {
	if run.Lane != runLaneReview || strings.TrimSpace(run.ActiveAttemptID) == "" {
		return "", nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	for _, attempt := range attempts {
		if attempt.AttemptID == run.ActiveAttemptID && attempt.Lane == runLaneReview && strings.TrimSpace(attempt.WorkspacePath) != "" &&
			!sameCanonicalProjectPath(attempt.WorkspacePath, run.WorkspacePath) {
			return attempt.WorkspacePath, nil
		}
	}
	return "", nil
}

// reviewWorktreeMatchesSubmission binds a review worktree to the submitted
// commit: HEAD is the execute submission and the material scope is untouched.
// The material fingerprint itself is still checked on the shared checkout,
// where generated outputs and deletions were measured at submission.
func reviewWorktreeMatchesSubmission(path string, parent RunAttempt) error {
	head, err := gitOutputTrim(path, "rev-parse", "HEAD")
	if err != nil || head != parent.EndState.HeadSHA {
		return tuskerError(errorInvalidTransition, "review worktree is not at the submitted commit")
	}
	out, err := exec.Command("git", append([]string{"-C", path, "status", "--porcelain=v1", "--untracked-files=all", "--"}, parent.EndState.MaterialScope...)...).Output()
	if err != nil || strings.TrimSpace(string(out)) != "" {
		return tuskerError(errorInvalidTransition, "review worktree material changed from the submitted commit")
	}
	return nil
}

// Recovery verifies the submitted implementation, never the registered base
// checkout. Unlike active review, recovery has no live review lease to bind;
// the immutable execute attempt and its captured scope/source are the fence.
func recoveryCommandVerificationWorkspace(store *RuntimeStore, vault string, note Note, run RunStatus) (*v7VerificationWorkspace, error) {
	source := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	parent, material, err := reviewImplementationParent(store, vault, run.ProjectID, trackerRecordID(note), intField(note.Data, "work_revision"), source, note)
	if err != nil {
		return nil, err
	}
	baseline := implementationMaterialGitBaseline(parent)
	verify := func() error {
		current, err := resolveV7Note(vault, trackerRecordID(note), "task")
		if err != nil {
			return err
		}
		workRevision := intField(current.Data, "work_revision")
		currentSource := firstNonEmpty(stringField(current.Data, "source_sha"), stringField(current.Data, "source_commit"))
		scope, scopeErr := canonicalTaskMaterialScope(vault, current)
		generatedOutputScope, generatedErr := taskGeneratedOutputScope(current)
		if scopeErr != nil || generatedErr != nil || !submittedMaterialScopeMatches(scope, parent) ||
			strings.Join(generatedOutputScope, "\x00") != strings.Join(parent.EndState.GeneratedOutputScope, "\x00") {
			return tuskerError(errorInvalidTransition, "verification recovery implementation binding changed")
		}
		// The same first-match rule reviewImplementationParent applies.
		bound, ok, err := storedExecuteSubmission(store, run.ProjectID, trackerRecordID(current), func(a RunAttempt) bool {
			return a.Lane == runLaneExecute && a.WorkRevision == workRevision && a.Outcome == string(AttemptOutcomeSucceeded) && a.EndState.HeadSHA == currentSource
		})
		if err != nil {
			return err
		}
		if !ok || !sameExecuteSubmission(bound, parent, workRevision, currentSource) {
			return tuskerError(errorInvalidTransition, "verification recovery implementation binding changed")
		}
		return implementationMaterialFresh(parent, baseline)
	}
	return &v7VerificationWorkspace{Path: parent.WorkspacePath, Verify: verify, MaterialFingerprint: material, Submission: &parent}, nil
}

// storedExecuteSubmission returns the first stored attempt match selects,
// without reading its workspace.
func storedExecuteSubmission(store *RuntimeStore, projectID, recordID string, match func(RunAttempt) bool) (RunAttempt, bool, error) {
	attempts, err := store.ListAttemptsForRun(projectID, recordID)
	if err != nil {
		return RunAttempt{}, false, err
	}
	for _, attempt := range attempts {
		if match(attempt) {
			return attempt, true, nil
		}
	}
	return RunAttempt{}, false, nil
}

// sameExecuteSubmission reports whether current is still the successful
// execute submission parent for this revision and source.
func sameExecuteSubmission(current, parent RunAttempt, workRevision int, source string) bool {
	return current.AttemptID == parent.AttemptID && current.WorkspacePath == parent.WorkspacePath &&
		current.Lane == runLaneExecute && current.Outcome == string(AttemptOutcomeSucceeded) &&
		current.WorkRevision == workRevision && current.EndState.HeadSHA == source &&
		current.EndState.MaterialFingerprint == parent.EndState.MaterialFingerprint
}

// implementationMaterialGitBaseline is taken right after the submission's
// material hash matched. True means the scope then held exactly the submitted
// commit's content, so the stored fingerprint is that content's hash.
func implementationMaterialGitBaseline(parent RunAttempt) bool {
	return len(parent.EndState.GeneratedOutputScope) == 0 &&
		gitScopeAtCommit(parent.WorkspacePath, parent.EndState.HeadSHA, parent.EndState.MaterialScope)
}

// implementationMaterialFresh decides whether proof bound to the submission
// still matches its workspace. Git decides when it can: with a clean baseline,
// a scope that still holds exactly the submitted commit's content is the
// hashed material. A later commit or uncommitted edit in scope, a dirty
// submission, or generated outputs git ignores fall back to the content hash.
func implementationMaterialFresh(parent RunAttempt, baseline bool) error {
	if baseline && gitScopeAtCommit(parent.WorkspacePath, parent.EndState.HeadSHA, parent.EndState.MaterialScope) {
		return nil
	}
	_, err := verifiedImplementationWorkspaceMaterial(parent)
	return err
}

// gitScopeAtCommit reports whether scope in workspace holds exactly its
// content at commit: no later commit touched it and nothing in it is staged,
// modified, deleted or untracked. Ignored files are invisible to it.
func gitScopeAtCommit(workspace, commit string, scope []string) bool {
	if strings.TrimSpace(workspace) == "" || strings.TrimSpace(commit) == "" || len(scope) == 0 {
		return false
	}
	head, err := gitOutputTrim(workspace, "rev-parse", "HEAD")
	if err != nil {
		return false
	}
	if head != commit && exec.Command("git", append([]string{"-C", workspace, "--literal-pathspecs", "diff", "--quiet", commit, head, "--"}, scope...)...).Run() != nil {
		return false
	}
	out, err := exec.Command("git", append([]string{"-C", workspace, "--literal-pathspecs", "status", "--porcelain=v1", "--untracked-files=all", "--"}, scope...)...).Output()
	return err == nil && strings.TrimSpace(string(out)) == ""
}
