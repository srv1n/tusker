package main

import (
	"strings"
	"time"
)

// recoverVerificationChecks is the one recovery transition shared by Serve
// and the CLI. It verifies the submitted execute workspace, then reopens and
// queues only the independent review lane; attempts stay in the runtime log.
func recoverVerificationChecks(vault string, store *RuntimeStore, projectID, taskID, actor string, maxAttempts int, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "rerun_checks", TaskID: taskID}
	task, err := resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	run, err := store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil {
		return result, err
	}
	if run == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	workspace, err := recoveryCommandVerificationWorkspace(store, vault, task, *run)
	if err != nil {
		result.Refused, result.Lane, result.Reason = true, "verify", err.Error()
		return result, nil
	}
	waveID := strings.TrimSpace(stringField(task.Data, "wave"))
	wave, err := resolveV7Note(vault, waveID, "wave")
	if err != nil {
		return result, err
	}
	if reason := reviewRecoveryOperationalBlocker(wave, *run, maxAttempts); reason != "" {
		result.Refused, result.Lane, result.Reason = true, runLaneReview, reason
		return result, nil
	}
	// Proof is recorded before the task reopens for review: a failed command
	// must leave the member at its prior status so the projection keeps
	// offering rerun_checks instead of a misleading awaiting-review dead end.
	if v7VerificationReceiptInvalidationForWorkspace(vault, task, workspace) != nil {
		_, report, failures, err := executeV7CommandVerificationRowsInWorkspace(vault, task, Args{"rerun-invalid": "true"}, actor, true, workspace)
		if err != nil {
			result.Refused, result.Lane, result.Reason = true, "verify", err.Error()
			return result, nil
		}
		if len(failures) > 0 || report.Status != "satisfied" {
			reason := "verification did not pass"
			if len(failures) > 0 {
				reason = firstNonEmpty(failures[0].Message, reason)
			}
			result.Refused, result.Lane, result.Reason = true, "verify", reason
			return result, nil
		}
		task, err = resolveV7Note(vault, taskID, "task")
		if err != nil {
			return result, err
		}
	}
	if err := statusV7CmdAsInternalActor(Args{
		"vault": vault, "quiet": "true", "id": taskID, "status": "review",
		"task-rev": stringField(task.Data, "state_rev"),
		"reason":   "verification recovery passed; independent review required",
	}, "tusker:recovery"); err != nil {
		return result, err
	}
	task, err = resolveV7Note(vault, taskID, "task")
	if err != nil {
		return result, err
	}
	run, err = store.FindRunScoped(projectID, trackerRecordID(task))
	if err != nil || run == nil {
		if err == nil {
			err = tuskerError(errorNotFound, "project-scoped run not found after verification")
		}
		return result, err
	}
	review, err := queueReviewRecovery(store, task, wave, *run, actor, maxAttempts, now, true)
	if err != nil {
		return result, err
	}
	if !review.OK {
		return result, tuskerError(errorInvalidTransition, "checks passed, but independent review could not be queued: "+review.Reason)
	}
	result.OK, result.Admitted, result.Lane = true, review.Admitted, runLaneReview
	result.Reason = "current verification commands passed and were recorded; independent review queued"
	return result, nil
}

func workRecoverCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	id, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	if args.String("action") != "rerun_checks" {
		return tuskerError(errorInvalidArg, "work recover requires --action rerun_checks")
	}
	actor, err := directStartActor(args, "work recover")
	if err != nil {
		return err
	}
	store, err := OpenRuntimeStore(DefaultStateRoot())
	if err != nil {
		return err
	}
	defer store.Close()
	projectID, registered, err := registeredProjectIDForVault(store, vault)
	if err != nil {
		return err
	}
	if !registered {
		return tuskerError(errorNotFound, "work recover requires a registered project for the selected vault")
	}
	if requested := strings.TrimSpace(args.String("project")); requested != "" && requested != projectID {
		return tuskerError(errorInvalidTransition, "work recover project does not match the selected vault")
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		return err
	}
	result, err := recoverVerificationChecks(vault, store, projectID, id, actor, wf.Data.Retry.MaxAttempts, time.Now().UTC())
	if err != nil {
		return err
	}
	if result.Admitted {
		_ = sendDaemonControlOneWay(DefaultStateRoot(), daemonControlRequest{Command: "reconcile_project", ProjectID: projectID, Cause: "verification_recovery", Changes: []daemonControlChange{{ID: id, Kind: "run"}}}, 250*time.Millisecond)
	}
	emitJSON(result)
	return nil
}
