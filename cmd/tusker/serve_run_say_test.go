package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeRunSayRequiresProjectAndCanonicalState(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	var response serveRunSayResponse
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-0001/say", bytes.NewBufferString(`{"message":"hello"}`))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Refused || !strings.Contains(response.Reason, "project") {
		t.Fatalf("missing project: %#v", response)
	}
	response = serveRunSayResponse{}
	req = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-0001/say?project=app", bytes.NewBufferString(`{"message":"hello"}`))
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Refused {
		t.Fatalf("queued/missing run accepted Say: %#v", response)
	}
}

func TestServeSayRouteCapabilityAndState(t *testing.T) {
	working := runOperatorState{State: "working"}
	for _, tc := range []struct {
		runner RunnerName
		mode   string
	}{{RunnerClaude, "soft"}, {RunnerCodexExec, "hard"}, {RunnerMuse, "hard"}, {RunnerDevin, "hard"}} {
		route := serveSayRoute(RunStatus{Runner: string(tc.runner)}, working)
		if !route.Available || route.Mode != tc.mode || route.Note == "" {
			t.Fatalf("%s route: %#v", tc.runner, route)
		}
	}
	route := serveSayRoute(RunStatus{Runner: string(RunnerCodexExec)}, runOperatorState{State: "lost"})
	if route.Available {
		t.Fatalf("lost Say available: %#v", route)
	}
}

func TestServeRunSayReadbackSurvivesReload(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	identity := WorkerAttemptIdentity{ProjectID: "app", TaskID: "APP-T-0001", WorkRevision: 1, AttemptID: "first", AttemptGeneration: 1, Provider: "codex", NativeSessionID: "native-1"}
	delivery, _, err := server.store.PutWorkerDelivery(WorkerDelivery{Identity: identity, Kind: "instruction", Body: "Keep the exact text", IdempotencyKey: "operator-key"})
	if err != nil {
		t.Fatal(err)
	}
	readback, err := server.lastRunSayDelivery(RunStatus{ProjectID: "app", ItemID: "APP-T-0001", WorkRevision: 1})
	if err != nil || readback == nil || readback.ID != delivery.DeliveryID || readback.Body != "Keep the exact text" || readback.State != "stored" {
		t.Fatalf("readback=%#v err=%v", readback, err)
	}
}
