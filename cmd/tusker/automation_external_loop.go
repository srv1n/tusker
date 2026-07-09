package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	externalLoopSchema = "tusker.external_loop/v1"

	externalLoopStageCollected       = "collected"
	externalLoopStageApplySucceeded  = "apply_succeeded"
	externalLoopStageApplyFailed     = "apply_failed"
	externalLoopStageReviewSucceeded = "review_succeeded"
	externalLoopStageReviewFailed    = "review_failed"
	externalLoopStageNotesOnly       = "notes_only"
	externalLoopStageBlocked         = "blocked"

	externalLoopActionRecordResearch          = "record_research_artifact"
	externalLoopActionApplyPatch              = "apply_patch"
	externalLoopActionRequestReviewNext       = "request_review_next"
	externalLoopActionContinueThreadOnFailure = "continue_thread_with_failure"
	externalLoopActionCloseTask               = "close_task"
	externalLoopActionEscalateHuman           = "escalate_human"

	externalLoopDefaultMaxCycles              = 3
	externalLoopDefaultMaxRepairContinuations = 2
	externalLoopDefaultMaxExternalThreads     = 5
	externalLoopDefaultWallClockTimeoutHours  = 8
)

type externalLoopStatusReport struct {
	Schema   string               `json:"schema"`
	TaskID   string               `json:"task_id"`
	RecordID string               `json:"record_id"`
	Counters ExternalLoopCounters `json:"counters"`
	Caps     ExternalLoopCaps     `json:"caps"`
	Events   []ExternalLoopEvent  `json:"events"`
}

type externalLoopAdvanceResult struct {
	Schema            string                     `json:"schema"`
	TaskID            string                     `json:"task_id"`
	RecordID          string                     `json:"record_id"`
	Runner            string                     `json:"runner"`
	ApplyRunner       string                     `json:"apply_runner,omitempty"`
	JobID             string                     `json:"job_id,omitempty"`
	AttemptID         string                     `json:"attempt_id,omitempty"`
	Stage             string                     `json:"stage"`
	NextAction        string                     `json:"next_action"`
	Reason            string                     `json:"reason"`
	Dispatchable      bool                       `json:"dispatchable"`
	DispatchCommand   []string                   `json:"dispatch_command,omitempty"`
	DispatchTriggered bool                       `json:"dispatch_triggered,omitempty"`
	DispatchRun       *RunStatus                 `json:"dispatch_run,omitempty"`
	Blockers          []string                   `json:"blockers,omitempty"`
	Counters          ExternalLoopCounters       `json:"counters"`
	ProjectedCounters ExternalLoopCounters       `json:"projected_counters"`
	Caps              ExternalLoopCaps           `json:"caps"`
	Event             *ExternalLoopEvent         `json:"event,omitempty"`
	EventCreated      bool                       `json:"event_created"`
	Collect           *externalCollectReport     `json:"collect,omitempty"`
	AutomationExplain *automationTaskExplanation `json:"automation_explain,omitempty"`
}

type externalLoopPolicyInput struct {
	TaskID       string
	RecordID     string
	Runner       string
	ApplyRunner  string
	JobID        string
	AttemptID    string
	Stage        string
	Action       string
	Reason       string
	Dispatchable bool
	Blockers     []string
	Payload      map[string]any
}

func automationExternalLoopCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	if args.Bool("dispatch") {
		ctx.DispatchRefusal = oneShotDispatchRefusal("tusker automation advance-external --dispatch")
	}
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	report, err := externalLoopStatus(ctx, note, externalLoopCapsFromArgs(ctx.Workflow.Data, args))
	if err != nil {
		return err
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": true, "external_loop": report})
		return nil
	}
	printExternalLoopStatus(report)
	return nil
}

func automationAdvanceExternalCmd(args Args) error {
	taskID, err := requireArg(args, "id")
	if err != nil {
		return err
	}
	ctx, err := loadAutomationCommandContext(args)
	if err != nil {
		return err
	}
	defer ctx.Close()
	note, err := ctx.findTask(taskID)
	if err != nil {
		return err
	}
	runner := firstNonEmpty(strings.TrimSpace(args.String("runner")), "chatgpt-browser")
	run := ctx.effectiveRunForTask(note, runner)
	jobID := firstNonEmpty(strings.TrimSpace(args.String("job")), strings.TrimSpace(args.String("cloud-task-id")), strings.TrimSpace(run.CloudTaskID))
	stage := normalizeExternalLoopStage(args.String("event"))
	if stage == "" && jobID != "" {
		stage = externalLoopStageCollected
	}
	if stage == "" {
		return tuskerError(errorMissingArg, "automation advance-external requires --event or --job", withHint("use --job <chatgpt-job> to collect, or --event apply_failed|apply_succeeded|review_succeeded"))
	}
	explanation := ctx.explainTask(note)
	applyRunner := firstNonEmpty(strings.TrimSpace(args.String("apply-runner")), explanation.Runner)
	if applyRunner == runner && strings.TrimSpace(args.String("apply-runner")) == "" {
		applyRunner = externalLoopDefaultApplyRunner(ctx.Workflow.Data, runner)
	}
	dispatchExplanation := explanation
	result := externalLoopAdvanceResult{
		Schema:            externalLoopSchema,
		TaskID:            stringField(note.Data, "id"),
		RecordID:          trackerRecordID(note),
		Runner:            runner,
		ApplyRunner:       applyRunner,
		JobID:             jobID,
		AttemptID:         strings.TrimSpace(args.String("attempt-id")),
		Stage:             stage,
		Caps:              externalLoopCapsFromArgs(ctx.Workflow.Data, args),
		AutomationExplain: &explanation,
	}
	var collect *externalCollectReport
	if stage == externalLoopStageCollected {
		if jobID == "" {
			return tuskerError(errorMissingArg, "automation advance-external collected event requires --job or runtime cloud_task_id")
		}
		args["apply-runner"] = applyRunner
		collected, err := ctx.collectExternal(note, run, runner, jobID, args)
		if err != nil {
			return err
		}
		collect = &collected
		result.Collect = collect
		result.NextAction = collected.NextAction
		result.Dispatchable = collected.Dispatchable
		result.Blockers = append(result.Blockers, collected.Blockers...)
		result.Reason = externalLoopCollectReason(collected)
		if collected.NextAction == externalLoopActionApplyPatch {
			applyRun := externalLoopApplyDispatchRun(ctx.Project, note, ctx.ProjectRuns[trackerRecordID(note)], applyRunner)
			dispatchExplanation = ctx.explainTaskForRunner(note, applyRunner, &applyRun)
			result.AutomationExplain = &dispatchExplanation
		}
	} else {
		result.NextAction = externalLoopActionForStage(stage, args)
		result.Dispatchable = explanation.Dispatchable
		result.Reason = externalLoopReasonForStage(stage, args)
		if !explanation.Dispatchable && result.NextAction == externalLoopActionApplyPatch {
			result.Blockers = append(result.Blockers, explanation.Blockers...)
		}
	}
	if override := strings.TrimSpace(args.String("next-action")); override != "" {
		action := normalizeExternalLoopAction(override)
		if action == "" {
			return tuskerError(errorInvalidArg, "unknown external loop next action: "+override)
		}
		result.NextAction = action
	}
	policyInput := externalLoopPolicyInput{
		TaskID:       result.TaskID,
		RecordID:     result.RecordID,
		Runner:       runner,
		ApplyRunner:  applyRunner,
		JobID:        jobID,
		AttemptID:    result.AttemptID,
		Stage:        stage,
		Action:       result.NextAction,
		Reason:       result.Reason,
		Dispatchable: result.Dispatchable,
		Blockers:     result.Blockers,
		Payload:      externalLoopPayloadForResult(result),
	}
	policyResult, err := externalLoopApplyPolicy(ctx, note, policyInput, result.Caps)
	if err != nil {
		return err
	}
	result.NextAction = policyResult.NextAction
	result.Blockers = policyResult.Blockers
	result.Counters = policyResult.Counters
	result.ProjectedCounters = policyResult.ProjectedCounters
	result.Event = policyResult.Event
	result.EventCreated = policyResult.EventCreated
	result.Reason = policyResult.Reason
	if result.NextAction == externalLoopActionApplyPatch {
		result.Dispatchable = result.Dispatchable && len(result.Blockers) == 0
		result.DispatchCommand = []string{"tusker", "automation", "dispatch", result.TaskID, "--json"}
	}
	if args.Bool("dispatch") && result.NextAction == externalLoopActionApplyPatch {
		dispatched, dispatchErr := dispatchExternalApplyInput(ctx, note, dispatchExplanation, applyRunner)
		if dispatchErr != nil {
			result.Blockers = append(result.Blockers, dispatchErr.Error())
			result.NextAction = externalLoopActionEscalateHuman
		} else {
			result.DispatchTriggered = true
			result.DispatchRun = dispatched
		}
	}
	if args.Bool("json") {
		emitJSON(map[string]any{"ok": result.NextAction != externalLoopActionEscalateHuman, "external_loop": result})
		return nil
	}
	printExternalLoopAdvance(result)
	if result.NextAction == externalLoopActionEscalateHuman {
		return tuskerError(errorInvalidTransition, taskID+": external loop escalated: "+strings.Join(result.Blockers, "; "), withContext(result))
	}
	return nil
}

type externalLoopPolicyResult struct {
	NextAction        string
	Reason            string
	Blockers          []string
	Counters          ExternalLoopCounters
	ProjectedCounters ExternalLoopCounters
	Event             *ExternalLoopEvent
	EventCreated      bool
}

func externalLoopApplyPolicy(ctx *automationCommandContext, note Note, input externalLoopPolicyInput, caps ExternalLoopCaps) (externalLoopPolicyResult, error) {
	events, err := ctx.Store.ListExternalLoopEvents(ctx.Project.ProjectID, input.RecordID)
	if err != nil {
		return externalLoopPolicyResult{}, err
	}
	counters := externalLoopCountersForEvents(events)
	action := normalizeExternalLoopAction(input.Action)
	if action == "" {
		action = externalLoopActionEscalateHuman
	}
	reason := firstNonEmpty(strings.TrimSpace(input.Reason), "external loop policy recorded "+input.Stage)
	blockers := append([]string{}, input.Blockers...)
	event := externalLoopEventForInput(ctx, note, input, action, reason, blockers)
	existing, err := ctx.Store.FindExternalLoopEventByKey(event.ProjectID, event.RecordID, event.IdempotencyKey)
	if err != nil {
		return externalLoopPolicyResult{}, err
	}
	projected := counters
	if existing == nil {
		projected = externalLoopCountersWithEvent(counters, event)
	}
	if action != externalLoopActionEscalateHuman {
		capBlockers := externalLoopCapBlockers(caps, counters, projected, action)
		if len(capBlockers) > 0 {
			blockers = append(blockers, capBlockers...)
			action = externalLoopActionEscalateHuman
			reason = "external loop cap reached"
			event = externalLoopEventForInput(ctx, note, input, action, reason, blockers)
			if existing, err = ctx.Store.FindExternalLoopEventByKey(event.ProjectID, event.RecordID, event.IdempotencyKey); err != nil {
				return externalLoopPolicyResult{}, err
			}
			if existing == nil {
				projected = externalLoopCountersWithEvent(counters, event)
			} else {
				projected = counters
			}
		}
	}
	saved, created, err := ctx.Store.SaveExternalLoopEvent(event)
	if err != nil {
		return externalLoopPolicyResult{}, err
	}
	return externalLoopPolicyResult{
		NextAction:        action,
		Reason:            reason,
		Blockers:          uniqueStrings(blockers),
		Counters:          counters,
		ProjectedCounters: projected,
		Event:             &saved,
		EventCreated:      created,
	}, nil
}

func externalLoopStatus(ctx *automationCommandContext, note Note, caps ExternalLoopCaps) (externalLoopStatusReport, error) {
	recordID := trackerRecordID(note)
	events, err := ctx.Store.ListExternalLoopEvents(ctx.Project.ProjectID, recordID)
	if err != nil {
		return externalLoopStatusReport{}, err
	}
	return externalLoopStatusReport{
		Schema:   externalLoopSchema,
		TaskID:   stringField(note.Data, "id"),
		RecordID: recordID,
		Counters: externalLoopCountersForEvents(events),
		Caps:     caps,
		Events:   events,
	}, nil
}

func externalLoopCapsFromArgs(wf Workflow, args Args) ExternalLoopCaps {
	caps := externalLoopCapsFromWorkflow(wf)
	if value := intArg(args, "max-cycles"); value > 0 {
		caps.MaxCycles = value
	}
	if value := intArg(args, "max-repair-continuations"); value > 0 {
		caps.MaxRepairContinuations = value
	}
	if value := intArg(args, "max-external-threads"); value > 0 {
		caps.MaxExternalThreads = value
	}
	if value := intArg(args, "wall-clock-timeout-hours"); value > 0 {
		caps.WallClockTimeoutHours = value
	}
	return normalizeExternalLoopCaps(caps)
}

func externalLoopCapsFromWorkflow(wf Workflow) ExternalLoopCaps {
	return normalizeExternalLoopCaps(wf.ExternalLoop)
}

func normalizeExternalLoopCaps(caps ExternalLoopCaps) ExternalLoopCaps {
	if caps.MaxCycles <= 0 {
		caps.MaxCycles = externalLoopDefaultMaxCycles
	}
	if caps.MaxRepairContinuations <= 0 {
		caps.MaxRepairContinuations = externalLoopDefaultMaxRepairContinuations
	}
	if caps.MaxExternalThreads <= 0 {
		caps.MaxExternalThreads = externalLoopDefaultMaxExternalThreads
	}
	if caps.WallClockTimeoutHours <= 0 {
		caps.WallClockTimeoutHours = externalLoopDefaultWallClockTimeoutHours
	}
	return caps
}

func externalLoopCountersForEvents(events []ExternalLoopEvent) ExternalLoopCounters {
	var counters ExternalLoopCounters
	jobSeen := map[string]bool{}
	for _, event := range events {
		counters.Events++
		if externalLoopActionCountsCycle(event.Action) {
			counters.Cycles++
		}
		if normalizeExternalLoopAction(event.Action) == externalLoopActionContinueThreadOnFailure {
			counters.RepairContinuations++
		}
		jobID := strings.TrimSpace(event.JobID)
		if jobID != "" && !jobSeen[jobID] {
			jobSeen[jobID] = true
			counters.DistinctJobIDs = append(counters.DistinctJobIDs, jobID)
		}
	}
	sort.Strings(counters.DistinctJobIDs)
	counters.ExternalThreads = len(counters.DistinctJobIDs)
	return counters
}

func externalLoopCountersWithEvent(counters ExternalLoopCounters, event ExternalLoopEvent) ExternalLoopCounters {
	out := counters
	out.Events++
	if externalLoopActionCountsCycle(event.Action) {
		out.Cycles++
	}
	if normalizeExternalLoopAction(event.Action) == externalLoopActionContinueThreadOnFailure {
		out.RepairContinuations++
	}
	jobID := strings.TrimSpace(event.JobID)
	if jobID != "" && !containsString(out.DistinctJobIDs, jobID) {
		out.DistinctJobIDs = append(out.DistinctJobIDs, jobID)
		sort.Strings(out.DistinctJobIDs)
	}
	out.ExternalThreads = len(out.DistinctJobIDs)
	return out
}

func externalLoopCapBlockers(caps ExternalLoopCaps, current, projected ExternalLoopCounters, action string) []string {
	var blockers []string
	if externalLoopActionCountsCycle(action) && projected.Cycles > caps.MaxCycles {
		blockers = append(blockers, fmt.Sprintf("external loop cycle cap reached: %d/%d", current.Cycles, caps.MaxCycles))
	}
	if normalizeExternalLoopAction(action) == externalLoopActionContinueThreadOnFailure && projected.RepairContinuations > caps.MaxRepairContinuations {
		blockers = append(blockers, fmt.Sprintf("patch repair continuation cap reached: %d/%d", current.RepairContinuations, caps.MaxRepairContinuations))
	}
	if externalLoopActionMayOpenExternalThread(action) && current.ExternalThreads >= caps.MaxExternalThreads {
		blockers = append(blockers, fmt.Sprintf("external thread cap reached: %d/%d", current.ExternalThreads, caps.MaxExternalThreads))
	} else if projected.ExternalThreads > caps.MaxExternalThreads {
		blockers = append(blockers, fmt.Sprintf("external thread cap reached: %d/%d", current.ExternalThreads, caps.MaxExternalThreads))
	}
	return blockers
}

func externalLoopActionCountsCycle(action string) bool {
	switch normalizeExternalLoopAction(action) {
	case externalLoopActionApplyPatch, externalLoopActionRequestReviewNext, externalLoopActionContinueThreadOnFailure:
		return true
	default:
		return false
	}
}

func externalLoopActionMayOpenExternalThread(action string) bool {
	switch normalizeExternalLoopAction(action) {
	case externalLoopActionRequestReviewNext, externalLoopActionContinueThreadOnFailure:
		return true
	default:
		return false
	}
}

func externalLoopEventForInput(ctx *automationCommandContext, note Note, input externalLoopPolicyInput, action, reason string, blockers []string) ExternalLoopEvent {
	status := "ok"
	if action == externalLoopActionEscalateHuman || len(blockers) > 0 {
		status = "blocked"
	}
	payload := map[string]any{}
	for key, value := range input.Payload {
		payload[key] = value
	}
	payload["blockers"] = uniqueStrings(blockers)
	payload["apply_runner"] = input.ApplyRunner
	raw, _ := json.Marshal(payload)
	event := ExternalLoopEvent{
		ProjectID:   ctx.Project.ProjectID,
		RecordID:    input.RecordID,
		ItemID:      stringField(note.Data, "id"),
		Runner:      input.Runner,
		JobID:       input.JobID,
		AttemptID:   input.AttemptID,
		Stage:       input.Stage,
		Action:      action,
		Status:      status,
		Reason:      reason,
		PayloadJSON: string(raw),
	}
	event.IdempotencyKey = externalLoopIdempotencyKey(event)
	return event
}

func externalLoopIdempotencyKey(event ExternalLoopEvent) string {
	source := strings.Join([]string{
		strings.TrimSpace(event.Stage),
		strings.TrimSpace(event.Action),
		strings.TrimSpace(event.JobID),
		strings.TrimSpace(event.AttemptID),
		strings.TrimSpace(event.Reason),
	}, "|")
	sum := sha256.Sum256([]byte(source))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func externalLoopPayloadForResult(result externalLoopAdvanceResult) map[string]any {
	payload := map[string]any{
		"task_id":      result.TaskID,
		"stage":        result.Stage,
		"next_action":  result.NextAction,
		"dispatchable": result.Dispatchable,
	}
	if result.JobID != "" {
		payload["job_id"] = result.JobID
	}
	if result.Collect != nil {
		payload["artifact_dir"] = result.Collect.ArtifactDir
		payload["patches"] = result.Collect.Patches
		payload["review_packets"] = result.Collect.ReviewPackets
		payload["evidence_added"] = result.Collect.EvidenceAdded
		payload["evidence_existing"] = result.Collect.EvidenceExisting
	}
	return payload
}

func externalLoopCollectReason(collect externalCollectReport) string {
	if len(collect.Blockers) > 0 {
		return "collected external artifacts with blockers"
	}
	switch collect.NextAction {
	case externalLoopActionApplyPatch:
		return "collected one external apply input"
	case externalLoopActionCloseTask:
		if collect.ReviewResult != nil {
			return "external review accepted the applied result"
		}
		return "external loop close requested"
	case externalLoopActionContinueThreadOnFailure:
		if collect.ReviewResult != nil {
			return "external review requested changes; continue the thread with rework context"
		}
		return "external loop requested continuation"
	case externalLoopActionRecordResearch:
		return "collected external review notes without an apply input"
	default:
		return "collected external artifacts"
	}
}

func externalLoopReasonForStage(stage string, args Args) string {
	if reason := strings.TrimSpace(args.String("reason")); reason != "" {
		return reason
	}
	switch normalizeExternalLoopStage(stage) {
	case externalLoopStageApplyFailed:
		return "external apply attempt failed; continue the ChatGPT thread with failure context"
	case externalLoopStageApplySucceeded:
		return "external apply attempt passed machine verification; request an external review"
	case externalLoopStageReviewSucceeded:
		return "external review accepted the applied result"
	case externalLoopStageReviewFailed:
		return "external review found rework; continue the thread with review failure context"
	case externalLoopStageNotesOnly:
		return "external result produced notes only"
	case externalLoopStageBlocked:
		return "external loop blocked"
	default:
		return "external loop advanced"
	}
}

func externalLoopActionForStage(stage string, args Args) string {
	if action := normalizeExternalLoopAction(args.String("next-action")); action != "" {
		return action
	}
	switch normalizeExternalLoopStage(stage) {
	case externalLoopStageApplyFailed, externalLoopStageReviewFailed:
		return externalLoopActionContinueThreadOnFailure
	case externalLoopStageApplySucceeded:
		if args.Bool("acceptance-satisfied") || args.Bool("close") {
			return externalLoopActionCloseTask
		}
		return externalLoopActionRequestReviewNext
	case externalLoopStageReviewSucceeded:
		if args.Bool("request-review-next") {
			return externalLoopActionRequestReviewNext
		}
		return externalLoopActionCloseTask
	case externalLoopStageNotesOnly:
		return externalLoopActionRecordResearch
	case externalLoopStageBlocked:
		return externalLoopActionEscalateHuman
	default:
		return externalLoopActionEscalateHuman
	}
}

func normalizeExternalLoopStage(stage string) string {
	stage = strings.ToLower(strings.TrimSpace(stage))
	stage = strings.ReplaceAll(stage, "-", "_")
	switch stage {
	case "collect", "collected", "external_collected":
		return externalLoopStageCollected
	case "apply_succeeded", "apply_success", "patch_applied", "applied", "verified":
		return externalLoopStageApplySucceeded
	case "apply_failed", "patch_failed", "failed_apply", "conflict":
		return externalLoopStageApplyFailed
	case "review_succeeded", "review_success", "review_accepted", "accepted":
		return externalLoopStageReviewSucceeded
	case "review_failed", "review_rejected", "review_rework", "rework":
		return externalLoopStageReviewFailed
	case "notes", "notes_only", "research":
		return externalLoopStageNotesOnly
	case "blocked", "escalate", "escalated":
		return externalLoopStageBlocked
	default:
		return stage
	}
}

func normalizeExternalLoopAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	action = strings.ReplaceAll(action, "-", "_")
	switch action {
	case "record_research", "record_research_artifact", "notes_only":
		return externalLoopActionRecordResearch
	case "apply", "apply_patch", "patch":
		return externalLoopActionApplyPatch
	case "request_review", "review_next", "request_review_next":
		return externalLoopActionRequestReviewNext
	case "continue", "continue_failure", "continue_thread", "continue_thread_with_failure":
		return externalLoopActionContinueThreadOnFailure
	case "close", "close_task", "done":
		return externalLoopActionCloseTask
	case "escalate", "escalate_human", "human":
		return externalLoopActionEscalateHuman
	default:
		return action
	}
}

func dispatchExternalApplyInput(ctx *automationCommandContext, note Note, explanation automationTaskExplanation, applyRunner string) (*RunStatus, error) {
	if !explanation.Dispatchable {
		return nil, tuskerError(errorInvalidTransition, stringField(note.Data, "id")+": automation dispatch blocked: "+strings.Join(explanation.Blockers, "; "), withContext(explanation))
	}
	if strings.TrimSpace(applyRunner) == "" {
		applyRunner = explanation.Runner
	}
	recordID := trackerRecordID(note)
	run := ctx.effectiveRunForTask(note, applyRunner)
	if current, ok := ctx.ProjectRuns[recordID]; ok && externalLoopRunnerRequiresCollect(ctx.Workflow.Data, current.Runner) && LeaseState(strings.TrimSpace(current.LeaseState)) == LeaseStateReleased {
		run = externalLoopApplyDispatchRun(ctx.Project, note, current, applyRunner)
	}
	if reason := strings.TrimSpace(ctx.DispatchRefusal); reason != "" {
		return nil, tuskerError(errorInvalidTransition, reason, withContext(explanation))
	}
	// Ensure the run row exists and reflects the current dispatch intent
	// (runner/lane/work_revision) WITHOUT overwriting the live lease/process
	// columns, so dispatchRun's ClaimRunLease CAS still validates against the
	// true stored lease generation and a concurrent operator stop is not clobbered.
	if err := ctx.Store.UpsertRunPreservingLease(run); err != nil {
		return nil, err
	}
	daemon := &Daemon{stateRoot: ctx.StateRoot, store: ctx.Store, dispatchRefusalReason: ctx.DispatchRefusal}
	updated, persisted, dispatchErr := daemon.dispatchRun(context.Background(), ctx.Project, ctx.Workflow, note, run, runLaneExecute)
	if !persisted {
		if dispatchErr != nil {
			updated = daemon.scheduleRetry(updated, ctx.Workflow.Data, dispatchErr.Error())
		}
		if err := ctx.Store.UpsertRun(updated); err != nil {
			return nil, err
		}
	}
	if dispatchErr != nil {
		return &updated, dispatchErr
	}
	return &updated, nil
}

func intArg(args Args, key string) int {
	value := strings.TrimSpace(args.String(key))
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func printExternalLoopStatus(report externalLoopStatusReport) {
	fmt.Printf("%s external loop: cycles=%d/%d repairs=%d/%d threads=%d/%d events=%d\n", report.TaskID, report.Counters.Cycles, report.Caps.MaxCycles, report.Counters.RepairContinuations, report.Caps.MaxRepairContinuations, report.Counters.ExternalThreads, report.Caps.MaxExternalThreads, report.Counters.Events)
	if len(report.Events) == 0 {
		fmt.Println("(no external loop events)")
		return
	}
	for _, event := range report.Events {
		fmt.Printf("- %s %s action=%s status=%s job=%s\n", event.CreatedAt, event.Stage, event.Action, event.Status, event.JobID)
	}
}

func printExternalLoopAdvance(result externalLoopAdvanceResult) {
	fmt.Printf("%s external loop next_action=%s", result.TaskID, result.NextAction)
	if result.Reason != "" {
		fmt.Printf(" reason=%q", result.Reason)
	}
	fmt.Println()
	if len(result.Blockers) > 0 {
		fmt.Println("Blockers:")
		for _, blocker := range result.Blockers {
			fmt.Printf("- %s\n", blocker)
		}
	}
	if len(result.DispatchCommand) > 0 {
		fmt.Printf("Dispatch: %s\n", strings.Join(result.DispatchCommand, " "))
	}
	fmt.Printf("caps cycles=%d/%d repairs=%d/%d threads=%d/%d\n", result.ProjectedCounters.Cycles, result.Caps.MaxCycles, result.ProjectedCounters.RepairContinuations, result.Caps.MaxRepairContinuations, result.ProjectedCounters.ExternalThreads, result.Caps.MaxExternalThreads)
}
