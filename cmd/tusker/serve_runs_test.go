package main

import (
	"encoding/json"
	"testing"
)

// The runtime EventLog writes the timestamp under "at" (RFC3339) and nests the
// human fields under "payload". Reading only "ts"/"timestamp" left TS empty,
// which the UI rendered as NaN:NaN:NaN (SRV-T-0015 A1).
func TestServeRunEventFromPayloadReadsAtAndNestedFields(t *testing.T) {
	var payload map[string]any
	line := `{"seq":3,"at":"2026-07-08T18:22:05Z","attempt_id":"a1","runner":"codex","kind":"thread_started","payload":{"text":"first turn started","level":"info"}}`
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatal(err)
	}
	ev := serveRunEventFromPayload(payload)
	assertEqual(t, "2026-07-08T18:22:05Z", ev.TS, "timestamp from at")
	assertEqual(t, "thread_started", ev.Kind, "kind")
	assertEqual(t, "first turn started", ev.Text, "text from nested payload")
	assertEqual(t, "info", ev.Level, "level from nested payload")
}

func TestServeRunEventFromPayloadFallsBackToLegacyKeys(t *testing.T) {
	var payload map[string]any
	line := `{"ts":"2026-07-08T01:02:03Z","event":"tool_call","message":"ran build"}`
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		t.Fatal(err)
	}
	ev := serveRunEventFromPayload(payload)
	assertEqual(t, "2026-07-08T01:02:03Z", ev.TS, "legacy ts key")
	assertEqual(t, "tool_call", ev.Kind, "legacy event key")
	assertEqual(t, "ran build", ev.Text, "legacy message key")
}

// A redrive on a review/done task is meaningless: the daemon would refuse and
// silently retire the run. serveRedriveRefusal surfaces that synchronously
// (SRV-T-0016 A2/A3) instead of requeuing into a silent retire.
func TestServeRedriveRefusalGuardsCanonicalStatus(t *testing.T) {
	idle := RunStatus{ItemID: "SRV-T-0016", LeaseState: string(LeaseStateReleased)}

	refused, reason := serveRedriveRefusal("review", idle)
	if !refused || reason == "" {
		t.Fatalf("review task must refuse with a reason, got refused=%v reason=%q", refused, reason)
	}

	refused, reason = serveRedriveRefusal("done", idle)
	if !refused || reason == "" {
		t.Fatalf("done task must refuse with a reason, got refused=%v reason=%q", refused, reason)
	}

	if refused, _ := serveRedriveRefusal("ready", idle); refused {
		t.Fatal("a ready task with no live process must be redrivable, not refused")
	}
	if refused, _ := serveRedriveRefusal("rework", idle); refused {
		t.Fatal("a rework task with no live process must be redrivable, not refused")
	}
}
