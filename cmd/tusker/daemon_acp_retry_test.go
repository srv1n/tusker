package main

import "testing"

func TestACPDeliveryUnknownRetryFailureIsTerminal(t *testing.T) {
	classification := classifyRetryFailure("acp_v1 delivery_unknown; no automatic retry or resume")
	if classification.retryable {
		t.Fatal("delivery_unknown must never be automatically retried")
	}
	if classification.outcome != AttemptOutcomeUnknown {
		t.Fatalf("delivery_unknown outcome=%s, want outcome_unknown", classification.outcome)
	}
	if state := sessionStateForOutcome(AttemptOutcomeUnknown); state != "closed" {
		t.Fatalf("delivery_unknown session state=%s, want closed", state)
	}
}

func TestProjectedAttemptOutcomeRecognizesLegacyWriteComplete(t *testing.T) {
	legacy := RunStatus{LeaseState: string(LeaseStateReleased), AttemptOutcome: string(AttemptOutcomeFailed), LastError: `acp outcome delivery_unknown (write_complete): connection lost`, Terminal: true}
	got := projectedAttemptOutcome(legacy.AttemptOutcome, legacy.LastError)
	if got != AttemptOutcomeUnknown {
		t.Fatalf("legacy outcome=%s, want outcome_unknown", got)
	}
	if stage := realWorkStage(&legacy, "ready"); stage != "outcome_unknown" {
		t.Fatalf("legacy stage=%s, want outcome_unknown", stage)
	}
	if !serveTerminalFailure(legacy, 3) || !digestRunIsRedOrParked(legacy) {
		t.Fatal("legacy uncertain run must remain visible on attention surfaces")
	}
	if got := projectedAttemptOutcome(string(AttemptOutcomeFailed), "ordinary implementation failure"); got != AttemptOutcomeFailed {
		t.Fatalf("explicit failure changed to %s", got)
	}
	if got := projectedAttemptOutcome(string(AttemptOutcomeBlocked), "attempt cap reached (3): acp_v1 child exited without terminal status"); got != AttemptOutcomeUnknown {
		t.Fatalf("missing ACP terminal status changed to %s", got)
	}
}
