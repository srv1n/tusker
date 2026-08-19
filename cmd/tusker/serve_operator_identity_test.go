package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeDurableMutationsRequireConfiguredOperator(t *testing.T) {
	server := newServeFixture(t)
	server.operatorActor = ""
	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "close", path: "/api/tasks/APP-T-0001/close?project=app", body: `{}`},
		{name: "land", path: "/api/tasks/APP-T-0001/land?project=app", body: `{}`},
		{name: "status", path: "/api/tasks/APP-T-0001/status?project=app", body: `{"status":"rework"}`},
		{name: "discard", path: "/api/tasks/APP-T-0005/discard?project=app", body: `{"reason":"no longer needed","dependents":"detach"}`},
		{name: "gate", path: "/api/gates/APP-G-0001/satisfy?project=app", body: `{"evidence":"checked"}`},
		{name: "evidence", path: "/api/evidence?project=app", body: `{"id":"APP-T-0001","kind":"automated_test","covers":"A1"}`},
		{name: "redrive", path: "/api/runs/APP-T-0008/redrive?project=app", body: `{}`},
		{name: "acknowledge", path: "/api/runs/APP-T-0007/acknowledge?project=app", body: `{}`},
		{name: "docgraph", path: "/api/docgraph/doc?project=app&subject=alpha", body: `{"base_rev":"sha256:missing","body":"edited"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seedDocgraphCorpus(t, server.repoRoot)
			status, payload := serveMutationRaw(t, server, http.MethodPost, tc.path, tc.body)
			if tc.name == "docgraph" {
				status, payload = serveMutationRaw(t, server, http.MethodPut, tc.path, tc.body)
			}
			if status != http.StatusPreconditionFailed {
				t.Fatalf("status=%d body=%s", status, payload)
			}
			if !strings.Contains(payload, "SERVE_OPERATOR_REQUIRED") {
				t.Fatalf("missing operator refusal code: %s", payload)
			}
		})
	}
}

func TestServeDurableMutationsRejectForgedOperator(t *testing.T) {
	server := newServeFixture(t)
	cases := []struct {
		name string
		path string
		body string
	}{
		{name: "close", path: "/api/tasks/APP-T-0001/close?project=app", body: `{"actor":"human:forged"}`},
		{name: "land", path: "/api/tasks/APP-T-0001/land?project=app", body: `{"actor":"human:forged"}`},
		{name: "status", path: "/api/tasks/APP-T-0001/status?project=app", body: `{"status":"rework","actor":"human:forged"}`},
		{name: "discard", path: "/api/tasks/APP-T-0005/discard?project=app", body: `{"reason":"no longer needed","dependents":"detach","actor":"human:forged"}`},
		{name: "gate", path: "/api/gates/APP-G-0001/satisfy?project=app", body: `{"actor":"human:forged","evidence":"checked"}`},
		{name: "evidence", path: "/api/evidence?project=app", body: `{"id":"APP-T-0001","actor":"human:forged","kind":"automated_test","covers":"A1"}`},
		{name: "redrive", path: "/api/runs/APP-T-0008/redrive?project=app", body: `{"actor":"human:forged"}`},
		{name: "acknowledge", path: "/api/runs/APP-T-0007/acknowledge?project=app", body: `{"actor":"human:forged"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, payload := serveMutationRaw(t, server, http.MethodPost, tc.path, tc.body)
			if status != http.StatusOK {
				t.Fatalf("status=%d body=%s", status, payload)
			}
			if !strings.Contains(payload, "does not match the configured Serve operator") {
				t.Fatalf("missing forged actor refusal: %s", payload)
			}
		})
	}
}

func serveMutationRaw(t *testing.T, server *serveServer, method, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, "http://127.0.0.1:7420"+path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	var normalized any
	if err := json.Unmarshal(rec.Body.Bytes(), &normalized); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	return rec.Code, string(encoded)
}
