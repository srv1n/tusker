package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// runSaySoft returns fallback=true only when the wrapper cannot accept a
// request. A write with no echo is uncertain and must never be sent again.
func runSaySoft(store *RuntimeStore, run RunStatus, actor, body, key string, now time.Time) (runSayResult, bool, error) {
	result := runSayResult{Route: "soft"}
	prior, err := runSayPriorDelivery(store, run, actor, body, key)
	if err != nil {
		return result, false, err
	}
	state, err := runOperatorStateForRun(store, run, now)
	if err != nil {
		return result, false, err
	}
	result.OperatorState = state
	if strings.TrimSpace(run.SessionRef) == "" {
		return result, false, tuskerError(errorInvalidTransition, "runs say requires a current native session identity")
	}
	if prior != nil {
		result.Delivery, result.Duplicate = *prior, true
		return result, softSayNeedsHardFallback(*prior), nil
	}
	if state.State != "working" && state.State != "quiet" {
		return result, false, tuskerError(errorInvalidTransition, "soft Say requires a working or quiet run")
	}
	identity, err := runSayWorkerIdentity(store, run)
	if err != nil {
		return result, false, err
	}
	if identity == nil {
		return result, false, tuskerError(errorInvalidTransition, "runs say requires a current native session identity")
	}
	if key == "" {
		key = runSayIdempotencyKey(actor, body, *identity)
	}
	delivery, duplicate, err := store.PutWorkerDelivery(WorkerDelivery{Identity: *identity, Kind: "instruction", Body: body, IdempotencyKey: key})
	if err != nil {
		return result, false, err
	}
	result.Delivery, result.Duplicate = delivery, duplicate
	if duplicate {
		return result, softSayNeedsHardFallback(delivery), nil
	}
	current, err := store.FindRunScoped(run.ProjectID, run.RecordID)
	if err != nil {
		return result, false, err
	}
	if current == nil || current.ActiveAttemptID != identity.AttemptID || current.LeaseGeneration != identity.AttemptGeneration {
		_ = store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"stored": true}, "stale", "attempt changed before soft Say")
		return result, false, tuskerError(errorInvalidTransition, "attempt changed before soft Say")
	}
	claimed, won, err := store.ClaimWorkerDelivery(delivery.DeliveryID)
	if err != nil {
		return result, false, err
	}
	result.Delivery = claimed
	if !won {
		return result, false, nil
	}
	response, err := sendClaudeWrapperControl(context.Background(), run.StatusPath, claudeControlRequest{
		AttemptID: identity.AttemptID, LeaseGeneration: identity.AttemptGeneration,
		DeliveryID: delivery.DeliveryID, Op: "say", Body: body,
	})
	if err != nil || (response.Error != "" && !response.Uncertain) {
		why := "Claude control channel unavailable"
		if err != nil {
			why += ": " + err.Error()
		} else {
			why += ": " + response.Error
		}
		if stateErr := store.markWorkerDeliveryState(delivery.DeliveryID, map[string]bool{"delivering": true}, "stored", why); stateErr != nil {
			return result, false, stateErr
		}
		result.Delivery.State = "stored"
		return result, true, nil
	}
	if response.Uncertain || response.Receipt == "" {
		why := response.Error
		if why == "" {
			why = "Claude did not echo the message"
		}
		if err := store.MarkWorkerDeliveryUncertain(delivery.DeliveryID, why); err != nil {
			return result, false, err
		}
		result.Delivery.State = "uncertain"
		return result, false, fmt.Errorf("soft Say delivery uncertain: %s", why)
	}
	if err := store.MarkWorkerDeliveryAccepted(delivery.DeliveryID, map[string]any{"echo_uuid": response.Receipt, "route": "soft"}, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return result, false, err
	}
	result.Delivery.State = "accepted"
	result.Delivery.ProviderReceipt = map[string]any{"echo_uuid": response.Receipt, "route": "soft"}
	return result, false, nil
}

func softSayNeedsHardFallback(delivery WorkerDelivery) bool {
	return delivery.State == "stored" && strings.HasPrefix(delivery.LastError, "Claude control channel unavailable")
}
