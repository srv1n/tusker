package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAcknowledgeRetiresFailedRun proves the serve acknowledge action retires a
// settled failed run through the same path as `tusker runs retire`, and that the
// cleared run then drops out of both the needs and runs payloads and stays gone.
func TestAcknowledgeRetiresFailedRun(t *testing.T) {
	server := newServeFixture(t)

	// APP-T-0007 is the seeded terminal failure that surfaces as a failed need.
	var needs []map[string]any
	serveDecode(t, server, "/api/needs?project=app", &needs)
	if !needsContains(needs, "need-failed-APP-T-0007") {
		t.Fatalf("expected need-failed-APP-T-0007 before acknowledge, got %#v", needs)
	}

	var result serveActionResult
	servePost(t, server, "/api/runs/APP-T-0007/acknowledge?project=app", `{}`, &result)
	if !result.OK || result.Refused {
		t.Fatalf("expected acknowledge to succeed, got %#v", result)
	}

	// The runtime row is retired via retireRuntimeRun: terminal, lease released,
	// and stamped with the shared "retired by ..." reason.
	stored, err := server.store.FindRun("APP-T-0007")
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil {
		t.Fatal("expected APP-T-0007 run to remain in the store after retirement")
	}
	if !stored.Terminal {
		t.Fatalf("expected retired run to be terminal, got %#v", stored)
	}
	if LeaseState(stored.LeaseState) != LeaseStateReleased {
		t.Fatalf("expected retired run lease released, got %q", stored.LeaseState)
	}
	if !strings.HasPrefix(stored.LastError, "retired by ") {
		t.Fatalf("expected retirement reason on last_error, got %q", stored.LastError)
	}
	if !strings.Contains(stored.LastError, serveAcknowledgeReason) {
		t.Fatalf("expected acknowledge reason recorded, got %q", stored.LastError)
	}

	// Rebuild the projection so the cached payloads reflect the retirement.
	if _, err := server.loadFreshSnapshotForProject("app"); err != nil {
		t.Fatal(err)
	}

	serveDecode(t, server, "/api/needs?project=app", &needs)
	if needsContains(needs, "need-failed-APP-T-0007") {
		t.Fatalf("expected acknowledged failure to leave the needs payload, got %#v", needs)
	}

	var runs []map[string]any
	serveDecode(t, server, "/api/runs?project=app", &runs)
	for _, run := range runs {
		if run["taskId"] == "APP-T-0007" {
			t.Fatalf("expected acknowledged run to leave the runs payload, got %#v", runs)
		}
	}
}

// TestAcknowledgeRefusesActiveRun proves acknowledge refuses a still-leased run
// with a conflict status and leaves the runtime row untouched.
func TestAcknowledgeRefusesActiveRun(t *testing.T) {
	server := newServeFixture(t)

	before, err := server.store.FindRun("APP-T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if before == nil {
		t.Fatal("expected seeded active run APP-T-0003")
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/runs/APP-T-0003/acknowledge?project=app", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for an active run, got %d: %s", rec.Code, rec.Body.String())
	}

	after, err := server.store.FindRun("APP-T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if after == nil {
		t.Fatal("expected APP-T-0003 to remain after a refused acknowledge")
	}
	if after.Terminal {
		t.Fatalf("expected refused acknowledge to leave the run non-terminal, got %#v", after)
	}
	if after.LeaseState != before.LeaseState {
		t.Fatalf("expected lease state untouched, got %q want %q", after.LeaseState, before.LeaseState)
	}
	if strings.HasPrefix(after.LastError, "retired by ") {
		t.Fatalf("expected no retirement stamp on a refused run, got %q", after.LastError)
	}
}

func needsContains(needs []map[string]any, id string) bool {
	for _, need := range needs {
		if need["id"] == id {
			return true
		}
	}
	return false
}
