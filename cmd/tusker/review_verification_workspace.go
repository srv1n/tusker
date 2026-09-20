package main

import "strings"

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
		if strings.Join(scope, "\x00") != strings.Join(parent.EndState.MaterialScope, "\x00") ||
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
		return nil
	}
	if err := verify(); err != nil {
		return nil, err
	}
	return &v7VerificationWorkspace{Path: parent.WorkspacePath, Verify: verify, MaterialFingerprint: material}, nil
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
