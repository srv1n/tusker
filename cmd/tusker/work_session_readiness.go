package main

import (
	"strings"
)

// workSessionAdmissionBlockers deliberately reads only the facts that make a
// user-directed work session unsafe. Daemon dispatch, automation enablement,
// wave authorization, runner health, and critical-risk dispatch policy are
// separate authority domains and cannot refuse direct interactive work.
func workSessionAdmissionBlockers(task Note, idx v7Index, notesByID, notesByRecordID map[string]Note) []ReadinessBlocker {
	taskID := stringField(task.Data, "id")
	if !isV7TaskNote(task) {
		return workSessionLegacyAdmissionBlockers(task, notesByID, notesByRecordID)
	}
	if blocker := workSessionTaskStateBlocker(task); blocker != nil {
		return []ReadinessBlocker{*blocker}
	}
	if edge, blocked := v7BlockingDependencyForReadiness(task, idx); blocked {
		return []ReadinessBlocker{workSessionDependencyBlocker(
			"interactive-dependency:"+taskID+":"+edge.ID, taskID, edge.ID,
			"Dependency "+edge.ID+" is not satisfied.",
			"Complete dependency "+edge.ID+" before starting this work session.",
		)}
	}
	if blocker := workSessionUnresolvedDependencyBlocker(task, notesByID, notesByRecordID); blocker != nil {
		return []ReadinessBlocker{*blocker}
	}
	if blocker := workSessionOpenHumanGateBlocker(task, idx); blocker != nil {
		return []ReadinessBlocker{*blocker}
	}
	return nil
}

func workSessionLegacyAdmissionBlockers(task Note, notesByID, notesByRecordID map[string]Note) []ReadinessBlocker {
	if blocker := workSessionTaskStateBlocker(task); blocker != nil {
		return []ReadinessBlocker{*blocker}
	}
	if blocker := workSessionUnresolvedDependencyBlocker(task, notesByID, notesByRecordID); blocker != nil {
		return []ReadinessBlocker{*blocker}
	}
	return nil
}

func workSessionTaskStateBlocker(task Note) *ReadinessBlocker {
	taskID := stringField(task.Data, "id")
	status := strings.ToLower(strings.TrimSpace(stringField(task.Data, "status")))
	if status == "done" || status == "cancelled" || status == "superseded" {
		return &ReadinessBlocker{
			ID: "interactive-terminal:" + taskID, Kind: ReadinessBlockerTaskTerminal, Authority: ReadinessAuthorityInteractive,
			Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID,
			Reason: "Task status is " + status + ".", Remedy: "Choose a non-terminal task or create a new task revision.",
		}
	}
	if status != "ready" && status != "rework" {
		return &ReadinessBlocker{
			ID: "interactive-state:" + taskID, Kind: ReadinessBlockerTaskNotReady, Authority: ReadinessAuthorityInteractive,
			Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID,
			Reason: "Task status is " + firstNonEmpty(status, "(missing)") + ".", Remedy: "Move the task to ready or rework before starting interactive work.",
		}
	}
	return nil
}

func workSessionUnresolvedDependencyBlocker(task Note, notesByID, notesByRecordID map[string]Note) *ReadinessBlocker {
	taskID := stringField(task.Data, "id")
	for _, raw := range normalizeList(task.Data["blocked_by"]) {
		dependencyID := wikiTarget(raw)
		if dependencyID == "" {
			continue
		}
		dependency, exists := notesByID[dependencyID]
		if !exists || !blockerResolved(dependency) {
			return &ReadinessBlocker{
				ID: "interactive-dependency:" + taskID + ":" + dependencyID, Kind: ReadinessBlockerDependencyIncomplete, Authority: ReadinessAuthorityContract,
				Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive, ReadinessDimensionContract}, TaskID: taskID, DependencyTaskID: dependencyID,
				Reason: "Dependency " + dependencyID + " is not satisfied.", Remedy: "Complete dependency " + dependencyID + " before starting this work session.",
			}
		}
	}
	for _, raw := range normalizeList(task.Data["blocked_by_record_ids"]) {
		recordID := strings.TrimSpace(raw)
		if recordID == "" {
			continue
		}
		dependency, exists := notesByRecordID[recordID]
		if !exists || !blockerResolved(dependency) {
			dependencyID := firstNonEmpty(stringField(dependency.Data, "id"), recordID)
			return &ReadinessBlocker{
				ID: "interactive-dependency:" + taskID + ":" + dependencyID, Kind: ReadinessBlockerDependencyIncomplete, Authority: ReadinessAuthorityContract,
				Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive, ReadinessDimensionContract}, TaskID: taskID, DependencyTaskID: dependencyID,
				Reason: "Dependency " + dependencyID + " is not satisfied.", Remedy: "Complete dependency " + dependencyID + " before starting this work session.",
			}
		}
	}
	return nil
}

func workSessionOpenHumanGateBlocker(task Note, idx v7Index) *ReadinessBlocker {
	taskID := stringField(task.Data, "id")
	for _, gate := range sortedV7Gates(idx) {
		if !v7GateTouchesTask(gate, taskID) || !boolField(gate.Data, "blocking") || !strings.EqualFold(stringField(gate.Data, "status"), "open") {
			continue
		}
		owner := stringField(gate.Data, "owner")
		kind := strings.ToLower(strings.TrimSpace(stringField(gate.Data, "gate_kind")))
		action := strings.TrimSpace(stringField(gate.Data, "action"))
		verification := strings.TrimSpace(stringField(gate.Data, "verification"))
		why := v7GateBoundaryText(gate)
		if _, knownKind := v7GateKinds[kind]; v7ProofOwnerClass(owner) != "human" || !knownKind || action == "" || verification == "" || why == "" || v7GateTextIsPlaceholder(action) || v7GateTextIsPlaceholder(verification) || v7HumanGateOwnsAgentCapableWork(kind, owner, action, verification, why, v7GateSuggestionText(gate)) || (kind == "decision" && !v7GateHasSuggestion(gate)) {
			continue
		}
		gateID := stringField(gate.Data, "id")
		return &ReadinessBlocker{
			ID: "interactive-human-gate:" + taskID + ":" + gateID, Kind: ReadinessBlockerHumanGateOpen, Authority: ReadinessAuthorityHuman,
			Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID, GateID: gateID,
			Reason: "Human gate " + gateID + " is open.", Remedy: "Complete the required human action for gate " + gateID + ".",
		}
	}
	return nil
}

func workSessionUnsafeWorkspaceBlocker(taskID, reason string) ReadinessBlocker {
	return ReadinessBlocker{
		ID: "interactive-workspace:" + taskID, Kind: ReadinessBlockerWorkspaceUnsafe, Authority: ReadinessAuthorityInteractive,
		Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID,
		Reason: "Workspace is unsafe: " + strings.TrimSpace(reason), Remedy: "Repair the workspace or choose a safe exact workspace before starting work.",
	}
}

func workSessionDependencyBlocker(id, taskID, dependencyID, reason, remedy string) ReadinessBlocker {
	blocker := NewDependencyReadinessBlocker(id, taskID, dependencyID, reason, remedy)
	blocker.Affects = []ReadinessDimensionKind{ReadinessDimensionInteractive, ReadinessDimensionContract}
	return blocker
}

func workSessionOwnerBlocker(taskID, owner, reason string) ReadinessBlocker {
	return ReadinessBlocker{
		ID: "interactive-owner:" + taskID, Kind: ReadinessBlockerInteractiveOwner, Authority: ReadinessAuthorityInteractive,
		Affects: []ReadinessDimensionKind{ReadinessDimensionInteractive}, TaskID: taskID, Owner: owner,
		Reason: strings.TrimSpace(reason), Remedy: "Wait for the current owner to release the work session or reclaim it after the holder is safely dead.",
	}
}
