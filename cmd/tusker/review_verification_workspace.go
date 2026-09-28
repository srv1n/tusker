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
	reviewPath, err := sharedReviewWorkspace(store, run)
	if err != nil {
		return nil, err
	}
	verify := func() error {
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
		bound, currentMaterial, err := reviewAttemptImplementation(store, run.ProjectID, run.RecordID, run.ActiveAttemptID, run.WorkRevision, source)
		if err != nil {
			return err
		}
		if bound.AttemptID != parent.AttemptID || bound.WorkspacePath != parent.WorkspacePath || currentMaterial != material {
			return tuskerError(errorInvalidTransition, "review verification implementation binding changed")
		}
		if reviewPath != "" {
			return reviewWorktreeMatchesSubmission(reviewPath, parent)
		}
		return nil
	}
	if err := verify(); err != nil {
		return nil, err
	}
	return &v7VerificationWorkspace{Path: firstNonEmpty(reviewPath, parent.WorkspacePath), Verify: verify, MaterialFingerprint: material}, nil
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
	verify := func() error {
		current, err := resolveV7Note(vault, trackerRecordID(note), "task")
		if err != nil {
			return err
		}
		bound, currentMaterial, err := reviewImplementationParent(store, vault, run.ProjectID, trackerRecordID(current), intField(current.Data, "work_revision"), firstNonEmpty(stringField(current.Data, "source_sha"), stringField(current.Data, "source_commit")), current)
		if err != nil {
			return err
		}
		if bound.AttemptID != parent.AttemptID || bound.WorkspacePath != parent.WorkspacePath || currentMaterial != material {
			return tuskerError(errorInvalidTransition, "verification recovery implementation binding changed")
		}
		return nil
	}
	if err := verify(); err != nil {
		return nil, err
	}
	return &v7VerificationWorkspace{Path: parent.WorkspacePath, Verify: verify, MaterialFingerprint: material}, nil
}
