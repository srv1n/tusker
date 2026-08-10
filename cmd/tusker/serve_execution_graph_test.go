package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeExecutionCancelRequiresRegisteredProjectMatch(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	addServeProjectFixture(t, server, "backend", "BCK-T-0001", "Backend task")
	execution, err := server.store.CreateDirectExecution(DirectExecutionInput{ProjectID: "backend", Source: "direct_codex", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}

	missingProject := httptest.NewRecorder()
	server.handleExecutionCancel(missingProject, httptest.NewRequest(http.MethodPost, "/api/executions/"+execution.ExecutionID+"/cancel", nil), execution.ExecutionID)
	if missingProject.Code != http.StatusBadRequest {
		t.Fatalf("cancel without project status=%d body=%s, want bad request", missingProject.Code, missingProject.Body.String())
	}

	for _, path := range []string{
		"/api/executions/" + execution.ExecutionID + "/cancel?project=app",
		"/api/executions/" + execution.ExecutionID + "/cancel?project=missing",
	} {
		recorder := httptest.NewRecorder()
		server.handleExecutionCancel(recorder, httptest.NewRequest(http.MethodPost, path, nil), execution.ExecutionID)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("cancel %q status=%d body=%s, want not found", path, recorder.Code, recorder.Body.String())
		}
	}
	var evidence int
	if err := server.store.queryRowScan(`SELECT COUNT(*) FROM execution_cancellation_evidence WHERE execution_id=?`, []any{execution.ExecutionID}, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence != 0 {
		t.Fatalf("wrong-project cancellation recorded control evidence: %d", evidence)
	}

	recorder := httptest.NewRecorder()
	server.handleExecutionCancel(recorder, httptest.NewRequest(http.MethodPost, "/api/executions/"+execution.ExecutionID+"/cancel?project=backend", nil), execution.ExecutionID)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"ok":false`) {
		t.Fatalf("matched registered project did not reach cancellation boundary: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}
