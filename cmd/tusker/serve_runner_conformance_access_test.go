package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	runnercore "tusker/internal/runner"
)

func TestServeRunnerConformanceSetupResolvesDraftAccessWithoutModel(t *testing.T) {
	server := newServeFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("project fixture: %#v %v", projects, err)
	}
	project := projects[0]
	bin := t.TempDir()
	invocations := filepath.Join(t.TempDir(), "invocations")
	codex := filepath.Join(bin, "codex")
	if err := os.WriteFile(codex, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" >> "+invocations+"\nif [ \"$1\" = --version ]; then echo codex-fixture; exit 0; fi\nif [ \"$1\" = login ]; then echo Logged; exit 0; fi\nprintf '%s\\n' '{\"type\":\"turn.completed\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	ref := filepath.Join(t.TempDir(), "reference")
	private := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(ref, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	access := runnercore.AgentAccessV1{
		Schema:             agentAccessSchemaV1,
		Mode:               accessModeProjects,
		Network:            false,
		DestructiveActions: "deny",
		Folders:            []runnercore.AccessFolder{{Path: ref, Access: "read"}},
		PrivateFolders:     []string{private},
	}
	canonicalRef, _ := filepath.EvalSymlinks(ref)
	canonicalPrivate, _ := filepath.EvalSymlinks(private)
	req := httptest.NewRequest(http.MethodPost, "/api/runner/conformance", nil)
	body := serveActionBody{
		"projectId": project.ProjectID,
		"harness":   "codex_exec",
		"preset":    string(runnercore.PresetWorkspaceOffline),
		"draft":     true,
		"draftId":   "draft-access",
		"model":     "gpt-fixture",
		"effort":    "medium",
		"setup":     true,
		"access":    access,
	}
	rec := httptest.NewRecorder()
	server.handleRunnerConformance(rec, req, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%s", rec.Code, rec.Body.String())
	}
	var report runnercore.ConformanceReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Live || report.Ready || report.Access == nil {
		t.Fatalf("setup report=%#v", report)
	}
	requested, ok := report.Access.Requested.(map[string]any)
	if !ok || requested["network"] != false || requested["destructive_actions"] != "deny" {
		t.Fatalf("draft access was not preserved: %#v", report.Access.Requested)
	}
	if len(report.Access.References) != 1 || report.Access.References[0] != canonicalRef || len(report.Access.PrivateFolders) != 1 || report.Access.PrivateFolders[0] != canonicalPrivate {
		t.Fatalf("resolved paths were not preserved: %#v", report.Access)
	}
	if report.Access.Effective.Network || report.Access.Effective.Approvals != "deny" {
		t.Fatalf("effective draft policy was not resolved: %#v", report.Access.Effective)
	}
	if raw, err := os.ReadFile(invocations); err == nil {
		t.Fatalf("setup invoked the runtime: %q", raw)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
