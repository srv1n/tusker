package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func serveNeeds(snap serveSnapshot, now time.Time) []serveNeedItem {
	needs := []serveNeedItem{}
	seenHumanGates := map[string]bool{}
	for _, task := range snap.tasks {
		cap := serveTaskCapsuleFor(snap, task)
		blocking := serveBlockingCount(snap, cap.ID)
		for _, gate := range serveUnsatisfiedGatesForTask(snap, cap.ID) {
			if !serveHumanOwner(gate.Owner) {
				continue
			}
			if seenHumanGates[gate.ID] {
				continue
			}
			if strings.EqualFold(stringField(task.Data, "status"), "rework") {
				continue
			}
			seenHumanGates[gate.ID] = true
			needs = append(needs, serveGateNeed(snap, task, cap, gate, blocking))
		}
		if cap.ReworkCount >= 2 {
			needs = append(needs, serveReworkNeed(snap, task, cap, blocking))
		}
	}
	maxAttempts := snap.workflow.Retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	for _, run := range snap.runs {
		if !serveTerminalFailure(run, maxAttempts) {
			continue
		}
		taskID := firstNonEmpty(run.ItemID, run.RecordID)
		task, _ := snap.notesByID[taskID]
		cap := serveTaskCapsuleFor(snap, task)
		if cap.ID == "" {
			cap.ID = taskID
			cap.Title = firstNonEmpty(run.ItemID, run.RecordID)
			cap.Priority = "p2"
			cap.UpdatedAt = firstNonEmpty(run.UpdatedAt, run.LastEventAt)
		}
		needs = append(needs, serveFailedNeed(snap, cap, serveBlockingCount(snap, taskID), run))
	}
	sort.Slice(needs, func(i, j int) bool {
		bi := intValue(needs[i]["blocking"])
		bj := intValue(needs[j]["blocking"])
		if bi != bj {
			return bi > bj
		}
		return servePriorityRank(fmt.Sprint(needs[i]["priority"])) < servePriorityRank(fmt.Sprint(needs[j]["priority"]))
	})
	_ = now
	return needs
}

func serveNeedBaseMap(snap serveSnapshot, task Note, cap serveTaskCapsule, kind string, blocking int) serveNeedItem {
	return serveNeedItem{
		"id":          "need-" + kind + "-" + cap.ID,
		"kind":        kind,
		"projectId":   snap.projectID,
		"projectName": snap.projectName,
		"taskId":      cap.ID,
		"taskTitle":   cap.Title,
		"blocking":    blocking,
		"priority":    cap.Priority,
		"since":       firstNonEmpty(cap.UpdatedAt, serveUpdatedAt(task)),
	}
}

func serveReviewNeed(snap serveSnapshot, task Note, cap serveTaskCapsule, blocking int) serveNeedItem {
	need := serveNeedBaseMap(snap, task, cap, "review", blocking)
	need["acceptance"] = serveAcceptanceRows(task)
	return need
}

func serveReworkNeed(snap serveSnapshot, task Note, cap serveTaskCapsule, blocking int) serveNeedItem {
	need := serveNeedBaseMap(snap, task, cap, "review", blocking)
	need["acceptance"] = serveAcceptanceRows(task)
	need["reason"] = "Task bounced through review to rework at least twice."
	need["reworkCount"] = cap.ReworkCount
	return need
}

func serveGateNeed(snap serveSnapshot, task Note, cap serveTaskCapsule, gate serveGate, blocking int) serveNeedItem {
	need := serveNeedBaseMap(snap, task, cap, gate.Kind, blocking)
	need["id"] = "need-gate-" + gate.ID
	need["gateId"] = gate.ID
	blockedTaskIDs := serveGateBlockedTaskIDs(snap, gate.ID)
	if len(blockedTaskIDs) == 0 {
		blockedTaskIDs = []string{cap.ID}
	}
	need["blockedTaskIds"] = blockedTaskIDs
	if action := serveHumanActionForTaskGate(snap, task, gate.ID); action != nil {
		need["humanAction"] = action
	}
	switch gate.Kind {
	case "clarify":
		need["question"] = firstNonEmpty(serveAnyString(gate.Question), "Human input needed on "+cap.Title+".")
	case "provision":
		need["ask"] = firstNonEmpty(serveAnyString(gate.Ask), "Provisioning required for "+cap.Title+".")
		if gate.Path != nil {
			need["path"] = gate.Path
		}
	case "approve-spec":
		need["specTitle"] = firstNonEmpty(serveAnyString(gate.SpecTitle), cap.Title)
		need["specPath"] = firstNonEmpty(serveAnyString(gate.SpecPath), "")
	}
	return need
}

func serveHumanActionForTaskGate(snap serveSnapshot, task Note, gateID string) *serveHumanAction {
	if strings.EqualFold(stringField(task.Data, "status"), "rework") {
		return nil
	}
	for _, gate := range snap.gates {
		if stringField(gate.Data, "id") != gateID || !strings.EqualFold(stringField(gate.Data, "status"), "open") || !serveHumanOwner(stringField(gate.Data, "owner")) {
			continue
		}
		return serveHumanActionForGate(task, gate)
	}
	return nil
}

func serveAnyString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func serveFailedNeed(snap serveSnapshot, cap serveTaskCapsule, blocking int, run RunStatus) serveNeedItem {
	need := serveNeedItem{
		"id":          "need-failed-" + cap.ID,
		"kind":        "failed",
		"projectId":   snap.projectID,
		"projectName": snap.projectName,
		"taskId":      cap.ID,
		"taskTitle":   cap.Title,
		"blocking":    blocking,
		"priority":    firstNonEmpty(cap.Priority, "p2"),
		"since":       firstNonEmpty(cap.UpdatedAt, run.UpdatedAt, run.LastEventAt),
		"lastError":   firstNonEmpty(run.LastError, "Run exhausted its retry budget with no lease able to continue."),
		"attempts":    run.AttemptCount,
	}
	return need
}

func serveHumanOwner(owner string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(owner)), "human")
}

func serveTerminalFailure(run RunStatus, maxAttempts int) bool {
	// A retired run has been acknowledged (or otherwise cleared); it never
	// re-enters the attention surface even though its outcome stays "failed".
	if serveRunRetired(run) {
		return false
	}
	if LeaseState(run.LeaseState) == LeaseStateParkedNoProgress {
		return true
	}
	if LeaseState(run.LeaseState) == LeaseStateRetryQueued || isDispatchingLeaseState(run.LeaseState) {
		return false
	}
	outcome := AttemptOutcome(strings.TrimSpace(run.AttemptOutcome))
	if outcome != AttemptOutcomeFailed && outcome != AttemptOutcomeBudgetExceeded {
		return false
	}
	return run.AttemptCount >= maxAttempts || strings.TrimSpace(run.NextRetryAt) == ""
}

func serveBlockingCount(snap serveSnapshot, taskID string) int {
	count := 0
	for _, task := range snap.tasks {
		if stringField(task.Data, "id") == taskID {
			continue
		}
		if containsString(serveTaskDepIDs(task), taskID) {
			count++
		}
	}
	return count
}

func servePriorityRank(priority string) int {
	switch strings.ToLower(priority) {
	case "p0":
		return 0
	case "p1":
		return 1
	case "p2":
		return 2
	case "p3":
		return 3
	default:
		return 9
	}
}

func serveReworkCount(task Note) int {
	count := 0
	for _, transition := range normalizeTransitionList(task.Data["transitions"]) {
		to := strings.ToLower(toString(transition["to"]))
		if to == "rework" {
			count++
		}
	}
	return count
}

func normalizeTransitionList(value any) []map[string]any {
	var out []map[string]any
	switch transitions := value.(type) {
	case []any:
		for _, transition := range transitions {
			if m, ok := transition.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case []orderedMap:
		for _, transition := range transitions {
			m := map[string]any{}
			for _, entry := range transition {
				m[entry.Key] = entry.Value
			}
			out = append(out, m)
		}
	}
	return out
}
