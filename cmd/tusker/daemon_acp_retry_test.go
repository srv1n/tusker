package main

import "testing"

func TestACPDeliveryUnknownRetryFailureIsTerminal(t *testing.T) {
	classification := classifyRetryFailure("acp_v1 delivery_unknown; no automatic retry or resume")
	if classification.retryable {
		t.Fatal("delivery_unknown must never be automatically retried")
	}
	if classification.outcome != AttemptOutcomeBlocked {
		t.Fatalf("delivery_unknown outcome=%s, want blocked", classification.outcome)
	}
}
