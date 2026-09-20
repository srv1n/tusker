package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

const museProbeTimeout = 15 * time.Second

type MuseCommand struct {
	Args       []string
	Stdin      string
	OutputPath string
}

type MuseCommandRunner func(context.Context, MuseCommand) (string, error)

func museCLICommandRunner(ctx context.Context, command MuseCommand) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, museProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "muse", command.Args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if command.Stdin != "" {
		cmd.Stdin = strings.NewReader(command.Stdin)
	}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if command.OutputPath != "" {
		raw, err := os.ReadFile(command.OutputPath)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return stdout.String(), nil
}

func museRecords(raw string) []map[string]any {
	var records []map[string]any
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			records = append(records, typed)
			for _, nested := range typed {
				walk(nested)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	var decoded any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decoded); err == nil {
		walk(decoded)
		return records
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &decoded); err == nil {
			walk(decoded)
		}
	}
	return records
}

func museStringField(record map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[key].(string); ok {
			return value
		}
	}
	return ""
}

func museSessionRecords(records []map[string]any, sessionID string) []map[string]any {
	var matched []map[string]any
	for _, record := range records {
		stream, _ := record["stream"].(map[string]any)
		if museStringField(stream, "kind") == "session" && museStringField(stream, "id") == sessionID {
			matched = append(matched, record)
		}
	}
	return matched
}

func museCausationID(record map[string]any) string {
	return museStringField(record, "causation_id", "causationId")
}

func musePayloadType(record map[string]any) string {
	return museStringField(record, "payload_type", "payloadType")
}

func musePayloadText(record map[string]any) string {
	if payload, ok := record["payload"].(map[string]any); ok {
		if text := museStringField(payload, "text", "output", "message"); text != "" {
			return text
		}
	}
	return museStringField(record, "text", "output", "message")
}

func museDurableAcceptance(records []map[string]any) (string, bool) {
	for _, record := range records {
		switch musePayloadType(record) {
		case "runtime.command.accepted":
			if id := museCausationID(record); id != "" {
				return id, true
			}
		case "runtime.command_intake.settled":
			payload, _ := record["payload"].(map[string]any)
			intake, _ := payload["record"].(map[string]any)
			if museStringField(intake, "kind") != "settled" {
				continue
			}
			outcome, _ := intake["outcome"].(map[string]any)
			if museStringField(outcome, "kind") != "accepted" {
				continue
			}
			commandID := museStringField(intake, "command_id", "commandId")
			if commandID == "" {
				continue
			}
			if causationID := museCausationID(record); causationID != "" && causationID != commandID {
				continue
			}
			return commandID, true
		}
	}
	return "", false
}

func museTerminalRecord(records []map[string]any, causationID string) (map[string]any, bool) {
	for _, record := range records {
		switch musePayloadType(record) {
		case "run.terminal.completed", "run.terminal.failed", "run.terminal.cancelled":
			if museCausationID(record) == causationID {
				return record, true
			}
		}
	}
	return nil, false
}

func museIngressClosed(raw string) bool {
	return strings.Contains(raw, "external_agent_ingress_closed")
}

func QualifyMuseEndpoint(ctx context.Context, run MuseCommandRunner, sessionID string) (WorkerProviderQualification, error) {
	if run == nil {
		return WorkerProviderQualification{}, tuskerError(errorInvalidArg, "muse qualification requires a command runner")
	}
	if strings.TrimSpace(sessionID) == "" {
		return WorkerProviderQualification{}, tuskerError(errorInvalidArg, "muse qualification requires an exact session id")
	}
	qualification := WorkerProviderQualification{Provider: "muse", Endpoint: "muse cli", QualifiedAt: workerNow()}
	if version, err := run(ctx, MuseCommand{Args: []string{"--version"}}); err == nil {
		qualification.Version = strings.TrimSpace(version)
	}
	var evidence []string

	dir, err := os.MkdirTemp("", "tusker-muse-qualify-")
	if err != nil {
		return WorkerProviderQualification{}, err
	}
	defer os.RemoveAll(dir)
	exportPath := dir + string(os.PathSeparator) + "session-export.json"
	exported, exportErr := run(ctx, MuseCommand{Args: []string{"export", "--session", sessionID, "--out", exportPath, "--redacted"}, OutputPath: exportPath})
	if strings.TrimSpace(exported) == "" {
		if raw, readErr := os.ReadFile(exportPath); readErr == nil {
			exported = string(raw)
		}
	}
	var exportRecords []map[string]any
	if exportErr == nil && strings.TrimSpace(exported) != "" {
		exportRecords = museRecords(exported)
		if len(museSessionRecords(exportRecords, sessionID)) > 0 || strings.Contains(exported, sessionID) {
			qualification.Capabilities.AttachExternal = true
			evidence = append(evidence, "attach:exact session present in redacted export")
		}
	}

	probeBody := "tusker qualification probe " + newRecordID()
	if listOut, err := run(ctx, MuseCommand{Args: []string{"session-message", "list", "--json"}}); err == nil {
		sendOut, sendErr := run(ctx, MuseCommand{Args: []string{"session-message", "send", "--target", sessionID, "--json"}, Stdin: probeBody})
		if sendErr == nil && !museIngressClosed(listOut+sendOut) {
			accepted := false
			for _, record := range museRecords(sendOut) {
				if museStringField(record, "target", "session", "session_id", "id") == sessionID || strings.Contains(sendOut, sessionID) {
					accepted = true
					break
				}
			}
			if accepted || strings.Contains(sendOut, sessionID) {
				qualification.Capabilities.DeliverActive = true
				evidence = append(evidence, "deliver:ingress list succeeded and exact-target send probe was accepted")
			}
		}
	}

	if _, err := run(ctx, MuseCommand{Args: []string{"exec", "--provider", "echo", "--session-id", sessionID, "--json", "tusker qualification probe 1"}}); err == nil {
		second, err := run(ctx, MuseCommand{Args: []string{"exec", "--provider", "echo", "--session-id", sessionID, "--json", "tusker qualification probe 2"}})
		if err == nil {
			records := museRecords(second)
			sessionRecords := museSessionRecords(records, sessionID)
			if len(sessionRecords) > 0 || strings.Contains(second, sessionID) {
				qualification.Capabilities.ResumeIdle = true
				evidence = append(evidence, "resume:second echo exec produced records for the exact session")
			}
			if causationID, ok := museDurableAcceptance(sessionRecords); ok {
				if terminal, ok := museTerminalRecord(sessionRecords, causationID); ok {
					qualification.Capabilities.ObserveTurn = true
					evidence = append(evidence, "observe:durable command acceptance and run terminal share a causation id")
					if musePayloadText(terminal) != "" {
						qualification.Capabilities.CaptureReply = true
					}
				}
			}
		}
	}

	if causationID, ok := museDurableAcceptance(exportRecords); ok {
		qualification.Capabilities.ReconcileDelivery = true
		evidence = append(evidence, "reconcile:redacted export contains durable command acceptance "+causationID)
	}

	qualification.Capabilities.Endpoint = qualification.Endpoint
	qualification.Capabilities.Version = qualification.Version
	qualification.Capabilities.Evidence = strings.Join(evidence, "; ")
	return qualification, nil
}

func SendMuseWorkerDelivery(ctx context.Context, store *RuntimeStore, deliveryID string, run MuseCommandRunner) error {
	delivery, claimed, err := store.ClaimWorkerDelivery(deliveryID)
	if err != nil {
		return err
	}
	if !claimed {
		switch delivery.State {
		case "delivering":
			return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" is already in flight")
		case "accepted", "uncertain", "replied", "applied":
			return nil
		default:
			return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" cannot be sent from state "+delivery.State)
		}
	}
	current, err := store.workerIdentityCurrent(delivery.Identity)
	if err != nil {
		return err
	}
	if !current {
		if markErr := store.markWorkerDeliveryState(deliveryID, map[string]bool{"delivering": true}, "stale", "worker identity is stale or superseded"); markErr != nil {
			return markErr
		}
		return tuskerError(errorInvalidTransition, "worker delivery "+deliveryID+" identity is stale or superseded")
	}
	out, sendErr := run(ctx, MuseCommand{
		Args:  []string{"session-message", "send", "--target", delivery.Identity.NativeSessionID, "--json"},
		Stdin: workerDeliveryOutbound(delivery.DeliveryID, delivery.Body),
	})
	if sendErr != nil || museIngressClosed(out) {
		reason := "muse send result is ambiguous"
		if sendErr != nil {
			reason += ": " + sendErr.Error()
		}
		if museIngressClosed(out) {
			reason = "muse ingress is closed for the exact session"
		}
		if markErr := store.MarkWorkerDeliveryUncertain(deliveryID, reason); markErr != nil {
			return markErr
		}
		if sendErr != nil {
			return sendErr
		}
		return tuskerError(errorInvalidTransition, "muse send closed ingress; delivery marked uncertain")
	}
	receipt := map[string]any{"target": delivery.Identity.NativeSessionID}
	for _, record := range museRecords(out) {
		if id := museStringField(record, "event_id", "message_id", "receipt_id", "id"); id != "" {
			receipt["event_id"] = id
			break
		}
	}
	return store.MarkWorkerDeliveryAccepted(deliveryID, receipt, workerNow())
}

func ReconcileMuseWorker(ctx context.Context, store *RuntimeStore, identity WorkerAttemptIdentity, run MuseCommandRunner, now time.Time, interval time.Duration) error {
	if err := identity.validate(); err != nil {
		return err
	}
	current, err := store.workerIdentityCurrent(identity)
	if err != nil {
		return err
	}
	out, err := run(ctx, MuseCommand{Args: []string{"session-message", "list", "--json"}})
	if err != nil {
		return err
	}
	records := museRecords(out)
	var events []map[string]any
	for _, record := range records {
		target := museStringField(record, "target", "session_id", "session", "sessionId")
		stream, _ := record["stream"].(map[string]any)
		if streamID := museStringField(stream, "id"); streamID != "" {
			target = streamID
		}
		if target != identity.NativeSessionID {
			continue
		}
		events = append(events, record)
		eventID := museStringField(record, "event_id", "id")
		occurred := museStringField(record, "created_at", "timestamp", "occurred_at", "ts")
		if _, _, err := store.RecordWorkerProviderActivity(WorkerProviderActivity{
			Identity:        identity,
			ProviderEventID: eventID,
			Cursor:          eventID,
			Source:          museStringField(record, "source", "direction", "role"),
			OccurredAt:      occurred,
		}); err != nil {
			return err
		}
	}
	current, err = store.workerIdentityCurrent(identity)
	if err != nil {
		return err
	}
	if current {
		pending, err := store.workerPendingDeliveries(identity, "delivering", "accepted", "uncertain")
		if err != nil {
			return err
		}
		consumed := map[string]bool{}
		for _, delivery := range pending {
			if delivery.CorrelatedReplyEventID != "" {
				consumed[delivery.CorrelatedReplyEventID] = true
			}
		}
		for _, delivery := range pending {
			tag := workerDeliveryTag(delivery.DeliveryID)
			observedAt := delivery.WorkerActivityObservedAt
			if observedAt == "" {
				for _, record := range events {
					if strings.Contains(museRecordText(record), tag) {
						observedAt = museStringField(record, "created_at", "timestamp", "occurred_at", "ts")
						if err := store.markWorkerDeliveryObserved(delivery.DeliveryID, observedAt); err != nil {
							return err
						}
						break
					}
				}
			}
			observed, ok := parseWorkerTimestamp(observedAt)
			if observedAt == "" || !ok {
				continue
			}
			for _, record := range events {
				if !museProviderReplyRecord(record) || !strings.Contains(museRecordText(record), tag) {
					continue
				}
				eventID := museStringField(record, "event_id", "id")
				if eventID != "" && consumed[eventID] {
					continue
				}
				ts := museStringField(record, "created_at", "timestamp", "occurred_at", "ts")
				parsed, ok := parseWorkerTimestamp(ts)
				if !ok || !parsed.After(observed) {
					continue
				}
				if err := store.MarkWorkerDeliveryReply(delivery.DeliveryID, eventID, ts); err != nil {
					return err
				}
				if eventID != "" {
					consumed[eventID] = true
				}
				break
			}
		}
	}
	_, err = store.EvaluateWorkerAttention(identity, now, interval)
	return err
}

func museProviderReplyRecord(record map[string]any) bool {
	switch strings.ToLower(museStringField(record, "source", "direction", "role")) {
	case "provider", "assistant", "agent", "muse", "worker", "outbound_reply", "inbound":
		return true
	}
	return false
}

func museRecordText(record map[string]any) string {
	if text := musePayloadText(record); text != "" {
		return text
	}
	raw, _ := json.Marshal(record)
	return string(raw)
}
