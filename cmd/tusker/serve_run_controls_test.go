package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServeRunControlsParity(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", Runner: string(RunnerCodexExec), Lane: runLaneExecute, LeaseState: string(LeaseStateInterrupted), AttemptOutcome: string(AttemptOutcomeCancelled), LeaseGeneration: 1}
	if err := server.store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action string
		reason string
	}{
		{"continue", "Continue is available for Blocked, Failed, or Lost runs"},
		{"recover_context", "context recovery requires an unknown terminal outcome"},
		{"pause", runSessionControlPauseReason(run)},
		{"reconnect", "no live owner is available to reconnect"},
	} {
		got := server.runActionCapability(tc.action, RegisteredProject{}, Note{}, run, nil)
		if got.Available || got.Reason != tc.reason {
			t.Fatalf("%s: %#v", tc.action, got)
		}
	}
	var detail serveRunDetail
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if len(detail.Controls.Capabilities) != 8 {
		t.Fatalf("capabilities = %#v", detail.Controls.Capabilities)
	}
	for _, want := range []string{"say", "answer", "reconnect", "continue", "recover_context", "pause", "stop", "start_fresh"} {
		found := false
		for _, got := range detail.Controls.Capabilities {
			if got.Action == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing %s", want)
		}
	}
	intent := runSessionControlIntent{Action: runSessionControlStop, State: runSessionControlPending, ProjectID: "app", RecordID: run.RecordID, LeaseGeneration: 1}
	if err := saveRunSessionControlIntent(server.store, intent); err != nil {
		t.Fatal(err)
	}
	detail = serveRunDetail{}
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if detail.Controls.Pending == nil || detail.Controls.Pending.Action != runSessionControlStop {
		t.Fatalf("pending lost: %#v", detail.Controls.Pending)
	}
	intent.State = runSessionControlSettledState
	if err := saveRunSessionControlIntent(server.store, intent); err != nil {
		t.Fatal(err)
	}
	detail = serveRunDetail{}
	serveDecode(t, server, "/api/runs/APP-T-0001?project=app", &detail)
	if detail.Controls.Pending != nil {
		t.Fatalf("settled intent still pending: %#v", detail.Controls.Pending)
	}
}

func TestServeRunActivityFreshnessAndOrder(t *testing.T) {
	dir := t.TempDir()
	eventsPath := filepath.Join(dir, "events.jsonl")
	logPath := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(eventsPath, []byte(`{"at":"2026-09-23T10:00:05Z","kind":"agent_message","payload":{"text":"later","activity":true}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(`{"timestamp":"2026-09-23T10:00:03Z","type":"item.completed","item":{"type":"agent_message","text":"earlier"}}`+"\n"+`{"type":"item.completed","item":{"type":"agent_message","text":"untimed"}}`+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run := RunStatus{EventSinkPath: eventsPath, RawLogPath: logPath}
	for i := 0; i < 2; i++ {
		got := serveRunEvents(run, nil)
		if len(got) != 3 || got[0].Text != "earlier" || got[1].Text != "untimed" || got[1].TS != "" || got[2].Text != "later" {
			t.Fatalf("order %d: %#v", i, got)
		}
	}
	now := time.Date(2026, 9, 23, 10, 0, 10, 0, time.UTC)
	fresh := serveRunActivity(run, nil, now)
	if fresh.CaptureState != "available" || fresh.MessageAt != "2026-09-23T10:00:05Z" || fresh.MessageAgeSec != 5 || fresh.HeartbeatAt != nil {
		t.Fatalf("freshness = %#v", fresh)
	}
	missing := serveRunActivity(RunStatus{}, nil, now)
	if missing.CaptureState != "missing" || missing.MessageAt != nil {
		t.Fatalf("missing = %#v", missing)
	}
	// The display tail drops the older message, but freshness still sees it.
	burst := `{"at":"2026-09-23T10:00:05Z","kind":"agent_message","payload":{"text":"last message","activity":true}}` + "\n"
	burst += strings.Repeat(`{"at":"2026-09-23T10:00:06Z","kind":"tool_call","payload":{"text":"tool","activity":true}}`+"\n", 51)
	if err := os.WriteFile(eventsPath, []byte(burst), 0600); err != nil {
		t.Fatal(err)
	}
	burstRun := RunStatus{EventSinkPath: eventsPath}
	if len(serveRunEvents(burstRun, nil)) != 50 {
		t.Fatal("display tail did not trim activity")
	}
	burstFresh := serveRunActivity(burstRun, nil, now)
	if burstFresh.MessageAt != "2026-09-23T10:00:05Z" || burstFresh.MessageAgeSec != 5 {
		t.Fatalf("trimmed message lost freshness: %#v", burstFresh)
	}
}
