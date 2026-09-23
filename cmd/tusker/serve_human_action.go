package main

import (
	"sort"
	"strings"
	"time"
)

func (s *serveServer) serveOpenQuestions(snap serveSnapshot) (map[string][]AgentMessage, error) {
	result := map[string][]AgentMessage{}
	messages, err := s.store.ListAgentMessages(snap.projectID, "", "")
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		if message.Kind != "question" || !message.ReplyRequired || message.AnsweredAt != "" {
			continue
		}
		if message.ExpiresAt != "" {
			expires, err := time.Parse(time.RFC3339Nano, message.ExpiresAt)
			if err == nil && !s.now().Before(expires) {
				continue
			}
		}
		if !strings.HasPrefix(message.Sender, "task:") {
			continue
		}
		taskID := strings.TrimPrefix(message.Sender, "task:")
		if _, exists := snap.notesByID[taskID]; !exists {
			continue
		}
		operator := message.Recipient == (AgentAddress{Kind: "operator", ID: "operator"})
		if !operator {
			contacts, err := s.store.AgentContacts(snap.projectID, taskID)
			if err != nil {
				return nil, err
			}
			matched := false
			for _, contact := range contacts {
				if contact.Address != message.Recipient {
					continue
				}
				matched = true
				binding, err := s.store.ResolveAgentContactBinding(snap.projectID, taskID, contact.Role, contact.Name)
				if err == nil && binding.State == AgentContactBindingBound {
					operator = false
					break
				}
				operator = true
			}
			if !matched {
				for _, role := range []string{"architect", "origin"} {
					binding, err := s.store.ResolveAgentContactBinding(snap.projectID, taskID, role, "")
					if err == nil && binding.Contact.Address == message.Recipient {
						matched = true
						operator = binding.State != AgentContactBindingBound
						break
					}
				}
			}
			if !matched {
				operator = true
			}
		}
		if operator {
			result[taskID] = append(result[taskID], message)
		}
	}
	return result, nil
}

func (s *serveServer) servePermissionWaits(snap serveSnapshot) (map[string][]AgentAccessApproval, error) {
	result := map[string][]AgentAccessApproval{}
	approvals, err := s.store.ListAgentAccessApprovals(snap.projectID)
	if err != nil {
		return nil, err
	}
	for _, approval := range approvals {
		if approval.State != "pending" || approval.NativeOptionKind != "allow_once" || approval.NativeOptionID == "" {
			continue
		}
		if _, ok := snap.notesByID[approval.TaskID]; !ok {
			continue
		}
		expires, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
		if err != nil || !s.now().Before(expires) {
			continue
		}
		if approval.LiveUntil != "" {
			until, err := time.Parse(time.RFC3339Nano, approval.LiveUntil)
			if err != nil || !s.now().Before(until) {
				continue
			}
		}
		result[approval.TaskID] = append(result[approval.TaskID], approval)
	}
	return result, nil
}

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
	gates := make([]Note, 0)
	taskID := stringField(task.Data, "id")
	if !strings.EqualFold(stringField(task.Data, "status"), "rework") {
		for _, gate := range snap.gates {
			if strings.EqualFold(stringField(gate.Data, "status"), "open") &&
				serveHumanOwner(stringField(gate.Data, "owner")) &&
				serveGateBlocksTask(gate, taskID) {
				gates = append(gates, gate)
			}
		}
	}
	sort.Slice(gates, func(i, j int) bool {
		return stringField(gates[i].Data, "id") < stringField(gates[j].Data, "id")
	})
	actions := make([]serveHumanAction, 0, len(gates))
	for _, gate := range gates {
		if action := serveHumanActionForGate(task, gate); action != nil {
			actions = append(actions, *action)
		}
	}
	for _, message := range snap.openQuestions[taskID] {
		actions = append(actions, serveQuestionHumanAction(message, taskID))
	}
	for _, approval := range snap.permissionWaits[taskID] {
		actions = append(actions, serveHumanAction{Kind: "permission", RawKind: "permission", Title: "Permission requested by " + taskID,
			Action: approval.Reason, TaskID: taskID, RequestID: approval.RequestID, GateID: "permission-" + approval.RequestID, BlockedTaskIDs: []string{taskID}, Covers: []string{}, Acceptance: []serveAcceptanceRow{}})
	}
	return actions
}

func serveQuestionHumanAction(message AgentMessage, taskID string) serveHumanAction {
	return serveHumanAction{Kind: "question", RawKind: "question", Title: "Question from " + taskID,
		Action: message.Body, Body: message.Body, MessageID: message.ID, GateID: "question-" + message.ID, AskedAt: message.CreatedAt,
		RecipientLabel: message.Recipient.Kind + ":" + message.Recipient.ID, TaskID: taskID,
		YieldSender: message.YieldSender, BlockedTaskIDs: []string{taskID}, Covers: []string{}, Acceptance: []serveAcceptanceRow{}}
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
		MaterialRevision:    stringField(gate.Data, "state_rev"),
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
