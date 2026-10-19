package main

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func serveRunHistory(s *serveServer, snap serveSnapshot, taskID string) []serveRunSummary {
	out := []serveRunSummary{}
	for _, run := range snap.runs {
		if run.ItemID == taskID || run.RecordID == taskID {
			out = append(out, s.runSummary(snap, run))
		}
	}
	return out
}

func (s *serveServer) runSummary(snap serveSnapshot, run RunStatus) serveRunSummary {
	result, _ := s.runSummaryChecked(snap, run)
	return result
}

func (s *serveServer) runSummaryChecked(snap serveSnapshot, run RunStatus) (serveRunSummary, error) {
	return s.runSummaryCheckedWithScope(snap, run, false)
}

type serveRunFacts struct {
	attempts          []RunAttempt
	attemptsTruncated bool
	identity          *RunIdentityMetadata
	tails             runStateTails
}

func (s *serveServer) runSummaryCheckedWithScope(snap serveSnapshot, run RunStatus, listPage bool, budget ...*int64) (serveRunSummary, error) {
	var byteBudget *int64
	if len(budget) > 0 {
		byteBudget = budget[0]
	}
	summary, _, err := s.runSummaryCheckedWithFacts(snap, run, listPage, byteBudget)
	return summary, err
}

func (s *serveServer) runSummaryCheckedWithFacts(snap serveSnapshot, run RunStatus, listPage bool, byteBudget *int64, questions ...[]AgentMessage) (serveRunSummary, serveRunFacts, error) {
	facts := serveRunFacts{}

	taskID := firstNonEmpty(run.ItemID, run.RecordID)
	taskTitle := taskID
	if task, ok := snap.notesByID[taskID]; ok {
		taskTitle = firstNonEmpty(stringField(task.Data, "title"), taskID)
	}
	turns, _, err := s.store.ListTurnsForRunPage(run.ProjectID, run.RecordID, 200)
	if err != nil {
		return serveRunSummary{}, facts, err
	}
	identity, err := s.store.RunIdentity(run.ProjectID, run.RecordID)
	if err != nil {
		return serveRunSummary{}, facts, err
	}
	workspacePath, workspaceMode := run.WorkspacePath, "shared"
	if identity != nil {
		workspacePath, workspaceMode = identity.WorkspacePath, identity.WorkspaceMode
	}
	attention, err := s.store.WorkerAttentionForRun(run)
	if err != nil {
		return serveRunSummary{}, facts, err
	}
	attempts, attemptsTruncated, err := s.store.ListAttemptsForRunPage(run.ProjectID, run.RecordID, 100)
	if err != nil {
		return serveRunSummary{}, facts, err
	}
	facts.attempts, facts.attemptsTruncated, facts.identity = attempts, attemptsTruncated, identity
	if byteBudget != nil {
		facts.tails = loadRunStateTailsWithBudget(run, attempts, byteBudget, 50)
	} else {
		facts.tails = loadRunStateTails(run, attempts)
	}
	activity := serveRunActivityFromTails(run, attempts, s.now(), facts.tails)
	var messages []AgentMessage
	if len(questions) > 0 {
		messages = questions[0]
	} else {
		messages, err = s.store.ListAgentMessagesForTask(run.ProjectID, taskID)
		if err != nil {
			return serveRunSummary{}, facts, err
		}
	}
	operatorState := runOperatorStateFromFacts(run, attempts, snap.notesByID[taskID], messages, s.now(), defaultRunQuietAfter, true, facts.tails)
	lastSay, err := s.lastRunSayDelivery(run)
	if err != nil {
		return serveRunSummary{}, facts, err
	}
	return serveRunSummary{
		OperatorState:         operatorState,
		SayRoute:              serveSayRoute(run, operatorState),
		LastSayDelivery:       lastSay,
		TaskID:                taskID,
		TaskTitle:             taskTitle,
		ProjectID:             firstNonEmpty(run.ProjectID, snap.projectID),
		Runner:                serveRunner(run.Runner),
		RunnerName:            run.Runner,
		RunnerProfile:         run.RunnerProfile,
		RunnerHarness:         run.RunnerHarness,
		Model:                 run.RunnerModel,
		RunnerEffort:          run.RunnerEffort,
		RunnerFallbackReason:  run.RunnerFallbackReason,
		Lane:                  serveLane(run.Lane),
		LeaseState:            serveLeaseState(run.LeaseState),
		LeaseStateRaw:         run.LeaseState,
		HandRun:               runHandRunOrigin(run, snap.project.VaultRoot),
		ProcessRunning:        operatorState.Evidence.OwnerAlive,
		Outcome:               serveRunOutcome(run, s.now()),
		ElapsedSec:            serveRunElapsedSec(run, s.now()),
		SinceLastEventSec:     serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), s.now()),
		Liveness:              serveRunLiveness(run, s.now()),
		AttemptCount:          maxInt(run.AttemptCount, len(turnsByAttempt(turns))),
		ActiveAttemptID:       run.ActiveAttemptID,
		Terminal:              run.Terminal,
		Error:                 nullIfBlank(run.LastError),
		Infrastructure:        run.Infrastructure,
		LastHeartbeatAt:       nullIfBlank(run.LastHeartbeatAt),
		LastMessageAt:         activity.MessageAt,
		LastToolProgressAt:    activity.ToolAt,
		MessageAgeSec:         activity.MessageAgeSec,
		ToolProgressAgeSec:    activity.ToolAgeSec,
		HeartbeatAgeSec:       activity.HeartbeatAgeSec,
		ActivityCaptureState:  activity.CaptureState,
		ActivityCaptureReason: activity.CaptureReason,
		Activity:              activity,
		NextWakeAt:            nullIfBlank(run.NextRetryAt),
		WorkspacePath:         workspacePath,
		WorkspaceMode:         workspaceMode,
		StartedAt:             run.StartedAt,
		UpdatedAt:             run.UpdatedAt,
		Attention:             attention,
	}, facts, nil
}

func serveRunActivity(run RunStatus, attempts []RunAttempt, now time.Time) serveRunActivityFreshness {
	return serveRunActivityFromTails(run, attempts, now, loadRunStateTails(run, attempts))
}

func serveRunActivityFromTails(run RunStatus, attempts []RunAttempt, now time.Time, tails runStateTails) serveRunActivityFreshness {
	activity := serveRunActivityFreshness{CaptureState: "missing"}
	var latest RunAttempt
	for _, attempt := range attempts {
		if attempt.AttemptID == run.ActiveAttemptID && run.ActiveAttemptID != "" {
			latest = attempt
			break
		}
		if latest.AttemptID == "" || attempt.StartedAt > latest.StartedAt {
			latest = attempt
		}
	}
	paths := []string{bestRunEventPath(run, []RunAttempt{latest}), bestRunLogPath(run, []RunAttempt{latest})}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			activity.CaptureState = "available"
			break
		} else if err != nil && !os.IsNotExist(err) {
			activity.CaptureState, activity.CaptureReason = "unavailable", "recorded activity cannot be read"
		}
	}
	for _, event := range serveRunEventsFromTails(tails) {
		if _, ok := parseRunTimestamp(event.TS); !ok {
			continue
		}
		switch event.Kind {
		case "heartbeat":
			if event.TS > toString(activity.HeartbeatAt) {
				activity.HeartbeatAt = event.TS
			}
		case "agent_message", "message":
			if event.TS > toString(activity.MessageAt) {
				activity.MessageAt = event.TS
			}
		case "tool_call", "tool_result", "tool_progress":
			if event.TS > toString(activity.ToolAt) {
				activity.ToolAt = event.TS
			}
		}
	}
	if activity.HeartbeatAt != nil {
		activity.HeartbeatAgeSec = serveSinceSec(toString(activity.HeartbeatAt), now)
	}
	if activity.MessageAt != nil {
		activity.MessageAgeSec = serveSinceSec(toString(activity.MessageAt), now)
	}
	if activity.ToolAt != nil {
		activity.ToolAgeSec = serveSinceSec(toString(activity.ToolAt), now)
	}
	return activity
}

func serveFindRun(runs []RunStatus, id string) (RunStatus, bool) {
	for _, run := range runs {
		if run.RecordID == id {
			return run, true
		}
	}
	for _, run := range runs {
		if run.ItemID == id {
			return run, true
		}
	}
	return RunStatus{}, false
}

func serveRunner(runner string) string {
	switch {
	case strings.Contains(runner, "claude"):
		return "claude"
	default:
		return "codex"
	}
}

func serveLane(lane string) string {
	if strings.Contains(strings.ToLower(lane), "review") {
		return "review"
	}
	return "execute"
}

func serveLeaseState(state string) string {
	if strings.TrimSpace(state) == runnerInfrastructureBlockedState {
		return runnerInfrastructureBlockedState
	}
	switch LeaseState(strings.TrimSpace(state)) {
	case LeaseStateUnclaimed, LeaseStateRetryQueued:
		return "unclaimed"
	case LeaseStateReleased, LeaseStateInterrupted, LeaseStateParkedBudget:
		return "released"
	case LeaseStateParkedNoProgress:
		return "parked"
	default:
		return "held"
	}
}

type serveInterruptResult struct {
	OK             bool   `json:"ok"`
	Refused        bool   `json:"refused,omitempty"`
	Interrupted    bool   `json:"interrupted"`
	Reason         string `json:"reason"`
	TaskID         string `json:"taskId"`
	LeaseState     string `json:"leaseState,omitempty"`
	LeaseStateRaw  string `json:"leaseStateRaw,omitempty"`
	ProcessRunning bool   `json:"processRunning"`
}

// handleRunInterrupt shares the exact runtime transition used by
// `tusker runs stop --soft`, then returns canonical store readback so the UI does
// not enable Redrive based only on a successful HTTP response.
func (s *serveServer) handleRunInterrupt(w http.ResponseWriter, r *http.Request, taskID string) {
	taskID = strings.ToUpper(strings.TrimSpace(taskID))
	// Interrupt is a mutating, cross-project-dangerous action: without an explicit
	// project a same-ID run in any other project could be stopped instead.
	projectID := strings.TrimSpace(r.URL.Query().Get("project"))
	if projectID == "" {
		serveJSON(w, http.StatusBadRequest, serveInterruptResult{Refused: true, Reason: "project query parameter is required to interrupt a run", TaskID: taskID})
		return
	}
	if snap, err := s.loadFreshSnapshotForProject(projectID); err != nil {
		serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
		return
	} else {
		run, ok := serveFindRun(snap.runs, taskID)
		if !ok {
			serveJSON(w, http.StatusNotFound, serveInterruptResult{Refused: true, Reason: "run not found in project", TaskID: taskID})
			return
		}
		capability := s.runActionCapability("interrupt", snap.project, Note{}, run, nil)
		if !capability.Available {
			serveJSON(w, http.StatusConflict, serveInterruptResult{Refused: true, Reason: capability.Reason, TaskID: taskID})
			return
		}
	}
	run, _, err := interruptRuntimeRunScoped(DefaultStateRoot(), s.store, projectID, taskID)
	if err != nil {
		issue := errorToIssue(err)
		reason := issue.Message
		if issue.Hint != "" {
			reason += " Hint: " + issue.Hint
		}
		serveJSON(w, http.StatusOK, serveInterruptResult{Refused: true, Reason: reason, TaskID: taskID})
		return
	}
	// Refresh runtime facts after the store transition; notes/docs are unchanged.
	s.invalidateProjectRuntimeSnapshot(projectID)
	processRunning := runProcessGroupAlive(*run)
	confirmed := LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateInterrupted && !processRunning
	result := serveInterruptResult{
		OK:             confirmed,
		Refused:        !confirmed,
		Interrupted:    confirmed,
		TaskID:         firstNonEmpty(run.ItemID, taskID),
		LeaseState:     serveLeaseState(run.LeaseState),
		LeaseStateRaw:  run.LeaseState,
		ProcessRunning: processRunning,
	}
	if confirmed {
		result.Reason = "run interrupted; canonical lease is interrupted and no process is running"
	} else {
		result.Reason = "interrupt returned before canonical lease/process state confirmed the stop"
	}
	s.refreshProjectRuntimeSnapshot(firstNonEmpty(run.ProjectID, projectID))
	serveJSON(w, http.StatusOK, result)
}

func serveRunOutcome(run RunStatus, now time.Time) string {
	if strings.TrimSpace(run.LeaseState) == runnerInfrastructureBlockedState || (run.Infrastructure != nil && run.Infrastructure.State == runnerInfrastructureBlockedState) {
		return runnerInfrastructureBlockedState
	}
	projectedOutcome := projectedAttemptOutcome(run.AttemptOutcome, run.LastError)
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateUnclaimed:
		if run.AttemptCount > 0 || projectedOutcome != "" && projectedOutcome != AttemptOutcomeNone {
			return serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
		}
		return "idle"
	case LeaseStateParkedNoProgress:
		return "parked-no-progress"
	case LeaseStateParkedBudget:
		return "parked-budget"
	case LeaseStateRetryQueued:
		return "retry-queued"
	case LeaseStateClaimed, LeaseStateRunning:
		if serveRunHeartbeatFresh(run, now) {
			return "running"
		}
		return "stale"
	case LeaseStateReleased:
		if run.Terminal {
			switch projectedOutcome {
			case AttemptOutcomeSucceeded, AttemptOutcomeFailed, AttemptOutcomeUnknown, AttemptOutcomeBlocked, AttemptOutcomeEarlyExit, AttemptOutcomeDispatchDeclined, AttemptOutcomeTurnCapExhausted, AttemptOutcomeBudgetExceeded, AttemptOutcomeCancelled:
				outcome := serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
				return outcome
			}
			return "terminal"
		}
	}
	return serveRunOutcomeFromAttempt(string(projectedOutcome), run.LeaseState)
}

func serveRunOutcomeFromAttempt(outcome, lease string) string {
	if strings.TrimSpace(lease) == runnerInfrastructureBlockedState {
		return runnerInfrastructureBlockedState
	}
	switch AttemptOutcome(strings.TrimSpace(outcome)) {
	case AttemptOutcomeSucceeded:
		return "succeeded"
	case AttemptOutcomeUnknown:
		return "outcome-unknown"
	case AttemptOutcomeFailed, AttemptOutcomeBlocked, AttemptOutcomeAbandoned, AttemptOutcomeEarlyExit, AttemptOutcomeTurnCapExhausted, AttemptOutcomeBudgetExceeded:
		return "failed"
	case AttemptOutcomeDispatchDeclined:
		return "dispatch-declined"
	case AttemptOutcomeCancelled:
		return "interrupted"
	case AttemptOutcomeWaitingForReview:
		return "review-complete"
	case AttemptOutcomeWaitingForHuman:
		return "awaiting-human"
	default:
		if LeaseState(lease) == LeaseStateUnclaimed {
			return "idle"
		}
		if LeaseState(lease) == LeaseStateParkedNoProgress {
			return "parked-no-progress"
		}
		if LeaseState(lease) == LeaseStateParkedBudget {
			return "parked-budget"
		}
		if LeaseState(lease) == LeaseStateRetryQueued {
			return "retry-queued"
		}
		return "released"
	}
}

// serveAttemptOutcome keeps the active attempt aligned with the canonical run
// liveness. An attempt row is intentionally durable before it has a terminal
// outcome, so mapping an active "none" outcome through the lease fallback
// would incorrectly paint a live reviewer as released.
func (s *serveServer) serveAttemptOutcome(run RunStatus, attempt RunAttempt) string {
	if attempt.AttemptID == run.ActiveAttemptID {
		current := serveRunOutcome(run, s.now())
		if current == "running" || current == "stale" {
			return current
		}
	}
	return serveRunOutcomeFromAttempt(string(projectedAttemptOutcome(attempt.Outcome, attempt.LastError)), run.LeaseState)
}

func serveRunHeartbeatFresh(run RunStatus, now time.Time) bool {
	heartbeatAt, ok := parseRunTimestamp(run.LastHeartbeatAt)
	return ok && now.Sub(heartbeatAt) <= daemonHeartbeatDeadThreshold
}

func serveRunHiddenByDefault(run RunStatus) bool {
	return LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateUnclaimed && run.AttemptCount == 0
}

func serveRunLiveness(run RunStatus, now time.Time) string {
	if runProcessGroupAlive(run) {
		return "fresh"
	}
	age := serveSinceSec(firstNonEmpty(run.LastEventAt, run.UpdatedAt), now)
	switch {
	case age < 60:
		return "fresh"
	case age < 120:
		return "stale"
	default:
		return "stale"
	}
}

func serveWorstLiveness(current any, next string) any {
	rank := map[string]int{"fresh": 0, "stale": 1, "dead": 2}
	if current == nil {
		return next
	}
	if rank[next] > rank[fmt.Sprint(current)] {
		return next
	}
	return current
}

func serveSinceSec(value string, now time.Time) int {
	if value == "" {
		return 0
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return maxInt(0, int(now.Sub(ts).Seconds()))
	}
	return 0
}

func serveRunElapsedSec(run RunStatus, now time.Time) int {
	switch LeaseState(strings.TrimSpace(run.LeaseState)) {
	case LeaseStateClaimed, LeaseStateRunning:
		return serveDurationSec(run.StartedAt, "", now)
	default:
		return serveDurationSec(run.StartedAt, firstNonEmpty(run.UpdatedAt, run.LastEventAt, run.LastHeartbeatAt), now)
	}
}

func serveDurationSec(start, end string, now time.Time) int {
	if start == "" {
		return 0
	}
	startAt, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0
	}
	endAt := now
	if end != "" {
		if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			endAt = parsed
		}
	}
	return maxInt(0, int(endAt.Sub(startAt).Seconds()))
}

func turnsByAttempt(turns []RunTurn) map[string]struct{} {
	out := map[string]struct{}{}
	for _, turn := range turns {
		if turn.AttemptID != "" {
			out[turn.AttemptID] = struct{}{}
		}
	}
	return out
}

func serveRunEventsAll(run RunStatus, attempts []RunAttempt) []serveRunEvent {
	return serveRunEventsFromTails(loadRunStateTails(run, attempts))
}

// runEventsCache holds display events per unchanged tail state. Building them
// parses and redacts every record, and one task page asks several times per run.
var runEventsCache = struct {
	sync.Mutex
	entries map[string][]serveRunEvent
}{entries: map[string][]serveRunEvent{}}

const runEventsCacheLimit = 512

func serveRunEventsFromTails(tails runStateTails) []serveRunEvent {
	if tails.key == "" {
		return buildRunEventsFromTails(tails)
	}
	runEventsCache.Lock()
	cached, ok := runEventsCache.entries[tails.key]
	runEventsCache.Unlock()
	if !ok {
		cached = buildRunEventsFromTails(tails)
		runEventsCache.Lock()
		if len(runEventsCache.entries) >= runEventsCacheLimit {
			// ponytail: whole-cache reset; a growing log leaves one stale key per
			// change, so evict per run if resets show up in profiles.
			clear(runEventsCache.entries)
		}
		runEventsCache.entries[tails.key] = cached
		runEventsCache.Unlock()
	}
	return slices.Clone(cached)
}

func buildRunEventsFromTails(tails runStateTails) []serveRunEvent {
	out := []serveRunEvent{}
	for _, payload := range tails.events {
		out = appendRunActivity(out, serveRunEventFromPayload(payload))
	}
	eventCount := len(out)
	for _, payload := range tails.log {
		for _, event := range cliRunActivity(payload) {
			out = appendRunActivity(out, event)
		}
	}
	// Empty timestamps inherit their source's previous timestamp only while sorting.
	type orderedEvent struct {
		event serveRunEvent
		key   string
		index int
	}
	ordered := make([]orderedEvent, 0, len(out))
	last := [2]string{}
	for i, event := range out {
		source := 0
		if i >= eventCount {
			source = 1
		}
		if event.TS != "" {
			last[source] = event.TS
		}
		ordered = append(ordered, orderedEvent{event, last[source], i})
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].key != ordered[j].key {
			return ordered[i].key < ordered[j].key
		}
		return ordered[i].index < ordered[j].index
	})
	for i := range ordered {
		out[i] = ordered[i].event
	}
	return out
}

func serveRunEvents(run RunStatus, attempts []RunAttempt) []serveRunEvent {
	return serveRunDisplayEvents(serveRunEventsAll(run, attempts))
}

func serveRunDisplayEvents(out []serveRunEvent) []serveRunEvent {
	// Keep diagnostics from displacing the recent messages with heartbeat noise.
	activityCount, diagnosticCount := 0, 0
	keep := make([]serveRunEvent, 0, 100)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Activity {
			activityCount++
			if activityCount > 50 {
				continue
			}
		} else {
			diagnosticCount++
			if diagnosticCount > 50 {
				continue
			}
		}
		keep = append(keep, out[i])
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	return keep
}

// serveRunEventFromPayload flattens one JSONL event line into the shape the run
// event tail reads. The runtime EventLog (see event_log.go) writes the
// timestamp under the top-level "at" key (RFC3339) and nests the human-readable
// fields under a "payload" object. The UI parser only knew "ts"/"timestamp",
// so every row rendered NaN:NaN:NaN — read "at" first (the field the API
// actually emits) and dig into the nested payload for text/level.
func serveRunEventFromPayload(payload map[string]any) serveRunEvent {
	nested, _ := payload["payload"].(map[string]any)
	field := func(keys ...string) string {
		for _, k := range keys {
			if v := toString(payload[k]); v != "" {
				return v
			}
			if nested != nil {
				if v := toString(nested[k]); v != "" {
					return v
				}
			}
		}
		return ""
	}
	return serveRunEvent{
		TS:       field("at", "ts", "timestamp", "time"),
		ID:       field("message_id"),
		Kind:     firstNonEmpty(field("kind", "event"), "event"),
		Text:     runActivityText(field("text", "message", "summary", "detail")),
		Level:    field("level", "severity"),
		Activity: nested != nil && nested["activity"] == true,
	}
}

// serveRedriveResult is the response body for POST /api/runs/:taskId/redrive.
// It always carries a human-readable reason so the UI can surface both a
// successful requeue and a refusal — a redrive must never be silent.
type serveRedriveResult struct {
	OK              bool   `json:"ok"`
	Refused         bool   `json:"refused"`
	Requeued        bool   `json:"requeued"`
	Reason          string `json:"reason"`
	Issue           *Issue `json:"issue,omitempty"`
	TaskID          string `json:"taskId"`
	CanonicalStatus string `json:"canonicalStatus"`
	LeaseState      string `json:"leaseState"`
}

type serveRecoveryResult struct {
	OK       bool   `json:"ok"`
	Refused  bool   `json:"refused"`
	Admitted bool   `json:"admitted"`
	Action   string `json:"action"`
	TaskID   string `json:"taskId"`
	Lane     string `json:"lane,omitempty"`
	Reason   string `json:"reason"`
}

const outcomeUnknownRecoveryReasonPrefix = "recover outcome unknown from "
const outcomeUnknownContextRecoveryReasonPrefix = "recover context from "

func outcomeUnknownRecoveryParent(lastError string) string {
	for _, prefix := range []string{outcomeUnknownRecoveryReasonPrefix, outcomeUnknownContextRecoveryReasonPrefix} {
		marker := strings.Index(lastError, prefix)
		if marker >= 0 {
			return strings.TrimSpace(lastError[marker+len(prefix):])
		}
	}
	return ""
}

// The directive is the durable queued intent; the redrive record covers older
// recoveries whose directive lacks a reason. Both survive capacity waits.
func pendingOutcomeUnknownRecoveryParent(store *RuntimeStore, run RunStatus) (string, error) {
	parent := outcomeUnknownRecoveryParent(run.LastError)
	if store == nil {
		return parent, nil
	}
	directive, err := store.RunDirective(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	record, err := store.BudgetWindowStart(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	if directive != nil && (directive.State == "queued" || directive.State == "consumed") {
		if durable := outcomeUnknownRecoveryParent(directive.Reason); durable != "" {
			parent = durable
		}
	}
	// A later ordinary redrive supersedes an unclaimed recovery directive.
	if directive != nil && record.ResetAt != "" && outcomeUnknownRecoveryParent(record.Reason) == "" {
		redriveAt, redriveErr := time.Parse(time.RFC3339Nano, record.ResetAt)
		directiveAt, directiveErr := time.Parse(time.RFC3339Nano, directive.CreatedAt)
		if redriveErr == nil && directiveErr == nil && redriveAt.After(directiveAt) {
			return "", nil
		}
	}
	if parent == "" {
		parent = outcomeUnknownRecoveryParent(record.Reason)
	}
	if parent == "" {
		return "", nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return "", err
	}
	for _, attempt := range attempts {
		if attempt.ParentAttemptID == parent && attempt.ChildType == "recovery" {
			return "", nil
		}
	}
	return parent, nil
}

func queueOutcomeUnknownRecovery(store *RuntimeStore, task Note, wave Note, run RunStatus, actor string, now time.Time) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "recover_unknown", TaskID: stringField(task.Data, "id"), Lane: runLaneExecute}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	if current == nil {
		result.Refused, result.Reason = true, "run not found"
		return result, nil
	}
	run = *current
	if !run.Terminal || projectedAttemptOutcome(run.AttemptOutcome, run.LastError) != AttemptOutcomeUnknown {
		result.Refused, result.Reason = true, "recovery is only available when the prior outcome is unknown"
		return result, nil
	}
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		result.Refused, result.Reason = true, "a live owner still holds this task; wait for it to finish or become stale"
		return result, nil
	}
	attempts, err := store.ListAttemptsForRun(run.ProjectID, run.RecordID)
	if err != nil {
		return result, err
	}
	var parent RunAttempt
	for _, attempt := range attempts {
		if projectedAttemptOutcome(attempt.Outcome, attempt.LastError) == AttemptOutcomeUnknown {
			parent = attempt
			break
		}
	}
	if parent.AttemptID == "" {
		result.Refused, result.Reason = true, "the uncertain parent attempt cannot be identified"
		return result, nil
	}
	for _, attempt := range attempts {
		if attempt.ParentAttemptID == parent.AttemptID && attempt.ChildType == "recovery" {
			result.Refused, result.Reason = true, "one recovery attempt already exists; human review is required"
			return result, nil
		}
	}
	// Older detached ACP attempts retained the bound session in their event
	// ledger but did not copy it into the run row. Preserve that exact identity
	// before redrive so the normal native-resume checks can use it.
	if run.Runner == string(RunnerDevin) && parent.SessionRef == "" {
		if ref := acpSessionRefFromAttemptEvents(parent.EventSinkPath, parent.AttemptID, parent.Runner); ref != "" {
			parent.SessionRef = ref
			if err := store.SaveAttempt(parent); err != nil {
				return result, err
			}
			if err := store.SaveSession(RunnerSession{
				ProjectID: run.ProjectID, RecordID: run.RecordID, Runner: run.Runner, SessionRef: ref,
				WorkspacePath: parent.WorkspacePath, CurrentItemID: run.ItemID, WorkRevision: parent.WorkRevision,
				LastAttemptID: parent.AttemptID, State: sessionStateForLeaseState(LeaseStateReleased),
				Resumable: true, StartedAt: parent.StartedAt, LastSeenAt: now.UTC().Format(time.RFC3339),
			}); err != nil {
				return result, err
			}
		}
	}
	previousRun := run
	previousBudget, err := store.GetSetting(budgetRedriveSettingKey(run.ProjectID, run.RecordID))
	if err != nil {
		return result, err
	}
	if _, err := redriveRuntimeRun(store, &run, actor, outcomeUnknownRecoveryReasonPrefix+parent.AttemptID, now); err != nil {
		return result, err
	}
	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		Reason:    outcomeUnknownRecoveryReasonPrefix + parent.AttemptID,
		CreatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.UTC().Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: stringField(wave.Data, "authorization_fingerprint"), WaveAuthorizedAt: stringField(wave.Data, "authorized_at"),
	})
	if err != nil {
		// Redrive and directive insertion are separate store operations. If the
		// directive write fails, restore the exact pre-recovery snapshot only if
		// no daemon/operator write won the race in the meantime.
		if matches, matchErr := store.RunMatchesSnapshot(run); matchErr == nil && matches {
			if rollbackErr := store.UpsertRun(previousRun); rollbackErr != nil {
				return result, fmt.Errorf("recovery directive failed: %w; rollback failed: %v", err, rollbackErr)
			}
			if previousBudget == "" {
				_, _ = store.exec(`DELETE FROM daemon_settings WHERE key = ?`, budgetRedriveSettingKey(run.ProjectID, run.RecordID))
			} else {
				_ = store.SetSetting(budgetRedriveSettingKey(run.ProjectID, run.RecordID), previousBudget)
			}
		}
		return result, err
	}
	if !queued {
		result.OK, result.Reason = true, "recovery is already queued"
		return result, nil
	}
	result.OK, result.Admitted, result.Reason = true, true, "verification queued; the worker will inspect and preserve existing work before continuing"
	return result, nil
}

// queueReviewRecovery reuses the one-shot directive consumed by the normal
// daemon. It never resets AttemptCount: automatic recovery remains inside the
// originally authorized retry window.
func queueReviewRecovery(store *RuntimeStore, task Note, wave Note, run RunStatus, actor string, maxAttempts int, now time.Time, allowLaneChange ...bool) (serveRecoveryResult, error) {
	result := serveRecoveryResult{Action: "retry_review", TaskID: stringField(task.Data, "id"), Lane: runLaneReview}
	if current, err := store.FindRunScoped(run.ProjectID, run.RecordID); err != nil {
		return result, err
	} else if current != nil {
		run = *current
	}
	canChangeLane := len(allowLaneChange) > 0 && allowLaneChange[0]
	if !strings.EqualFold(stringField(task.Data, "status"), "review") || (run.Lane != runLaneReview && !canChangeLane) {
		result.Refused, result.Reason = true, "retry review is only available for a failed independent-review lane"
		return result, nil
	}
	if reason := reviewRecoveryOperationalBlocker(wave, run, maxAttempts); reason != "" {
		result.Refused, result.Reason = true, reason
		return result, nil
	}
	fingerprint := stringField(wave.Data, "authorization_fingerprint")
	authorizedAt := stringField(wave.Data, "authorized_at")
	prepared := prepareRunForLaneDispatch(run, runLaneReview, run.Runner)
	prepared.UpdatedAt = now.UTC().Format(time.RFC3339Nano)
	if err := store.UpsertRun(prepared); err != nil {
		return result, err
	}
	queued, err := store.QueueRunDirective(RunDirective{
		ProjectID: run.ProjectID, RecordID: run.RecordID, Actor: actor,
		CreatedAt: now.UTC().Format(time.RFC3339Nano), ExpiresAt: now.UTC().Add(directRunDirectiveTTL).Format(time.RFC3339Nano),
		WaveID: stringField(wave.Data, "id"), AuthorizationFingerprint: fingerprint, WaveAuthorizedAt: authorizedAt,
	})
	if err != nil {
		return result, err
	}
	if !queued {
		result.OK, result.Reason = true, "review recovery is already queued or owned"
		return result, nil
	}
	result.OK, result.Admitted, result.Reason = true, true, "review retry queued on the existing review lane; implementation will not rerun"
	return result, nil
}

func reviewRecoveryOperationalBlocker(wave Note, run RunStatus, maxAttempts int) string {
	if runProcessGroupAlive(run) || isDispatchingLeaseState(run.LeaseState) {
		return "an owner is already active for this review"
	}
	if maxAttempts > 0 && run.AttemptCount >= maxAttempts {
		return fmt.Sprintf("review attempt limit exhausted (%d); authorize one new bounded execution window", maxAttempts)
	}
	if strings.EqualFold(stringField(wave.Data, "authorization"), "paused") {
		return "wave is paused; recovery will not secretly resume it"
	}
	if !strings.EqualFold(stringField(wave.Data, "authorization"), "armed") || stringField(wave.Data, "authorization_fingerprint") == "" || stringField(wave.Data, "authorized_at") == "" {
		return "the existing wave execution window is not current"
	}
	// The queued review directive is bound to the stored authorization
	// fingerprint; if wave material drifted since then it can never match
	// runDirectiveMatchesTaskAuthority and would sit until TTL expiry. Refuse
	// up front with the real recovery action instead of admitting silently.
	if vault := v7VaultPathForLandingAudit(wave); vault != "" {
		idx, err := loadV7Index(vault)
		if err != nil {
			return "cannot verify wave material before retrying review: " + err.Error()
		}
		if auth := waveAuthorizationProjection(vault, idx, wave); boolFromAny(auth["stale"]) {
			return "wave material changed since authorization; re-authorize with `tusker start` before retrying review"
		}
	}
	return ""
}

// serveRedriveRefusal decides whether a redrive is meaningless for the task's
// canonical (frontmatter) status. Redrive resets the attempt window and
// requeues so the daemon spawns a fresh attempt; when the task is already in
// review or done there is no execution to redrive — the daemon would refuse and
// (the observed bug) silently retire the run behind a stale badge. We surface
// that refusal synchronously instead of requeuing into a silent retire.
func serveRedriveRefusal(rawStatus string, run RunStatus) (bool, string) {
	if projectedAttemptOutcome(run.AttemptOutcome, run.LastError) == AttemptOutcomeUnknown {
		return true, "this work needs recovery — use Verify and continue so the next worker inspects and preserves existing work"
	}
	switch strings.ToLower(strings.TrimSpace(rawStatus)) {
	case "review":
		return true, "task is in review — no execution to redrive; use the review/land lane, not retry"
	case "done", "closed":
		return true, "task is done — no execution to redrive; nothing to retry"
	}
	if LeaseState(strings.TrimSpace(run.LeaseState)) == LeaseStateRetryQueued {
		return true, "redrive is already queued; wait for the daemon to claim it or interrupt the queued run first"
	}
	if runProcessGroupAlive(run) {
		return true, "run is still executing; use tusker runs stop " + firstNonEmpty(run.ItemID, run.RecordID) + " --soft before redrive"
	}
	return false, ""
}

// serveRedriveRun mirrors the state transition of `tusker runs restart --requeue` (redriveCmd)
// using the shared runtime store: it resets the budget/attempt window and
// requeues the run so the daemon spawns a fresh codex-exec attempt. The
// attempt-window rules themselves are owned by RUN-T-0028; keep this in sync
// with redriveCmd.
func serveRedriveRun(store *RuntimeStore, run *RunStatus, actor, reason string, now time.Time) error {
	previousAttemptID := run.ActiveAttemptID
	previousSessionRef := run.SessionRef
	previousWorkspacePath := run.WorkspacePath
	if _, err := redriveRuntimeRun(store, run, actor, reason, now); err != nil {
		return err
	}
	_, err := store.SaveSupervisorDecision(SupervisorDecision{
		ProjectID:        run.ProjectID,
		RecordID:         run.RecordID,
		ItemID:           run.ItemID,
		Runner:           run.Runner,
		WorkRevision:     run.WorkRevision,
		AttemptID:        previousAttemptID,
		ParentAttemptID:  previousAttemptID,
		SessionRef:       previousSessionRef,
		ParentSessionRef: previousSessionRef,
		Kind:             string(SupervisorDecisionRedrive),
		Reason:           reason,
		WorkspacePath:    previousWorkspacePath,
		LeaseState:       run.LeaseState,
		ContextSignal:    "operator_redrive_serve",
		CreatedAt:        now.Format(time.RFC3339),
	})
	return err
}

// Operator view of the process inventory (Execution Operations → Processes).

// serveProcessStoppedWindow keeps a stopped process listed with its reason.
const serveProcessStoppedWindow = 24 * time.Hour

type serveProcess struct {
	ID        int64    `json:"id"`
	Kind      string   `json:"kind"` // agent_session | check
	What      string   `json:"what"`
	Owner     string   `json:"owner"`
	ProjectID string   `json:"projectId,omitempty"`
	TaskIDs   []string `json:"taskIds,omitempty"`
	AttemptID string   `json:"attemptId,omitempty"`
	// State is running, long_running, silent, stopping or stopped.
	State           string `json:"state"`
	Reason          string `json:"reason,omitempty"`
	Rule            string `json:"rule,omitempty"`
	PID             int    `json:"pid"`
	PGID            int    `json:"pgid"`
	StartedAt       string `json:"startedAt"`
	LastSignOfLife  string `json:"lastSignOfLife"`
	EndedAt         string `json:"endedAt,omitempty"`
	RuntimeSec      int    `json:"runtimeSec"`
	QuietSec        int    `json:"quietSec"`
	SilenceLimitSec int    `json:"silenceLimitSec"`
	CanStop         bool   `json:"canStop"`
}

type serveProcessList struct {
	Processes   []serveProcess `json:"processes"`
	GeneratedAt string         `json:"generatedAt"`
}

// Normal ends (passed, failed, exited) are not stops and drop off the list.
var serveProcessStopReasons = map[string]string{
	"operator_stopped":     "stopped by the operator",
	"starter_stopped":      "the process that started it is gone",
	"stuck_stopped":        "no output or CPU progress for the check silence limit",
	"owner_gone":           "no live run owns it any more",
	"cancelled":            "cancelled by its starter",
	"identity_unconfirmed": "PID, process group and start time no longer matched; reported, not stopped",
}

// recentOwnedProcesses returns open rows and rows ended since the cutoff.
func (s *RuntimeStore) recentOwnedProcesses(since time.Time) ([]ownedProcess, error) {
	rows, err := s.query(`SELECT id, owner, kind, pid, pgid, process_start, starter, started_at, last_sign_of_life, ended_at, end_reason
		FROM process_inventory WHERE ended_at = '' OR ended_at >= ? ORDER BY id DESC`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ownedProcess
	for rows.Next() {
		var p ownedProcess
		if err := rows.Scan(&p.ID, &p.Owner, &p.Kind, &p.PID, &p.PGID, &p.ProcessStart, &p.Starter, &p.StartedAt, &p.LastSignOfLife, &p.EndedAt, &p.EndReason); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// lastProcessStopEvents maps a process to its latest stop or report event.
func (s *RuntimeStore) lastProcessStopEvents(since time.Time) (map[int64]processEvent, error) {
	rows, err := s.query(`SELECT process_id, owner, pid, pgid, kind, reason, rule, at FROM process_events
		WHERE kind IN ('stopped', 'reported') AND at >= ? ORDER BY id`, since.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]processEvent{}
	for rows.Next() {
		var e processEvent
		if err := rows.Scan(&e.ProcessID, &e.Owner, &e.PID, &e.PGID, &e.Kind, &e.Reason, &e.Rule, &e.At); err != nil {
			return nil, err
		}
		out[e.ProcessID] = e
	}
	return out, rows.Err()
}

// liveRunForAttempt returns the claimed or running run that owns an attempt.
func (s *RuntimeStore) liveRunForAttempt(attemptID string) (RunStatus, bool) {
	runs, err := s.listRunsQuery(`SELECT `+runtimeRunColumns+` FROM runs WHERE active_attempt_id = ? AND lease_state IN (?, ?) LIMIT 1`,
		attemptID, string(LeaseStateClaimed), string(LeaseStateRunning))
	if err != nil || len(runs) == 0 {
		return RunStatus{}, false
	}
	return runs[0], true
}

// serveProcessOwnerTasks reads task IDs out of a check owner:
// "closeout:<task>" or "landing_gate:task-batch:<task>@<branch>@<sha>,...".
func serveProcessOwnerTasks(owner string) (string, []string) {
	if task, ok := strings.CutPrefix(owner, "closeout:"); ok {
		return "Closeout check", []string{task}
	}
	if batch, ok := strings.CutPrefix(owner, "landing_gate:task-batch:"); ok {
		var tasks []string
		for _, part := range strings.Split(batch, ",") {
			if id, _, _ := strings.Cut(part, "@"); id != "" {
				tasks = append(tasks, id)
			}
		}
		return "Landing gate", tasks
	}
	if strings.HasPrefix(owner, "landing_gate:") {
		return "Landing gate", nil
	}
	return "Check", nil
}

func (s *serveServer) handleProcesses(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	snap, err := s.loadSnapshotForRequest(r)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	since := now.Add(-serveProcessStoppedWindow)
	rows, err := s.store.recentOwnedProcesses(since)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	events, err := s.store.lastProcessStopEvents(since)
	if err != nil {
		serveJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	var live janitorSnapshot
	if raw, err := processListing(); err == nil {
		live = janitorSnapshot(raw)
	}
	limits := map[string][2]time.Duration{}
	limitsFor := func(projectID string) (time.Duration, time.Duration) {
		if l, ok := limits[projectID]; ok {
			return l[0], l[1]
		}
		vault := snap.project.VaultRoot
		if projectID != "" && projectID != snap.projectID {
			vault = registeredProjectVault(s.store, projectID)
		}
		check, agent := processSilenceLimits(vault)
		limits[projectID] = [2]time.Duration{check, agent}
		return check, agent
	}
	title := func(projectID, taskID string) string {
		if projectID == "" || projectID == snap.projectID {
			if note, ok := snap.notesByID[taskID]; ok {
				if t := strings.TrimSpace(stringField(note.Data, "title")); t != "" {
					return t
				}
			}
		}
		return taskID
	}
	out := serveProcessList{Processes: []serveProcess{}, GeneratedAt: now.Format(time.RFC3339Nano)}
	for _, p := range rows {
		item := serveProcess{ID: p.ID, Kind: p.Kind, Owner: p.Owner, PID: p.PID, PGID: p.PGID,
			StartedAt: p.StartedAt, LastSignOfLife: p.LastSignOfLife, EndedAt: p.EndedAt}
		lastSign := latestTime(p.StartedAt, p.LastSignOfLife)
		var limit time.Duration
		if p.Kind == processKindAgentSession {
			item.AttemptID = p.Owner
			item.What = "Agent session"
			if run, ok := s.store.liveRunForAttempt(p.Owner); ok {
				item.ProjectID, item.TaskIDs = run.ProjectID, []string{run.ItemID}
				item.What = title(run.ProjectID, run.ItemID)
				lastSign = latestTime(p.StartedAt, p.LastSignOfLife, run.LastEventAt)
			}
			_, limit = limitsFor(item.ProjectID)
		} else {
			label, tasks := serveProcessOwnerTasks(p.Owner)
			item.TaskIDs, item.What = tasks, label
			if len(tasks) > 0 {
				item.What = label + " · " + title("", tasks[0])
				if len(tasks) > 1 {
					item.What += fmt.Sprintf(" +%d", len(tasks)-1)
				}
			}
			limit, _ = limitsFor("")
		}
		if !lastSign.IsZero() {
			item.LastSignOfLife = lastSign.Format(time.RFC3339Nano)
		}
		item.SilenceLimitSec = int(limit / time.Second)
		end := now
		if p.EndedAt != "" {
			end = latestTime(p.EndedAt)
		}
		item.RuntimeSec = max(0, int(end.Sub(latestTime(p.StartedAt))/time.Second))
		if !lastSign.IsZero() {
			item.QuietSec = max(0, int(end.Sub(lastSign)/time.Second))
		}
		event, hasEvent := events[p.ID]
		switch {
		case p.EndedAt != "":
			reason, stopped := serveProcessStopReasons[p.EndReason]
			if !stopped {
				continue
			}
			item.State, item.Reason = "stopped", reason
			if hasEvent {
				item.Reason, item.Rule = event.Reason, event.Rule
			}
		case live == nil || !live.identity(p):
			item.State, item.Reason = "stopping", "the process is gone or no longer matches its recorded identity; the janitor closes this row on its next pass"
		case time.Duration(item.QuietSec)*time.Second > limit:
			item.State, item.CanStop = "silent", true
			item.Reason = "no sign of life for longer than the silence limit"
			if p.Kind == processKindAgentSession {
				item.Reason += "; flagged, left running"
			}
		case time.Duration(item.RuntimeSec)*time.Second > limit:
			item.State, item.CanStop = "long_running", true
		default:
			item.State, item.CanStop = "running", true
		}
		out.Processes = append(out.Processes, item)
	}
	serveJSON(w, http.StatusOK, out)
}

// handleProcessStop stops one inventoried process group for the operator. It
// refuses unless PID, process group and start time all still match, records
// the operator's intent as a process event, and for an agent session persists
// the run's stop intent so automatic retry does not restart it.
func (s *serveServer) handleProcessStop(w http.ResponseWriter, body serveActionBody) {
	const command = "serve processes stop"
	actor, err := s.serveOperatorActor(body, command)
	if err != nil {
		status, result := serveOperatorActorResult(command, err)
		serveJSON(w, status, result)
		return
	}
	refuse := func(code, reason string) {
		serveJSON(w, http.StatusOK, serveActionResult{Refused: true, Reason: reason, Issue: &Issue{Code: code, Message: reason}})
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(toString(body["id"])), 10, 64)
	p, found, err := s.store.ownedProcessByID(id)
	if err != nil {
		refuse("PROCESS_INVENTORY_UNAVAILABLE", "process inventory is unreadable: "+err.Error())
		return
	}
	if !found {
		refuse("PROCESS_NOT_FOUND", fmt.Sprintf("process %d is not in the inventory", id))
		return
	}
	if p.EndedAt != "" {
		refuse("PROCESS_ALREADY_ENDED", "process already ended: "+firstNonEmpty(serveProcessStopReasons[p.EndReason], p.EndReason))
		return
	}
	if !processIdentityMatches(RunStatus{ProcessPID: p.PID, ProcessPGID: p.PGID, ProcessStartedAt: p.ProcessStart}) {
		refuse("PROCESS_IDENTITY_UNCONFIRMED", fmt.Sprintf("refused: PID %d, process group %d and start time no longer all match the inventory, so nothing was stopped", p.PID, p.PGID))
		return
	}
	// End the row first so the operator reason wins over the exit the kill
	// causes (endOwnedProcess keeps the first reason).
	if err := s.store.endOwnedProcess(p.ID, "operator_stopped"); err != nil {
		refuse("PROCESS_INVENTORY_UNAVAILABLE", "could not record the stop: "+err.Error())
		return
	}
	_ = s.store.recordProcessEvent(p, "stopped", "stopped by "+actor, ruleOperator)
	result := serveActionResult{OK: true, Reason: fmt.Sprintf("stopped process group %d", p.PGID)}
	if p.Kind == processKindAgentSession {
		if run, ok := s.store.liveRunForAttempt(p.Owner); ok {
			prior, _ := loadRunSessionControlIntent(s.store, run.ProjectID, run.RecordID)
			control, _ := s.applyRunStopControl(run, actor, "operator stopped its process", prior, runSessionControlResult{TaskID: run.ItemID, ProjectID: run.ProjectID, Action: runSessionControlStop})
			result.TaskID, result.ProjectID = run.ItemID, run.ProjectID
			if control.Refused {
				result.Reason += "; run stop: " + control.Reason
			}
			defer s.refreshProjectRuntimeSnapshot(run.ProjectID)
		}
	}
	stopProcessGroup(p.PGID)
	serveJSON(w, http.StatusOK, result)
}

func (s *RuntimeStore) ownedProcessByID(id int64) (ownedProcess, bool, error) {
	var p ownedProcess
	err := s.queryRowScan(`SELECT id, owner, kind, pid, pgid, process_start, starter, started_at, last_sign_of_life, ended_at, end_reason
		FROM process_inventory WHERE id = ?`, []any{id}, &p.ID, &p.Owner, &p.Kind, &p.PID, &p.PGID, &p.ProcessStart, &p.Starter, &p.StartedAt, &p.LastSignOfLife, &p.EndedAt, &p.EndReason)
	if errors.Is(err, sql.ErrNoRows) {
		return p, false, nil
	}
	return p, err == nil, err
}
