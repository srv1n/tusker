package main

import (
	"time"
)

// Interactive submissions already capture the actual HEAD and scoped working
// tree material. Bind that completed candidate to review without changing the
// checkout or claiming that its uncommitted material is part of HEAD.
func projectInteractiveSubmissionToCanonical(store *RuntimeStore, vault string, run RunStatus, actor string) error {
	task, err := resolveV7Note(vault, run.ItemID, "task")
	if err != nil {
		return err
	}
	if trackerRecordID(task) != run.RecordID || stringField(task.Data, "status") != "review" || intField(task.Data, "work_revision") != run.WorkRevision {
		return tuskerError("CAS_CONFLICT", "interactive submission task identity or revision changed")
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return err
	}
	auth, err := store.LatestRunAuthorization(run.ProjectID, run.RecordID)
	if err != nil || auth == nil || auth.LeaseGeneration != run.LeaseGeneration || auth.Actor != actor {
		return firstNonNil(err, tuskerError("CAS_CONFLICT", "interactive submission authorization changed"))
	}
	for _, attempt := range attempts {
		if attempt.AttemptID != auth.AttemptID || attempt.Lane != runLaneExecute || attempt.WorkRevision != run.WorkRevision || attempt.Outcome != string(AttemptOutcomeSucceeded) {
			continue
		}
		if attempt.EndState.HeadSHA == "" {
			return tuskerError(errorInvalidTransition, "interactive submission lacks captured source identity")
		}
		tx, err := store.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		revision := run.WorkRevision + 1
		result, err := tx.Exec(`UPDATE runs SET work_revision=? WHERE project_id=? AND record_id=? AND work_revision=? AND lease_generation=? AND lease_state='released' AND attempt_outcome='succeeded'`, revision, run.ProjectID, run.RecordID, run.WorkRevision, run.LeaseGeneration)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		if err != nil || rows != 1 {
			return firstNonNil(err, tuskerError("CAS_CONFLICT", "interactive submission ownership changed"))
		}
		attempt.WorkRevision = revision
		if err := saveAttempt(tx, attempt); err != nil {
			return err
		}
		baseRev := stringField(task.Data, "state_rev")
		task.Data["work_revision"], task.Data["source_sha"] = revision, attempt.EndState.HeadSHA
		task.Data["updated_at"], task.Data["updated_by"] = time.Now().UTC().Format(time.RFC3339), actor
		if _, err := saveV7DocumentCAS(task.AbsolutePath, task.Data, task.Body, v7FrontmatterOrder["task"], baseRev); err != nil {
			return err
		}
		return tx.Commit()
	}
	return tuskerError(errorInvalidTransition, "interactive submission lacks its completed execute attempt")
}
