package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoSessionPermissionFakeServe(t *testing.T) {
	repo := t.TempDir()
	if err := demoSaveManifest(repo, &demoManifest{Schema: demoManifestSchema, Scenario: demoScenario, RepoRoot: repo}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(demoVaultPath(repo), "config.local.yaml")
	original := []byte("automation:\n  profiles:\n    execute-fast:\n      harness: codex_exec\n      permission_preset: workspace-write-offline\n      sandbox:\n        mode: workspace-write\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	phase := "initial"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config, err := os.ReadFile(path)
		if err != nil {
			t.Error(err)
		}
		denied := strings.Contains(string(config), "permission_preset: read-only")
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/capability":
			json.NewEncoder(w).Encode(map[string]string{"capability": "test"})
		case r.Method == "POST" && r.URL.Path == "/api/actions/projects/demo/waves/standalone/start":
			if !denied {
				t.Error("wave started before denial was installed")
			}
			phase = "blocked"
			json.NewEncoder(w).Encode(map[string]string{"authorization": "authorized"})
		case r.Method == "GET" && r.URL.Path == "/api/runs/task":
			state := phase
			detail := map[string]any{"operatorState": map[string]any{"state": state, "reason": map[string]string{"code": string(RunFailurePermissionDenied), "class": "blocked"}}, "session": map[string]string{"session_ref": "native-1"}, "attempts": []map[string]string{{"id": "attempt-1"}}}
			json.NewEncoder(w).Encode(detail)
		case r.Method == "POST" && r.URL.Path == "/api/runs/task/continue":
			if denied {
				t.Error("profile was not restored before Continue")
			}
			phase = "working"
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	old := demoServeBaseURL
	demoServeBaseURL = server.URL
	defer func() { demoServeBaseURL = old }()
	proof := demoSessionProof{Scenario: "permission-deny", Harness: "codex_exec", Label: "live-provider", AttemptIDs: []string{}, Observations: []demoSessionObservation{}}
	got := demoRunSessionPermissionWithProfile(context.Background(), repo, "demo", "task", "standalone", "execute-fast", "test", proof)
	if got.Status != "passed" || got.NativeBefore != "native-1" || got.NativeAfter != "native-1" {
		t.Fatalf("permission proof: %#v", got)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatalf("config not restored: %v", err)
	}
}

func TestDemoSessionPermissionProfileRestored(t *testing.T) {
	repo := t.TempDir()
	if err := demoSaveManifest(repo, &demoManifest{Schema: demoManifestSchema, Scenario: demoScenario, RepoRoot: repo}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(demoVaultPath(repo), "config.local.yaml")
	original := []byte("automation:\n  profiles:\n    execute-fast:\n      harness: codex_exec\n      permission_preset: workspace-write-offline\n      sandbox:\n        mode: workspace-write\n        network: false\n    other:\n      harness: codex_exec\n      permission_preset: workspace-write-offline\n      sandbox:\n        mode: workspace-write\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err := demoWithDeniedProfile(repo, "execute-fast", func() demoSessionProof {
		called = true
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !strings.Contains(string(current), "permission_preset: read-only") || !strings.Contains(string(current), "mode: read-only") || !strings.Contains(string(current), "other:") {
			t.Fatalf("denied profile was not scoped: %s", current)
		}
		return demoSessionProof{}
	})
	if err != nil || !called {
		t.Fatalf("permission callback: called=%v err=%v", called, err)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, original) {
		t.Fatalf("config not restored exactly: %v", err)
	}
	if _, err := demoWithDeniedProfile(repo, "missing", func() demoSessionProof { t.Fatal("unexpected callback"); return demoSessionProof{} }); err == nil {
		t.Fatal("missing profile accepted")
	}
}

func TestDemoSessionTypedPermissionDenial(t *testing.T) {
	for _, code := range []RunFailureReasonCode{RunFailurePermissionDenied, RunFailureSandboxDenied} {
		detail := serveRunDetail{serveRunSummary: serveRunSummary{OperatorState: runOperatorState{State: "blocked", Reason: &runOperatorReason{Code: string(code), Class: "blocked"}}}}
		if err := demoSessionTypedPermissionDenial(detail); err != nil {
			t.Fatal(err)
		}
	}
	for _, detail := range []serveRunDetail{
		{serveRunSummary: serveRunSummary{OperatorState: runOperatorState{State: "working"}}},
		{serveRunSummary: serveRunSummary{OperatorState: runOperatorState{State: "blocked", Reason: &runOperatorReason{Code: string(RunFailureAuthExpired)}}}},
	} {
		if err := demoSessionTypedPermissionDenial(detail); err == nil {
			t.Fatalf("accepted non-permission state: %+v", detail.OperatorState)
		}
	}
}
