package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestServeModelLevelsTaskRouteAuthoringRoundTrip(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var tasks []serveTaskCapsule
	serveDecode(t, server, "/api/tasks?project="+project.ProjectID, &tasks)
	var before serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+tasks[0].ID+"?project="+project.ProjectID, &before)

	body := `{"projectId":"` + project.ProjectID + `","revision":"` + before.StateRevision + `","workLevel":"demanding","reviewLevel":"light"}`
	updated := servePostJSON(t, server, "/api/tasks/"+tasks[0].ID+"/route", body)
	if updated.Code != http.StatusOK || !strings.Contains(updated.Body.String(), `"ok":true`) {
		t.Fatalf("route update=%d %s", updated.Code, updated.Body.String())
	}
	var after serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+tasks[0].ID+"?project="+project.ProjectID, &after)
	if after.AuthoredWorkLevel != "demanding" || after.AuthoredReviewLevel != "light" || after.EffectiveReview.WorkLevel != "light" {
		t.Fatalf("route round trip = %#v", after)
	}
	stale := servePostJSON(t, server, "/api/tasks/"+tasks[0].ID+"/route", body)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale route update=%d %s", stale.Code, stale.Body.String())
	}
}

func TestServeModelLevelsProfileLifecycleContract(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var initial modelLevelsReport
	serveDecode(t, server, "/api/models?project="+project.ProjectID, &initial)
	body := `{"action":"profile-set","scope":"project","name":"temporary","harness":"codex_exec","model":"gpt-manual","effort":"high","preset":"workspace-write-offline","revision":"` + initial.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec := servePostJSON(t, server, "/api/models", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	var created modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	disable := `{"action":"profile-disable","scope":"project","name":"temporary","revision":"` + created.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", disable)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"temporary":"disabled"`) {
		t.Fatalf("disable=%d %s", rec.Code, rec.Body.String())
	}
	var disabled modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &disabled); err != nil {
		t.Fatal(err)
	}
	remove := `{"action":"profile-remove","scope":"project","name":"temporary","revision":"` + disabled.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", remove)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"temporary"`) {
		t.Fatalf("remove=%d %s", rec.Code, rec.Body.String())
	}
	stale := servePostJSON(t, server, "/api/models", remove)
	if stale.Code != http.StatusUnprocessableEntity || !strings.Contains(stale.Body.String(), "refresh") {
		t.Fatalf("stale remove=%d %s", stale.Code, stale.Body.String())
	}
}

func TestServeModelLevelsGlobalUnusedProfileCanBeRemoved(t *testing.T) {
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var initial modelLevelsReport
	serveDecode(t, server, "/api/models?scope=global&project="+project.ProjectID, &initial)
	removeSeeded := `{"action":"profile-remove","scope":"global","name":"review-frontier","revision":"` + initial.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec := servePostJSON(t, server, "/api/models", removeSeeded)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"review-frontier"`) {
		t.Fatalf("seeded global remove=%d %s", rec.Code, rec.Body.String())
	}
	var afterSeededRemove modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &afterSeededRemove); err != nil || len(afterSeededRemove.Levels[0].Review.Profiles) != 0 {
		t.Fatalf("seeded global cleanup=%#v err=%v", afterSeededRemove, err)
	}
	create := `{"action":"profile-set","scope":"global","name":"global-unused","harness":"codex_exec","model":"gpt-manual","effort":"high","preset":"workspace-write-offline","revision":"` + afterSeededRemove.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", create)
	if rec.Code != http.StatusOK {
		t.Fatalf("global create=%d %s", rec.Code, rec.Body.String())
	}
	var created modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	remove := `{"action":"profile-remove","scope":"global","name":"global-unused","revision":"` + created.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", remove)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"global-unused"`) {
		t.Fatalf("global remove=%d %s", rec.Code, rec.Body.String())
	}
	var afterRemove modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &afterRemove); err != nil {
		t.Fatal(err)
	}
	referencedCreate := `{"action":"profile-set","scope":"global","name":"global-referenced","harness":"codex_exec","model":"gpt-manual","effort":"high","preset":"workspace-write-offline","eligibleTiers":["light"],"revision":"` + afterRemove.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", referencedCreate)
	if rec.Code != http.StatusOK {
		t.Fatalf("referenced create=%d %s", rec.Code, rec.Body.String())
	}
	var referenced modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &referenced); err != nil {
		t.Fatal(err)
	}
	set := `{"action":"set","scope":"global","level":"light","lane":"execute","profiles":["global-referenced"],"revision":"` + referenced.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", set)
	if rec.Code != http.StatusOK {
		t.Fatalf("global assignment=%d %s", rec.Code, rec.Body.String())
	}
	var assigned modelLevelsReport
	if err := json.Unmarshal(rec.Body.Bytes(), &assigned); err != nil {
		t.Fatal(err)
	}
	removeReferenced := `{"action":"profile-remove","scope":"global","name":"global-referenced","revision":"` + assigned.Revision + `","projectId":"` + project.ProjectID + `"}`
	rec = servePostJSON(t, server, "/api/models", removeReferenced)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"global-referenced"`) {
		t.Fatalf("referenced global remove=%d %s", rec.Code, rec.Body.String())
	}
}

func servePostJSON(t *testing.T, server http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}
