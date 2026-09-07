package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeModelLevelsReadWriteConflictAndCLIAgreement(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("project fixture: %#v %v", projects, err)
	}
	project := projects[0]
	var initial modelLevelsReport
	serveDecode(t, server, "/api/models?project="+project.ProjectID, &initial)
	cli, err := modelLevelsRead(project.VaultRoot)
	if err != nil || initial.Revision != cli.Revision || len(initial.Profiles) != len(cli.Profiles) {
		t.Fatalf("CLI/API mismatch: %#v %#v %v", initial, cli, err)
	}
	var tasks []serveTaskCapsule
	serveDecode(t, server, "/api/tasks?project="+project.ProjectID, &tasks)
	if len(tasks) == 0 {
		t.Fatal("task fixture is empty")
	}
	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+tasks[0].ID+"?project="+project.ProjectID, &detail)
	if detail.EffectiveExecute.Schema == "" || detail.EffectiveReview.Schema == "" || detail.EffectiveExecute.WorkLevel == "" {
		t.Fatalf("task routes unavailable: %#v %#v", detail.EffectiveExecute, detail.EffectiveReview)
	}

	body := `{"action":"set","scope":"project","level":"standard","lane":"execute","profiles":["execute-cheap"],"revision":"` + initial.Revision + `","projectId":"` + project.ProjectID + `"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/models", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil || updated.Revision == initial.Revision || !updated.Levels[1].Execute.Overridden {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	profileBody := `{"action":"profile-set","scope":"project","name":"manual-test","harness":"codex_exec","model":"gpt-manual","effort":"high","preset":"workspace-write-offline","revision":"` + updated.Revision + `","projectId":"` + project.ProjectID + `"}`
	profileReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/models", strings.NewReader(profileBody))
	profileReq.Header.Set("Content-Type", "application/json")
	profileRec := httptest.NewRecorder()
	server.ServeHTTP(profileRec, profileReq)
	if profileRec.Code != http.StatusOK || !strings.Contains(profileRec.Body.String(), `"manual-test"`) || !strings.Contains(profileRec.Body.String(), `"configured_unverified"`) {
		t.Fatalf("profile-set status=%d body=%s", profileRec.Code, profileRec.Body.String())
	}

	stale := httptest.NewRecorder()
	staleReq := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/models", strings.NewReader(body))
	staleReq.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(stale, staleReq)
	if stale.Code != http.StatusUnprocessableEntity || !strings.Contains(stale.Body.String(), "refresh") {
		t.Fatalf("stale write status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestServeModelLevelsRunIdentityIsActualOrUnavailable(t *testing.T) {
	run := RunStatus{ProjectID: "app", RecordID: "APP-T-0001", ItemID: "APP-T-0001", RunnerProfile: "fallback", RunnerHarness: "codex_exec", RunnerModel: "gpt-actual", RunnerEffort: "high", RunnerFallbackReason: "primary runtime missing"}
	raw, err := json.Marshal(run)
	if err != nil || !strings.Contains(string(raw), `"runner_model":"gpt-actual"`) || !strings.Contains(string(raw), `"runner_fallback_reason":"primary runtime missing"`) {
		t.Fatalf("actual identity missing: %s %v", raw, err)
	}
	store, err := OpenRuntimeStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertRun(run); err != nil {
		t.Fatal(err)
	}
	runs, err := store.ListRuns()
	if err != nil || len(runs) != 1 || runs[0].RunnerFallbackReason != run.RunnerFallbackReason {
		t.Fatalf("persisted fallback identity = %#v err=%v", runs, err)
	}
	empty, _ := json.Marshal(RunStatus{})
	if strings.Contains(string(empty), "assumed") {
		t.Fatalf("missing identity was fabricated: %s", empty)
	}
}
