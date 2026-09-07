package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"tusker/internal/acp"
)

func executeACP(ctx context.Context, prepared PreparedLaunch, sink EventSink) (ExecutionReceipt, error) {
	receipt := ExecutionReceipt{Schema: "tusker.runner-execution/v1", AttemptID: eventAttempt(prepared), LaunchHash: prepared.LaunchHash, HarnessID: prepared.HarnessID, Provider: prepared.Provider, Transport: prepared.Transport, Version: prepared.Version, StartedAt: time.Now().UTC(), RequestedPreset: prepared.RequestedPreset, EffectivePolicy: prepared.EffectivePolicy}
	var eventMu sync.Mutex
	emit := func(kind EventType, reason string, data json.RawMessage) {
		eventMu.Lock()
		defer eventMu.Unlock()
		event := Event{AttemptID: receipt.AttemptID, Sequence: uint64(len(receipt.Events) + 1), ObservedAt: time.Now().UTC(), Type: kind, Reason: reason, Data: data}
		receipt.Events = append(receipt.Events, event)
		if sink != nil {
			_ = sink(ctx, event)
		}
	}
	if !prepared.Capabilities["native_containment"] {
		return receipt, errors.New("ACP execution requires verified native containment")
	}
	runCtx, cancel := context.WithTimeout(ctx, prepared.Deadline)
	defer cancel()
	var stderr bytes.Buffer
	client, err := acp.Start(runCtx, acp.Config{
		Argv: prepared.Argv, CWD: prepared.CWD, Env: prepared.Environment, Stderr: &stderr,
		Limits:   acp.Limits{MaxFrameBytes: maxProtocolFrame, MaxUpdateBytes: prepared.OutputLimit},
		Timeouts: acp.Timeouts{Prompt: prepared.Deadline},
		PermissionHandler: func(context.Context, acp.PermissionRequest) (acp.PermissionDecision, error) {
			emit(EventPermissionRequest, "denied by unattended policy", nil)
			return acp.Reject, nil
		},
	})
	if err != nil {
		receipt.FinishedAt, receipt.Outcome, receipt.Reason = time.Now().UTC(), EventFailed, bounded(err.Error(), 500)
		emit(EventFailed, receipt.Reason, nil)
		return receipt, err
	}
	defer client.Close()
	emit(EventStarted, "", nil)
	if _, err = client.Initialize(runCtx); err == nil {
		_, err = client.NewSession(runCtx)
	}
	var result acp.PromptResult
	if err == nil {
		result, err = client.Prompt(runCtx, prepared.prompt)
	}
	for {
		select {
		case update := <-client.Updates():
			emit(EventProgress, update.Method, nil)
		default:
			goto updatesDrained
		}
	}
updatesDrained:
	receipt.FinishedAt, receipt.Stderr = time.Now().UTC(), bounded(stderr.String(), prepared.OutputLimit)
	if err != nil {
		if runCtx.Err() != nil {
			receipt.Reason = "timeout"
		} else {
			receipt.Reason = bounded(err.Error(), 500)
		}
		receipt.Outcome = EventFailed
		emit(EventFailed, receipt.Reason, nil)
		return receipt, err
	}
	receipt.SessionID = result.SessionID
	if result.Outcome != acp.OutcomeCompleted {
		receipt.Outcome, receipt.Reason = EventFailed, string(result.Outcome)
		emit(EventFailed, receipt.Reason, result.Raw)
		return receipt, errors.New(receipt.Reason)
	}
	receipt.Outcome = EventCompleted
	emit(EventCompleted, "", result.Raw)
	return receipt, nil
}
