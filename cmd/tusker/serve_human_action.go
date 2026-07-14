package main

import (
	"sort"
	"strings"
)

// serveHumanActionForTask returns the first open human-owned gate in stable
// gate-id order. A task can have several gates, but the operator needs one
// unambiguous next action; the gate's covers field keeps the checklist narrow.
func serveHumanActionForTask(snap serveSnapshot, task Note) *serveHumanAction {
	actions := serveHumanActionsForTask(snap, task)
	if len(actions) == 0 {
		return nil
	}
	return &actions[0]
}

func serveHumanActionsForTask(snap serveSnapshot, task Note) []serveHumanAction {
	if strings.EqualFold(stringField(task.Data, "status"), "rework") {
		return []serveHumanAction{}
	}

	gates := make([]Note, 0)
	taskID := stringField(task.Data, "id")
	for _, gate := range snap.gates {
		if strings.EqualFold(stringField(gate.Data, "status"), "open") &&
			serveHumanOwner(stringField(gate.Data, "owner")) &&
			serveGateBlocksTask(gate, taskID) {
			gates = append(gates, gate)
		}
	}
	sort.Slice(gates, func(i, j int) bool {
		return stringField(gates[i].Data, "id") < stringField(gates[j].Data, "id")
	})
	if len(gates) == 0 {
		return []serveHumanAction{}
	}
	actions := make([]serveHumanAction, 0, len(gates))
	for _, gate := range gates {
		if action := serveHumanActionForGate(task, gate); action != nil {
			actions = append(actions, *action)
		}
	}
	return actions
}

func serveHumanActionForGate(task Note, gate Note) *serveHumanAction {
	acceptance := serveAcceptanceRows(task)
	acceptanceIDs := make([]string, 0, len(acceptance))
	for _, row := range acceptance {
		acceptanceIDs = append(acceptanceIDs, row.ID)
	}

	covers := normalizeList(gate.Data["covers"])
	coveredIDs := v7CoversToAcceptanceIDs(covers, acceptanceIDs)
	// V7 proof semantics treat a blocking gate with no explicit covers as
	// covering the task's full acceptance contract. Keep the served checklist
	// aligned with that canonical rule.
	if len(coveredIDs) == 0 && serveGateBlocksTask(gate, stringField(task.Data, "id")) {
		coveredIDs = append(coveredIDs, acceptanceIDs...)
	}
	coveredSet := make(map[string]bool, len(coveredIDs))
	for _, id := range coveredIDs {
		coveredSet[id] = true
	}
	coveredAcceptance := make([]serveAcceptanceRow, 0, len(coveredIDs))
	for _, row := range acceptance {
		if coveredSet[row.ID] {
			coveredAcceptance = append(coveredAcceptance, row)
		}
	}

	rawKind := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		stringField(gate.Data, "gate_kind"),
		stringField(gate.Data, "kind"),
	)))
	kind := serveHumanActionKind(rawKind)
	title := firstNonEmpty(stringField(gate.Data, "title"), serveHumanActionKindTitle(kind))
	action := firstNonEmpty(stringField(gate.Data, "action"), sectionContent(gate.Body, "## Action"), title)
	why := firstNonEmpty(stringField(gate.Data, "why_agent_cannot"), sectionContent(gate.Body, "## Why agent cannot do this"))
	completion := firstNonEmpty(stringField(gate.Data, "verification"), sectionContent(gate.Body, "## Verification"))

	return &serveHumanAction{
		Kind:                kind,
		RawKind:             rawKind,
		Title:               title,
		Action:              action,
		WhyAgentCannot:      why,
		CompletionCondition: completion,
		GateID:              stringField(gate.Data, "id"),
		BlockedTaskIDs:      serveGateBlockIDs(gate),
		Covers:              coveredIDs,
		Acceptance:          coveredAcceptance,
	}
}

func serveHumanActionKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "verification":
		return "manual-verification"
	case "decision":
		return "decision"
	case "signoff":
		return "signoff"
	case "release":
		return "release"
	case "provision", "provisioning", "auth", "env", "setup", "dev_host", "ci", "quota", "external_service", "security":
		return "provision"
	default:
		return "human-action"
	}
}

func serveHumanActionKindTitle(kind string) string {
	switch kind {
	case "manual-verification":
		return "Manual verification"
	case "decision":
		return "Decision needed"
	case "signoff":
		return "Sign-off needed"
	case "release":
		return "Release approval"
	case "provision":
		return "Provisioning needed"
	default:
		return "Human action needed"
	}
}
