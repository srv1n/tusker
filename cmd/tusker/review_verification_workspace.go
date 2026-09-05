package main

import "strings"

// Resolve the exact execute parent rather than accepting a caller-controlled
// working directory or silently testing the unchanged base checkout.
func reviewCommandVerificationWorkspace(store *RuntimeStore, vault string, note Note, run RunStatus) (*v7VerificationWorkspace, error) {
	if _, pending := v7VerificationManifest(note.Data, parseV7VerificationRows(note.Body)); len(pending) == 0 {
		return nil, nil
	}
	source := firstNonEmpty(stringField(note.Data, "source_sha"), stringField(note.Data, "source_commit"))
	parent, material, err := reviewAttemptImplementation(store, run.ProjectID, run.RecordID, run.ActiveAttemptID, run.WorkRevision, source)
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
		scope, err := canonicalTaskMaterialScope(vault, current)
		if err != nil {
			return err
		}
		if strings.Join(scope, "\x00") != strings.Join(parent.EndState.MaterialScope, "\x00") ||
			firstNonEmpty(stringField(current.Data, "source_sha"), stringField(current.Data, "source_commit")) != source ||
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
		return nil
	}
	if err := verify(); err != nil {
		return nil, err
	}
	return &v7VerificationWorkspace{Path: parent.WorkspacePath, Verify: verify}, nil
}
