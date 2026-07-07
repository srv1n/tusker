package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (d *Daemon) autoAdvanceExternalLoop(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, notes []Note, note Note, run RunStatus) (RunStatus, bool, error) {
	if !daemonShouldAutoAdvanceExternalRun(wfFile.Data, note, run) {
		return run, false, nil
	}
	jobID := firstNonEmpty(strings.TrimSpace(run.CloudTaskID), strings.TrimSpace(run.ApplyRef))
	if jobID == "" {
		return d.recordDaemonExternalLoopBlock(project, wfFile, notes, note, run, "completed external runner did not expose cloud_task_id/apply_ref")
	}
	handled, err := externalLoopJobAlreadyHandled(d.store, project.ProjectID, run.RecordID, jobID)
	if err != nil {
		return run, false, err
	}
	if handled {
		return run, false, nil
	}

	collectCtx, err := d.automationContextForDaemon(project, wfFile, notes, run)
	if err != nil {
		return run, false, err
	}
	applyRunner := externalLoopDefaultApplyRunner(wfFile.Data, run.Runner)
	collectArgs := Args{"runner": run.Runner, "apply-runner": applyRunner}
	if command := externalLoopCollectCommandForRunner(wfFile.Data, run.Runner); command != "" {
		collectArgs["fetch-command"] = command
	}
	collect, err := collectCtx.collectExternal(note, run, run.Runner, jobID, collectArgs)
	if err != nil {
		return d.recordDaemonExternalLoopBlock(project, wfFile, notes, note, run, "external collect failed for "+jobID+": "+err.Error())
	}

	policyCtx := collectCtx
	dispatchExplanation := collectCtx.explainTask(note)
	if collect.NextAction == externalLoopActionApplyPatch {
		applyRun := externalLoopApplyDispatchRun(project, note, run, applyRunner)
		policyCtx, err = d.automationContextForDaemon(project, wfFile, notes, applyRun)
		if err != nil {
			return run, false, err
		}
		dispatchExplanation = policyCtx.explainTaskForRunner(note, applyRunner, &applyRun)
		collect.Dispatchable = dispatchExplanation.Dispatchable
		collect.Blockers = append([]string{}, dispatchExplanation.Blockers...)
	}

	result := externalLoopAdvanceResult{
		Schema:            externalLoopSchema,
		TaskID:            stringField(note.Data, "id"),
		RecordID:          trackerRecordID(note),
		Runner:            run.Runner,
		ApplyRunner:       applyRunner,
		JobID:             jobID,
		AttemptID:         run.ActiveAttemptID,
		Stage:             externalLoopStageCollected,
		NextAction:        collect.NextAction,
		Reason:            externalLoopCollectReason(collect),
		Dispatchable:      collect.Dispatchable,
		Blockers:          append([]string{}, collect.Blockers...),
		Caps:              externalLoopCapsFromWorkflow(wfFile.Data),
		Collect:           &collect,
		AutomationExplain: &dispatchExplanation,
	}
	policyInput := externalLoopPolicyInput{
		TaskID:       result.TaskID,
		RecordID:     result.RecordID,
		Runner:       run.Runner,
		ApplyRunner:  applyRunner,
		JobID:        jobID,
		AttemptID:    run.ActiveAttemptID,
		Stage:        externalLoopStageCollected,
		Action:       result.NextAction,
		Reason:       result.Reason,
		Dispatchable: result.Dispatchable,
		Blockers:     result.Blockers,
		Payload:      externalLoopPayloadForResult(result),
	}
	policyResult, err := externalLoopApplyPolicy(policyCtx, note, policyInput, result.Caps)
	if err != nil {
		return run, false, err
	}
	result.NextAction = policyResult.NextAction
	result.Reason = policyResult.Reason
	result.Blockers = policyResult.Blockers
	result.Counters = policyResult.Counters
	result.ProjectedCounters = policyResult.ProjectedCounters
	result.Event = policyResult.Event
	result.EventCreated = policyResult.EventCreated

	advanced := true
	if result.NextAction == externalLoopActionApplyPatch {
		result.Dispatchable = result.Dispatchable && len(result.Blockers) == 0
		result.DispatchCommand = []string{"tusker", "automation", "dispatch", result.TaskID, "--json"}
	}
	if result.NextAction == externalLoopActionApplyPatch && result.Dispatchable && result.EventCreated {
		updated, dispatchErr := dispatchExternalApplyInput(policyCtx, note, dispatchExplanation, applyRunner)
		if dispatchErr != nil {
			run.LastError = "external loop auto-dispatch failed: " + dispatchErr.Error()
			run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return run, true, nil
		}
		result.DispatchTriggered = true
		result.DispatchRun = updated
		return *updated, true, nil
	}
	if (result.NextAction == externalLoopActionContinueThreadOnFailure || result.NextAction == externalLoopActionRequestReviewNext) && result.EventCreated && len(result.Blockers) == 0 {
		lane := runLaneExecute
		if result.NextAction == externalLoopActionRequestReviewNext {
			lane = runLaneReview
		}
		updated, dispatchErr := dispatchExternalLoopContinuation(policyCtx, note, run, run.Runner, lane)
		if dispatchErr != nil {
			run.LeaseState = string(LeaseStateReleased)
			run.AttemptOutcome = string(AttemptOutcomeBlocked)
			run.NextRetryAt = ""
			run.LastError = "external loop continuation dispatch failed: " + dispatchErr.Error()
			run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return run, true, nil
		}
		return *updated, true, nil
	}
	if result.NextAction == externalLoopActionCloseTask && result.EventCreated && len(result.Blockers) == 0 {
		if err := d.closeExternalLoopTask(project, wfFile, note, collect); err != nil {
			run.LeaseState = string(LeaseStateReleased)
			run.AttemptOutcome = string(AttemptOutcomeBlocked)
			run.NextRetryAt = ""
			run.LastError = "external loop close failed: " + err.Error()
			run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return run, true, nil
		}
		run.LeaseState = string(LeaseStateReleased)
		run.AttemptOutcome = string(AttemptOutcomeSucceeded)
		run.NextRetryAt = ""
		run.LastError = ""
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return run, true, nil
	}
	if result.NextAction == externalLoopActionEscalateHuman || len(result.Blockers) > 0 {
		run.WorkRevision = intField(note.Data, "work_revision")
		run.LastError = "external loop escalated: " + strings.Join(result.Blockers, "; ")
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return run, advanced, nil
}

func (d *Daemon) autoAdvanceExternalApplyResult(ctx context.Context, project RegisteredProject, wfFile WorkflowFile, notes []Note, note Note, run RunStatus) (RunStatus, bool, error) {
	if !daemonShouldAutoAdvanceExternalApplyRun(wfFile.Data, note, run) {
		return run, false, nil
	}
	if latest, err := resolveNote(project.VaultRoot, trackerRecordID(note)); err == nil {
		note = latest
	}
	inputs, err := d.store.ListApplyInputsForRun(project.ProjectID, run.RecordID)
	if err != nil {
		return run, false, err
	}
	if len(inputs) == 0 {
		return run, false, nil
	}
	stage, action := externalLoopApplyResultDecision(wfFile.Data, note, run)
	if stage == "" || action == "" {
		return run, false, nil
	}

	originRunner, jobID := externalLoopOriginForApplyResult(d.store, project.ProjectID, run.RecordID, inputs)
	blockers := externalLoopApplyResultBlockers(wfFile.Data, originRunner, inputs)
	if len(blockers) > 0 {
		action = externalLoopActionEscalateHuman
	}
	reason := externalLoopApplyResultReason(stage, run, note)
	policyCtx, err := d.automationContextForDaemon(project, wfFile, notes, run)
	if err != nil {
		return run, false, err
	}
	result := externalLoopAdvanceResult{
		Schema:       externalLoopSchema,
		TaskID:       stringField(note.Data, "id"),
		RecordID:     trackerRecordID(note),
		Runner:       originRunner,
		ApplyRunner:  run.Runner,
		JobID:        jobID,
		AttemptID:    externalLoopRunAttemptIdentity(run),
		Stage:        stage,
		NextAction:   action,
		Reason:       reason,
		Dispatchable: action == externalLoopActionRequestReviewNext || action == externalLoopActionContinueThreadOnFailure,
		Blockers:     blockers,
		Caps:         externalLoopCapsFromWorkflow(wfFile.Data),
	}
	policyInput := externalLoopPolicyInput{
		TaskID:       result.TaskID,
		RecordID:     result.RecordID,
		Runner:       originRunner,
		ApplyRunner:  run.Runner,
		JobID:        jobID,
		AttemptID:    result.AttemptID,
		Stage:        stage,
		Action:       result.NextAction,
		Reason:       result.Reason,
		Dispatchable: result.Dispatchable,
		Blockers:     result.Blockers,
		Payload:      externalLoopApplyResultPayload(result, run, note, inputs),
	}
	policyResult, err := externalLoopApplyPolicy(policyCtx, note, policyInput, result.Caps)
	if err != nil {
		return run, false, err
	}
	result.NextAction = policyResult.NextAction
	result.Reason = policyResult.Reason
	result.Blockers = policyResult.Blockers
	result.Counters = policyResult.Counters
	result.ProjectedCounters = policyResult.ProjectedCounters
	result.Event = policyResult.Event
	result.EventCreated = policyResult.EventCreated

	if result.NextAction == externalLoopActionContinueThreadOnFailure || result.NextAction == externalLoopActionRequestReviewNext {
		if !result.EventCreated || len(result.Blockers) > 0 {
			if len(result.Blockers) > 0 {
				run.LeaseState = string(LeaseStateReleased)
				run.AttemptOutcome = string(AttemptOutcomeBlocked)
				run.NextRetryAt = ""
				run.LastError = "external loop escalated: " + strings.Join(result.Blockers, "; ")
				run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				return run, true, nil
			}
			return run, false, nil
		}
		lane := runLaneExecute
		if result.NextAction == externalLoopActionRequestReviewNext {
			lane = runLaneReview
		}
		updated, dispatchErr := dispatchExternalLoopContinuation(policyCtx, note, run, originRunner, lane)
		if dispatchErr != nil {
			run.LeaseState = string(LeaseStateReleased)
			run.AttemptOutcome = string(AttemptOutcomeBlocked)
			run.NextRetryAt = ""
			run.LastError = "external loop continuation dispatch failed: " + dispatchErr.Error()
			run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			return run, true, nil
		}
		return *updated, true, nil
	}
	if result.NextAction == externalLoopActionEscalateHuman || len(result.Blockers) > 0 {
		run.LeaseState = string(LeaseStateReleased)
		run.AttemptOutcome = string(AttemptOutcomeBlocked)
		run.NextRetryAt = ""
		run.LastError = "external loop escalated: " + strings.Join(result.Blockers, "; ")
		run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		return run, true, nil
	}
	return run, result.EventCreated, nil
}

func daemonShouldAutoAdvanceExternalApplyRun(wf Workflow, note Note, run RunStatus) bool {
	if daemonNoteKind(note) != "task" || strings.TrimSpace(run.RecordID) == "" {
		return false
	}
	if firstNonEmpty(strings.TrimSpace(run.Lane), runLaneExecute) != runLaneExecute {
		return false
	}
	if strings.TrimSpace(run.Runner) == "" || externalLoopRunnerRequiresCollect(wf, run.Runner) {
		return false
	}
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateReleased, LeaseStateRetryQueued:
	default:
		return false
	}
	switch AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) {
	case AttemptOutcomeSucceeded, AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeCancelled, AttemptOutcomeWaitingForHuman:
		return true
	default:
		return false
	}
}

func externalLoopApplyResultDecision(wf Workflow, note Note, run RunStatus) (string, string) {
	_ = wf
	switch AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) {
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeCancelled:
		return externalLoopStageApplyFailed, externalLoopActionContinueThreadOnFailure
	case AttemptOutcomeWaitingForHuman:
		return externalLoopStageBlocked, externalLoopActionEscalateHuman
	case AttemptOutcomeSucceeded:
		status := strings.TrimSpace(stringField(note.Data, "status"))
		if containsString([]string{"done", "cancelled", "superseded"}, status) || strings.TrimSpace(stringField(note.Data, "closed_at")) != "" {
			return externalLoopStageApplySucceeded, externalLoopActionCloseTask
		}
		if status == "review" {
			return externalLoopStageApplySucceeded, externalLoopActionRequestReviewNext
		}
	}
	return "", ""
}

func externalLoopOriginForApplyResult(store *RuntimeStore, projectID, recordID string, inputs []RuntimeApplyInput) (string, string) {
	runner := ""
	jobID := ""
	if len(inputs) > 0 {
		runner = strings.TrimSpace(inputs[0].Runner)
		jobID = strings.TrimSpace(inputs[0].JobID)
	}
	if store != nil {
		if events, err := store.ListExternalLoopEvents(projectID, recordID); err == nil {
			for i := len(events) - 1; i >= 0; i-- {
				event := events[i]
				if normalizeExternalLoopStage(event.Stage) != externalLoopStageCollected || normalizeExternalLoopAction(event.Action) != externalLoopActionApplyPatch {
					continue
				}
				runner = firstNonEmpty(runner, strings.TrimSpace(event.Runner))
				jobID = firstNonEmpty(jobID, strings.TrimSpace(event.JobID))
				break
			}
		}
	}
	return runner, jobID
}

func externalLoopApplyResultBlockers(wf Workflow, originRunner string, inputs []RuntimeApplyInput) []string {
	var blockers []string
	if len(inputs) != 1 {
		blockers = append(blockers, fmt.Sprintf("external apply result requires exactly one apply input, found %d", len(inputs)))
	}
	if strings.TrimSpace(originRunner) == "" {
		blockers = append(blockers, "external apply result has no originating external runner")
	} else if !externalLoopRunnerRequiresCollect(wf, originRunner) {
		blockers = append(blockers, "originating runner "+originRunner+" is not configured for external collection")
	}
	return blockers
}

func externalLoopApplyResultReason(stage string, run RunStatus, note Note) string {
	switch normalizeExternalLoopStage(stage) {
	case externalLoopStageApplyFailed:
		return firstNonEmpty(strings.TrimSpace(run.LastError), "external apply attempt failed; continue the ChatGPT thread with failure context")
	case externalLoopStageApplySucceeded:
		if strings.TrimSpace(stringField(note.Data, "status")) == "review" {
			return "external apply attempt reached review; request external review next"
		}
		return "external apply attempt completed"
	case externalLoopStageBlocked:
		return firstNonEmpty(strings.TrimSpace(run.LastError), "external apply attempt needs human input")
	default:
		return firstNonEmpty(strings.TrimSpace(run.LastError), "external apply result recorded")
	}
}

func externalLoopApplyResultPayload(result externalLoopAdvanceResult, run RunStatus, note Note, inputs []RuntimeApplyInput) map[string]any {
	payload := externalLoopPayloadForResult(result)
	payload["task_status"] = stringField(note.Data, "status")
	payload["apply_attempt_outcome"] = run.AttemptOutcome
	payload["apply_attempt_runner"] = run.Runner
	payload["apply_attempt_count"] = run.AttemptCount
	payload["apply_lane"] = firstNonEmpty(run.Lane, runLaneExecute)
	payload["last_error"] = run.LastError
	var paths []string
	for _, input := range inputs {
		paths = append(paths, firstNonEmpty(input.RelPath, input.Path))
	}
	payload["apply_inputs"] = paths
	return payload
}

func externalLoopRunAttemptIdentity(run RunStatus) string {
	if id := strings.TrimSpace(run.ActiveAttemptID); id != "" {
		return id
	}
	return fmt.Sprintf("%s:rev-%d:attempt-%d:%s", strings.TrimSpace(run.Runner), run.WorkRevision, run.AttemptCount, strings.TrimSpace(run.AttemptOutcome))
}

func dispatchExternalLoopContinuation(ctx *automationCommandContext, note Note, base RunStatus, externalRunner, lane string) (*RunStatus, error) {
	externalRunner = strings.TrimSpace(externalRunner)
	if externalRunner == "" {
		return nil, tuskerError(errorInvalidTransition, stringField(note.Data, "id")+": external continuation runner is required")
	}
	if !externalLoopRunnerRequiresCollect(ctx.Workflow.Data, externalRunner) {
		return nil, tuskerError(errorInvalidTransition, stringField(note.Data, "id")+": runner "+externalRunner+" is not configured for external collection")
	}
	run := base
	run.ProjectID = firstNonEmpty(run.ProjectID, ctx.Project.ProjectID)
	run.RecordID = firstNonEmpty(run.RecordID, trackerRecordID(note))
	run.ItemID = firstNonEmpty(run.ItemID, stringField(note.Data, "id"))
	run.WorkRevision = intField(note.Data, "work_revision")
	run = prepareRunForLaneDispatch(run, firstNonEmpty(strings.TrimSpace(lane), runLaneExecute), externalRunner)
	if reason := strings.TrimSpace(ctx.DispatchRefusal); reason != "" {
		return nil, tuskerError(errorInvalidTransition, reason, withContext(map[string]any{"task": run.RecordID, "lane": run.Lane}))
	}
	daemon := &Daemon{stateRoot: ctx.StateRoot, store: ctx.Store, dispatchRefusalReason: ctx.DispatchRefusal}
	updated, dispatchErr := daemon.dispatchRun(context.Background(), ctx.Project, ctx.Workflow, note, run, run.Lane)
	if dispatchErr != nil {
		updated = daemon.scheduleRetry(updated, ctx.Workflow.Data, dispatchErr.Error())
	}
	if err := ctx.Store.UpsertRun(updated); err != nil {
		return nil, err
	}
	if dispatchErr != nil {
		return &updated, dispatchErr
	}
	return &updated, nil
}

func (d *Daemon) closeExternalLoopTask(project RegisteredProject, wfFile WorkflowFile, note Note, collect externalCollectReport) error {
	result := collect.ReviewResult
	taskID := stringField(note.Data, "id")
	if result == nil || externalReviewVerdictClass(result.Verdict) != "accepted" {
		return tuskerError(errorInvalidTransition, taskID+": external close requires an accepted review verdict")
	}
	risk := strings.ToLower(strings.TrimSpace(firstNonEmpty(result.Risk, stringField(note.Data, "risk"))))
	if !reviewerMayAutoCloseRisk(wfFile.Data.Reviewer, risk) {
		return tuskerError(errorInvalidTransition, taskID+": external review accepted but risk "+firstNonEmpty(risk, "unknown")+" is not reviewer auto-close eligible")
	}
	actor := reviewerActorForNote(wfFile.Data.Reviewer.Actor, note)
	covers := strings.Join(v7AcceptanceIDs(note.Body), ",")
	if covers == "" {
		covers = "ALL"
	}
	summary := strings.TrimSpace(result.Summary)
	if summary == "" && len(result.Findings) > 0 {
		summary = strings.Join(result.Findings, "; ")
	}
	if summary == "" {
		summary = "External ChatGPT review accepted the applied result."
	}
	if err := verifyV7AddCmd(Args{
		"vault":  project.VaultRoot,
		"quiet":  "true",
		"local":  "true",
		"id":     taskID,
		"by":     actor,
		"covers": covers,
		"check":  "external ChatGPT review verified focused and broad tests; verdict: " + strings.TrimSpace(result.Verdict),
		"result": "pass",
		"note":   summary,
	}); err != nil {
		return err
	}
	return closeV7Cmd(Args{
		"vault":  project.VaultRoot,
		"quiet":  "true",
		"local":  "true",
		"id":     taskID,
		"by":     actor,
		"reason": "external ChatGPT review accepted: " + summary,
	})
}

func daemonShouldAutoAdvanceExternalRun(wf Workflow, note Note, run RunStatus) bool {
	if daemonNoteKind(note) != "task" || strings.TrimSpace(run.RecordID) == "" {
		return false
	}
	if !externalLoopRunnerRequiresCollect(wf, run.Runner) {
		return false
	}
	if LeaseState(strings.TrimSpace(run.LeaseState)) != LeaseStateReleased {
		return false
	}
	if AttemptOutcome(strings.TrimSpace(run.AttemptOutcome)) != AttemptOutcomeSucceeded {
		return false
	}
	return strings.TrimSpace(run.CloudTaskID) != "" || strings.TrimSpace(run.ApplyRef) != ""
}

func (d *Daemon) recordDaemonExternalLoopBlock(project RegisteredProject, wfFile WorkflowFile, notes []Note, note Note, run RunStatus, reason string) (RunStatus, bool, error) {
	policyCtx, err := d.automationContextForDaemon(project, wfFile, notes, run)
	if err != nil {
		return run, false, err
	}
	jobID := firstNonEmpty(strings.TrimSpace(run.CloudTaskID), strings.TrimSpace(run.ApplyRef))
	result := externalLoopAdvanceResult{
		Schema:       externalLoopSchema,
		TaskID:       stringField(note.Data, "id"),
		RecordID:     trackerRecordID(note),
		Runner:       run.Runner,
		JobID:        jobID,
		AttemptID:    run.ActiveAttemptID,
		Stage:        externalLoopStageBlocked,
		NextAction:   externalLoopActionEscalateHuman,
		Reason:       reason,
		Dispatchable: false,
		Blockers:     []string{reason},
		Caps:         externalLoopCapsFromWorkflow(wfFile.Data),
	}
	_, err = externalLoopApplyPolicy(policyCtx, note, externalLoopPolicyInput{
		TaskID:       result.TaskID,
		RecordID:     result.RecordID,
		Runner:       run.Runner,
		JobID:        jobID,
		AttemptID:    run.ActiveAttemptID,
		Stage:        externalLoopStageBlocked,
		Action:       externalLoopActionEscalateHuman,
		Reason:       reason,
		Dispatchable: false,
		Blockers:     result.Blockers,
		Payload:      externalLoopPayloadForResult(result),
	}, result.Caps)
	if err != nil {
		return run, false, err
	}
	run.WorkRevision = intField(note.Data, "work_revision")
	run.LastError = reason
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return run, true, nil
}

func (d *Daemon) automationContextForDaemon(project RegisteredProject, wfFile WorkflowFile, notes []Note, override RunStatus) (*automationCommandContext, error) {
	runs, err := d.store.ListRuns()
	if err != nil {
		return nil, err
	}
	lookup := buildNoteLookup(notes)
	ctx := &automationCommandContext{
		StateRoot:          d.stateRoot,
		Store:              d.store,
		Project:            project,
		ProjectRegistered:  true,
		Workflow:           wfFile,
		Notes:              append([]Note{}, notes...),
		NotesByID:          lookup.ByID,
		NotesByRecordID:    lookup.ByRecordID,
		ProjectRuns:        map[string]RunStatus{},
		NoteStatusByRecord: map[string]string{},
		StateActiveRuns:    map[string]int{},
	}
	for _, note := range notes {
		if daemonNoteKind(note) != "task" {
			continue
		}
		recordID := trackerRecordID(note)
		if recordID == "" {
			continue
		}
		ctx.NotesByRecordID[recordID] = note
		ctx.NoteStatusByRecord[recordID] = stringField(note.Data, "status")
	}
	ctx.Runs = runs
	ctx.GlobalActiveRuns = countDispatchCapacityRuns(runs)
	for _, run := range runs {
		if run.ProjectID != project.ProjectID || strings.TrimSpace(run.RecordID) == "" {
			continue
		}
		ctx.ProjectRuns[run.RecordID] = run
	}
	if strings.TrimSpace(override.RecordID) != "" {
		ctx.ProjectRuns[override.RecordID] = override
	}
	ctx.ProjectActiveRuns = countDispatchCapacityProjectRuns(ctx.ProjectRuns)
	ctx.StateActiveRuns = countDispatchCapacityProjectRunsByState(ctx.ProjectRuns, ctx.NoteStatusByRecord)
	return ctx, nil
}

func (d *Daemon) externalLoopLaunchContext(projectID, recordID string) ExternalLoopLaunchContext {
	if d == nil || d.store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return ExternalLoopLaunchContext{}
	}
	events, err := d.store.ListExternalLoopEvents(projectID, recordID)
	if err != nil {
		return ExternalLoopLaunchContext{}
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if strings.TrimSpace(event.Status) != "ok" {
			continue
		}
		action := normalizeExternalLoopAction(event.Action)
		switch action {
		case externalLoopActionContinueThreadOnFailure, externalLoopActionRequestReviewNext:
			return ExternalLoopLaunchContext{
				Stage:       normalizeExternalLoopStage(event.Stage),
				Action:      action,
				OriginJobID: strings.TrimSpace(event.JobID),
				EventID:     strings.TrimSpace(event.EventID),
				Reason:      strings.TrimSpace(event.Reason),
			}
		}
	}
	return ExternalLoopLaunchContext{}
}

func externalLoopRunnerRequiresCollect(wf Workflow, runner string) bool {
	runner = strings.TrimSpace(runner)
	if runner == "" {
		return false
	}
	definition, ok := wf.Runners[runner]
	if ok {
		if definition.ExternalCollect {
			return true
		}
		if RunnerName(strings.TrimSpace(definition.Kind)) == RunnerCodexCloud {
			return codexCloudConfigFromRunnerDefinition(definition, wf.CodexCloud).ExternalCollect
		}
	}
	return RunnerName(runner) == RunnerCodexCloud && wf.CodexCloud.ExternalCollect
}

func externalLoopCollectCommandForRunner(wf Workflow, runner string) string {
	runner = strings.TrimSpace(runner)
	if definition, ok := wf.Runners[runner]; ok {
		if command := strings.TrimSpace(definition.CollectCommand); command != "" {
			return command
		}
		if RunnerName(strings.TrimSpace(definition.Kind)) == RunnerCodexCloud {
			return strings.TrimSpace(codexCloudConfigFromRunnerDefinition(definition, wf.CodexCloud).CollectCommand)
		}
	}
	if RunnerName(runner) == RunnerCodexCloud {
		return strings.TrimSpace(wf.CodexCloud.CollectCommand)
	}
	return ""
}

func externalLoopDefaultApplyRunner(wf Workflow, externalRunner string) string {
	externalRunner = strings.TrimSpace(externalRunner)
	preferred := []string{strings.TrimSpace(wf.Agents.Default), string(RunnerCodexExec), string(RunnerClaude)}
	for _, candidate := range preferred {
		if externalLoopApplyRunnerCandidate(wf, externalRunner, candidate) {
			return candidate
		}
	}
	for _, candidate := range wf.Agents.Enabled {
		if externalLoopApplyRunnerCandidate(wf, externalRunner, candidate) {
			return strings.TrimSpace(candidate)
		}
	}
	for _, candidate := range []string{string(RunnerCodexExec), string(RunnerCodex), string(RunnerClaude)} {
		if strings.TrimSpace(candidate) != externalRunner {
			return candidate
		}
	}
	return ""
}

func externalLoopApplyRunnerCandidate(wf Workflow, externalRunner, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" || candidate == externalRunner {
		return false
	}
	if len(wf.Agents.Enabled) > 0 && !containsString(wf.Agents.Enabled, candidate) {
		return false
	}
	return !externalLoopRunnerRequiresCollect(wf, candidate)
}

func externalLoopApplyDispatchRun(project RegisteredProject, note Note, base RunStatus, applyRunner string) RunStatus {
	run := base
	run.ProjectID = firstNonEmpty(run.ProjectID, project.ProjectID)
	run.RecordID = firstNonEmpty(run.RecordID, trackerRecordID(note))
	run.ItemID = firstNonEmpty(run.ItemID, stringField(note.Data, "id"))
	run.WorkRevision = intField(note.Data, "work_revision")
	run = prepareRunForLaneDispatch(run, runLaneExecute, applyRunner)
	return run
}

func externalLoopJobAlreadyHandled(store *RuntimeStore, projectID, recordID, jobID string) (bool, error) {
	if store == nil || strings.TrimSpace(jobID) == "" {
		return false, nil
	}
	events, err := store.ListExternalLoopEvents(projectID, recordID)
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if strings.TrimSpace(event.JobID) != strings.TrimSpace(jobID) {
			continue
		}
		switch normalizeExternalLoopStage(event.Stage) {
		case externalLoopStageCollected, externalLoopStageBlocked:
			return true, nil
		}
	}
	return false, nil
}

func externalLoopCloseTaskRecorded(store *RuntimeStore, projectID, recordID string) bool {
	if store == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(recordID) == "" {
		return false
	}
	events, err := store.ListExternalLoopEvents(projectID, recordID)
	if err != nil {
		return false
	}
	for _, event := range events {
		if normalizeExternalLoopAction(event.Action) == externalLoopActionCloseTask && strings.TrimSpace(event.Status) == "ok" {
			return true
		}
	}
	return false
}
