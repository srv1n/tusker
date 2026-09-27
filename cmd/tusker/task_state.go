package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// taskState is the one state a task shows (docs/system/proposals/task-states.md).
// The UI, CLI and API display it; none of them map internal values on their own.
type taskState struct {
	State      string `json:"state"`
	Label      string `json:"label"`
	Category   string `json:"category"`
	NextActor  string `json:"next_actor"`
	ReasonCode string `json:"reason_code"`
	Reason     string `json:"reason"`
	NextAction string `json:"next_action"`
}

// taskStateOrder is the wave urgency order (spec rule 5), most urgent first.
var taskStateOrder = []string{"blocked", "needs_input", "working", "in_review", "planned", "backlog", "done", "canceled"}

var taskStateMeta = map[string]struct{ label, category, actor string }{
	"backlog":     {"Backlog", "not_started", "architect"},
	"planned":     {"Planned", "not_started", "daemon"},
	"working":     {"Working", "active", "worker"},
	"needs_input": {"Needs input", "active", "you"}, // architect first once V6 lands
	"blocked":     {"Blocked", "active", "you"},
	"in_review":   {"In review", "active", "reviewer"},
	"done":        {"Done", "closed", "nobody"},
	"canceled":    {"Canceled", "closed", "nobody"},
}

func newTaskState(state, code, reason, action string) taskState {
	meta := taskStateMeta[state]
	return taskState{State: state, Label: meta.label, Category: meta.category, NextActor: meta.actor, ReasonCode: code, Reason: reason, NextAction: action}
}

// taskStateRun is the run evidence the state needs. Operator is the existing
// run_state.go derivation, so lease/outcome folding lives in one place.
type taskStateRun struct {
	Lane         string
	LeaseState   string
	Terminal     bool
	AttemptCount int
	ReasonCode   string
	Operator     runOperatorState
}

type taskStateFacts struct {
	ID            string
	Status        string
	Readiness     string
	SupersededBy  string
	BlockedReason string
	WaveID        string
	WaveAuth      string // stored wave authorization; "" when the task has no wave
	ReworkCount   int
	WaitingOn     string // first unmet dependency or queue blocker
	Question      string // open worker question text
	Permission    string // pending permission request reason
	HasPermission bool
	GateText      string // first open human gate: question or title
	GateAction    string
	AutomationOff bool
	MaxAttempts   int
	Run           *taskStateRun
}

// deriveTaskState is the single function that computes a task's state. It
// reads no files or stores; adapters gather the facts.
func deriveTaskState(f taskStateFacts) taskState {
	status := strings.ToLower(strings.TrimSpace(f.Status))
	readiness := strings.ToLower(strings.TrimSpace(f.Readiness))
	switch status {
	case "done", "closed":
		return newTaskState("done", "", "", "")
	case "cancelled", "canceled", "superseded":
		if f.SupersededBy != "" {
			return newTaskState("canceled", "superseded", "superseded by "+f.SupersededBy, "")
		}
		return newTaskState("canceled", status, "", "")
	}
	if f.Question != "" {
		return newTaskState("needs_input", "question", f.Question, "Answer the question")
	}
	if f.HasPermission {
		return newTaskState("needs_input", "permission", "asks permission: "+firstNonEmpty(f.Permission, "an action"), "Allow or deny the request")
	}
	if state, ok := taskStateFromRun(f); ok {
		return state
	}
	if f.GateText != "" {
		if status == "review" {
			state := newTaskState("in_review", "landing", "waiting for your Land: "+f.GateText, firstNonEmpty(f.GateAction, "Land it"))
			state.NextActor = "you" // until auto-land accepts every harness (F14)
			return state
		}
		return newTaskState("needs_input", "gate", f.GateText, f.GateAction)
	}
	switch status {
	case "review":
		return newTaskState("in_review", "reviewing", "reviewing", "")
	case "blocked":
		return newTaskState("blocked", "outside_problem", firstNonEmpty(f.BlockedReason, "marked blocked"), "Fix the named problem, then Retry")
	case "in_progress":
		return newTaskState("working", "claimed", "in progress", "")
	}
	if readiness == "draft" || status == "draft" {
		return newTaskState("backlog", "draft", "draft", "Finish writing the task")
	}
	if readiness == "held" {
		return newTaskState("backlog", "held", "held", "")
	}
	switch strings.ToLower(f.WaveAuth) {
	case "paused":
		return newTaskState("blocked", "paused", "wave paused", "tusker wave resume "+f.WaveID)
	case "armed":
	default:
		if f.WaveID != "" {
			return newTaskState("backlog", "not_armed", "not armed", "tusker wave start "+f.WaveID)
		}
	}
	armedReady := f.WaveID != "" && (readiness == "ready" || strings.HasPrefix(readiness, "blocked_by_dep"))
	if status != "ready" && status != "rework" && !armedReady {
		return newTaskState("backlog", "not_planned", "not planned", "")
	}
	if f.WaitingOn != "" {
		return newTaskState("planned", "waiting_on", f.WaitingOn, "")
	}
	if f.AutomationOff {
		state := newTaskState("planned", "automation_off", "background work is off", "tusker projects enable")
		state.NextActor = "you"
		return state
	}
	if status == "rework" {
		return newTaskState("planned", "queued", "queued for rework", "")
	}
	return newTaskState("planned", "queued", "queued", "")
}

func taskStateFromRun(f taskStateFacts) (taskState, bool) {
	run := f.Run
	if run == nil {
		return taskState{}, false
	}
	maxAttempts := f.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultRetryMaxAttempts
	}
	switch run.Operator.State {
	case "waiting_on_you":
		return newTaskState("needs_input", "question", "the worker is waiting for you", "Open the run and reply"), true
	case "working", "quiet":
		if serveLane(run.Lane) == "review" {
			return newTaskState("in_review", "reviewing", "reviewing", ""), true
		}
		switch {
		case run.AttemptCount > 1:
			return newTaskState("working", "attempt", fmt.Sprintf("attempt %d of %d", run.AttemptCount, maxAttempts), ""), true
		case f.ReworkCount > 0 || strings.EqualFold(f.Status, "rework"):
			return newTaskState("working", "rework", "rework", ""), true
		}
		return newTaskState("working", "first_attempt", "first attempt", ""), true
	case "queued":
		if LeaseState(run.LeaseState) == LeaseStateRetryQueued {
			return newTaskState("working", "retrying", fmt.Sprintf("retry queued, attempt %d of %d", run.AttemptCount+1, maxAttempts), ""), true
		}
		return newTaskState("planned", "queued", "queued", ""), true
	case "stopped":
		return newTaskState("blocked", "paused", "stopped by you", "Continue"), true
	case "lost", "failed", "blocked":
		if LeaseState(run.LeaseState) == LeaseStateParkedBudget || run.ReasonCode == string(RunFailureMaxBudget) {
			return newTaskState("blocked", "outside_problem", "the run budget was exhausted", "Raise the budget, then Retry"), true
		}
		return blockedStateForReason(run), true
	}
	return taskState{}, false // finished: the task status decides
}

func blockedStateForReason(run *taskStateRun) taskState {
	guidance := ""
	if run.Operator.Reason != nil {
		guidance = run.Operator.Reason.Guidance
	}
	switch RunFailureReasonCode(run.ReasonCode) {
	case RunFailurePermissionDenied, RunFailureSandboxDenied, RunFailureNetworkDenied, RunFailureMissingAccess:
		return newTaskState("blocked", "not_allowed", firstNonEmpty(guidance, "a permission was denied"), "Change the profile or the task, then Retry")
	case RunFailureUsageLimit, RunFailureAuthExpired, RunFailureConfigInvalid, RunFailureContextWindow:
		return newTaskState("blocked", "outside_problem", firstNonEmpty(guidance, "an outside problem stopped the run"), "Fix the named problem, then Retry")
	case RunFailureCancelled:
		return newTaskState("blocked", "paused", "stopped by you", "Continue")
	}
	reason := fmt.Sprintf("the harness failed after %d attempts", run.AttemptCount)
	if run.AttemptCount <= 1 {
		reason = "the harness failed"
	}
	return newTaskState("blocked", "crashed", reason, "Open the log, then Retry")
}

// waveTaskState is the most urgent member state with a count reason.
func aggregateWaveState(members []taskState) taskState {
	if len(members) == 0 {
		return newTaskState("backlog", "count", "no tasks", "")
	}
	counts := map[string]int{}
	for _, member := range members {
		counts[member.State]++
	}
	var top string
	var parts []string
	for _, state := range taskStateOrder {
		if counts[state] == 0 {
			continue
		}
		if top == "" {
			top = state
		}
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], strings.ToLower(taskStateMeta[state].label)))
	}
	return newTaskState(top, "count", strings.Join(parts, ", "), "")
}

// serveTaskStateFor gathers facts from a Serve snapshot. The CLI builds the
// same snapshot shape, so both surfaces share this adapter.
func serveTaskStateFor(snap serveSnapshot, task Note) taskState {
	id := stringField(task.Data, "id")
	f := taskStateFacts{
		ID: id, Status: stringField(task.Data, "status"), Readiness: stringField(task.Data, "readiness"),
		SupersededBy:  stringField(task.Data, "superseded_by"),
		BlockedReason: firstNonEmpty(stringField(task.Data, "blocked_reason"), stringField(task.Data, "next_action")),
		WaveID:        serveTaskWaveID(snap, task),
		ReworkCount:   serveReworkCount(task),
		AutomationOff: snap.projectRegistered && !snap.project.Enabled,
		MaxAttempts:   snap.workflow.Retry.MaxAttempts,
	}
	if wave, ok := snap.notesByID[f.WaveID]; ok && f.WaveID != "" {
		f.WaveAuth = fallback(stringField(wave.Data, "authorization"), "disarmed")
	}
	if explanation, ok := snap.queue[id]; ok && len(explanation.Blockers) > 0 {
		f.WaitingOn = explanation.Blockers[0]
	} else {
		for _, dep := range serveTaskDepIDs(task) {
			if note, ok := snap.notesByID[dep]; !ok || !blockerResolved(note) {
				f.WaitingOn = "waiting on " + dep
				break
			}
		}
	}
	if questions := snap.openQuestions[id]; len(questions) > 0 {
		f.Question = firstNonEmpty(strings.TrimSpace(questions[0].Body), "the worker asked a question")
	}
	if waits := snap.permissionWaits[id]; len(waits) > 0 {
		f.HasPermission, f.Permission = true, waits[0].Reason
	}
	if gates := serveOpenHumanGatesForTask(snap, id); len(gates) > 0 {
		f.GateText = firstNonEmpty(serveAnyString(gates[0].Question), gates[0].Title, gates[0].ID)
		f.GateAction = gates[0].Action
	}
	for _, run := range snap.runs { // newest first
		if (run.ItemID == id || run.RecordID == id) && !serveRunRetired(run) {
			f.Run = taskStateRunFor(run, f.Question != "", f.HasPermission)
			break
		}
	}
	return deriveTaskState(f)
}

func serveTaskWaveID(snap serveSnapshot, task Note) string {
	id := stringField(task.Data, "id")
	for _, wave := range snap.waves {
		if containsString(normalizeList(wave.Data["members"]), id) {
			return stringField(wave.Data, "id")
		}
	}
	return stringField(task.Data, "wave")
}

func taskStateRunFor(run RunStatus, question, permission bool) *taskStateRun {
	facts := runOperatorFactsFromRun(run, nil)
	facts.LastActivityAt = run.LastEventAt
	facts.PermissionWait = permission
	if question {
		facts.OpenQuestionID = "open"
	}
	return &taskStateRun{
		Lane: run.Lane, LeaseState: run.LeaseState, Terminal: run.Terminal, AttemptCount: run.AttemptCount,
		ReasonCode: inspectedReasonCode(run, nil),
		Operator:   deriveRunOperatorState(facts, time.Now(), defaultRunQuietAfter),
	}
}

func serveWaveMemberStates(snap serveSnapshot, wave Note) []taskState {
	var out []taskState
	for _, id := range normalizeList(wave.Data["members"]) {
		if task, ok := snap.notesByID[id]; ok && serveNoteKind(task) == "task" {
			out = append(out, serveTaskStateFor(snap, task))
		}
	}
	return out
}

// cliTaskState computes the state for CLI JSON from the vault and, when it
// exists, the runtime store opened read-only.
func cliTaskState(vaultPath string, task Note) taskState {
	idx, err := loadV7Index(vaultPath)
	if err != nil {
		return serveTaskStateFor(serveSnapshot{notesByID: map[string]Note{}}, task)
	}
	snap := serveSnapshot{notesByID: map[string]Note{}}
	snap.projectID, _ = resolveV7ProjectID(vaultPath)
	if wf, err := loadWorkflow(vaultPath); err == nil {
		snap.workflow = wf.Data
	}
	for _, group := range []map[string]Note{idx.Tasks, idx.Gates, idx.Waves} {
		for id, note := range group {
			snap.notesByID[id] = note
		}
	}
	for _, note := range idx.Gates {
		snap.gates = append(snap.gates, note)
	}
	for _, note := range idx.Waves {
		snap.waves = append(snap.waves, note)
	}
	for _, note := range idx.Tasks {
		snap.tasks = append(snap.tasks, note)
	}
	if store, missing, err := openRuntimeStoreReadOnly(DefaultStateRoot()); err == nil && !missing {
		defer store.Close()
		if projects, err := store.ListProjects(); err == nil {
			for _, project := range projects {
				if canonicalTaskStatePath(project.VaultRoot) == canonicalTaskStatePath(vaultPath) || project.ProjectID == snap.projectID {
					snap.project, snap.projectRegistered, snap.projectID = project, true, project.ProjectID
					break
				}
			}
		}
		if runs, _, err := store.ListRunsForProjectPage(snap.projectID, serveSnapshotRunCap); err == nil {
			snap.runs = runs
		}
		server := &serveServer{store: store, now: time.Now}
		snap.openQuestions, _ = server.serveOpenQuestions(snap)
		snap.permissionWaits, _ = server.servePermissionWaits(snap)
	}
	return serveTaskStateFor(snap, task)
}

func canonicalTaskStatePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
