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
	launchArgv := prepared.Argv
	if len(prepared.LaunchArgv) > 0 {
		launchArgv = prepared.LaunchArgv
	}
	client, err := acp.Start(runCtx, acp.Config{
		Argv: launchArgv, CWD: prepared.CWD, Env: prepared.Environment, Stderr: &stderr,
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
	var session acp.Session
	if _, err = client.Initialize(runCtx); err == nil {
		session, err = client.NewSession(runCtx)
	}
	if err == nil && prepared.Provider == "devin" {
		_, err = client.SetConfigOption(runCtx, "mode", "smart")
		if err == nil {
			model, thought := DevinACPModelAndThought(session, prepared.Model, prepared.Effort)
			if _, err = client.SetConfigOption(runCtx, "model", model); err == nil && thought != "" {
				_, err = client.SetConfigOption(runCtx, "thought_level", thought)
			}
		}
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

// DevinACPModelAndThought maps the operator-facing Devin model selection onto
// the model and thought_level options the ACP session actually advertises.
// Dispatch and the conformance live canary share this mapping so both
// negotiate identically: swe-2-max is the profile-level ID, while the
// provider exposes it as swe-2-high at max thinking.
func DevinACPModelAndThought(session acp.Session, model, effort string) (string, string) {
	if model == "swe-2-max" && effort == "max" {
		for _, option := range session.ConfigOptions {
			if option.ID == "model" {
				for _, value := range option.Options {
					if value.Value == "swe-2-high" {
						return "swe-2-high", "max"
					}
				}
			}
		}
	}
	return model, ""
}
