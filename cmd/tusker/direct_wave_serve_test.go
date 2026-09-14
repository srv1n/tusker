package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func directWaveServeFixture(t *testing.T, members []string, extra map[string]any) (vault string, store *RuntimeStore, project RegisteredProject, server *serveServer) {
	t.Helper()
	vault, store, project = autonomousWaveFixture(t, members, extra)
	server = newServeServer(vault, project.RepoRoot, defaultServeAddr, store, nil)
	server.operatorActor = "human:test-operator"
	return vault, store, project, server
}

func TestDirectWaveServeReviewReturnsCanonicalProjection(t *testing.T) {
	vault, _, project, server := directWaveServeFixture(t, []string{"APP-T-0001"}, map[string]any{"outcome": "Ship the frontier split."})
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ProjectID+"/waves/W-0001/review", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("review returned %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"plan", "planPath", "path", "planFingerprint", "planIdentity", "factory"} {
		if _, ok := raw[key]; ok {
			t.Fatalf("direct review leaked plan/factory field %q", key)
		}
	}
	var review directWaveReview
	if err := json.Unmarshal(rec.Body.Bytes(), &review); err != nil {
		t.Fatal(err)
	}
	if review.Schema != "tusker.wave-review/v1" {
		t.Fatalf("unexpected schema %q", review.Schema)
	}
	if review.State != "Planned" || review.Authorization != "inert" {
		t.Fatalf("unstarted wave should be Planned/inert: %#v", review)
	}
	if len(review.Members) != 1 || review.Members[0].TaskID != "APP-T-0001" {
		t.Fatalf("review members wrong: %#v", review.Members)
	}
	var waveStart, taskStart bool
	for _, control := range review.Controls {
		if control.Action == "wave start" && control.Enabled && control.Scope == "W-0001" {
			waveStart = true
		}
		if control.Action == "task start" && control.Enabled && control.Scope == "APP-T-0001" {
			taskStart = true
		}
	}
	if !waveStart || !taskStart {
		t.Fatalf("expected enabled wave/task start controls: %#v", review.Controls)
	}
}

func TestDirectWaveServeStartPauseResumeUseServeOperator(t *testing.T) {
	vault, store, project, server := directWaveServeFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)

	var started directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/start", `{"mode":"background"}`, &started)
	if started.Authorization != "authorized" || started.State != "Waiting" {
		t.Fatalf("serve start did not authorize: %#v", started)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].Actor != "human:test-operator" {
		t.Fatalf("serve start did not record the configured operator actor: %#v", directives)
	}
	idx, err := loadV7Index(vault)
	if err != nil {
		t.Fatal(err)
	}
	if stringField(idx.Waves["W-0001"].Data, "authorized_by") != "human:test-operator" {
		t.Fatalf("wave authorized_by is not the serve operator: %#v", idx.Waves["W-0001"].Data)
	}

	var paused directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/pause", `{}`, &paused)
	if paused.State != "Paused" || paused.Authorization != "paused" {
		t.Fatalf("serve pause did not pause: %#v", paused)
	}

	var resumed directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/resume", `{}`, &resumed)
	if resumed.Authorization != "authorized" {
		t.Fatalf("serve resume did not reauthorize: %#v", resumed)
	}
}

func TestDirectWaveServeTaskStartQueuesPlannedBacklog(t *testing.T) {
	vault, store, project, server := directWaveServeFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)

	var result directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/tasks/APP-T-0001/start", `{"mode":"background"}`, &result)
	if result.Authorization != "authorized" || result.Scope != "task" {
		t.Fatalf("planned backlog task start refused: %#v", result)
	}
	if len(result.QueuedTaskIDs) != 1 || result.QueuedTaskIDs[0] != "APP-T-0001" {
		t.Fatalf("task start queued wrong members: %#v", result.QueuedTaskIDs)
	}
	directives := queuedDirectives(t, store, project.ProjectID)
	if len(directives) != 1 || directives[0].RecordID != "APP-T-0001" || directives[0].Actor != "human:test-operator" {
		t.Fatalf("exact task directive not queued: %#v", directives)
	}
}

func TestDirectWaveServeTaskStartRefusalsCreateNoDirective(t *testing.T) {
	vault, store, project, server := directWaveServeFixture(t, []string{"APP-T-0001", "APP-T-0002", "APP-T-0003", "APP-T-0004", "APP-T-0005", "APP-T-0006"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", map[string]any{"status": "review", "readiness": "ready"})
	writeDirectTask(t, vault, "APP-T-0002", "W-0001", map[string]any{"status": "ready", "readiness": "ready", "work_level": "demanding"})
	writeDirectTask(t, vault, "APP-T-0003", "W-0001", map[string]any{"status": "backlog", "readiness": "held"})
	writeDirectTask(t, vault, "APP-T-0004", "W-0001", map[string]any{"status": "ready", "readiness": "blocked_dependency", "dependencies": []any{"APP-T-0003:hard"}})
	writeDirectTask(t, vault, "APP-T-0005", "W-0001", map[string]any{"status": "ready", "readiness": "ready"})
	writeHumanGate(t, vault, "APP-G-0001", "APP-T-0005")
	writeDirectTask(t, vault, "APP-T-0006", "W-0001", map[string]any{"status": "blocked", "readiness": "ready"})

	cases := []struct {
		taskID string
		want   string
	}{
		{"APP-T-0001", "task start accepts backlog, ready, or rework"},
		{"APP-T-0002", "ROUTE_INVALID"},
		{"APP-T-0004", "DEPENDENCY_WAITING"},
		{"APP-T-0005", "HUMAN_GATE_OPEN"},
		{"APP-T-0006", "task start accepts backlog, ready, or rework"},
	}
	for _, tc := range cases {
		var refusal serveActionResult
		servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/tasks/"+tc.taskID+"/start", `{"mode":"background"}`, &refusal)
		if !refusal.Refused {
			t.Fatalf("%s start was not refused: %#v", tc.taskID, refusal)
		}
		if !strings.Contains(refusal.Reason, tc.want) {
			t.Fatalf("%s refusal reason %q missing %q", tc.taskID, refusal.Reason, tc.want)
		}
		if directive, err := store.RunDirective(project.ProjectID, tc.taskID); err != nil || directive != nil {
			t.Fatalf("refused %s created a directive: %#v err=%v", tc.taskID, directive, err)
		}
	}
}

func TestDirectWaveServeChangedWaveReportsStale(t *testing.T) {
	vault, _, project, server := directWaveServeFixture(t, []string{"APP-T-0001"}, nil)
	writeDirectTask(t, vault, "APP-T-0001", "W-0001", nil)

	var started directStartResult
	servePost(t, server, "/api/actions/projects/"+project.ProjectID+"/waves/W-0001/start", `{"mode":"background"}`, &started)
	if started.Authorization != "authorized" {
		t.Fatalf("serve start did not authorize: %#v", started)
	}
	rewriteTaskFile(t, vault, "APP-T-0001", func(data map[string]any, body string) (map[string]any, string) {
		return data, body + "\nAmended material.\n"
	})
	var review directWaveReview
	serveDecode(t, server, "/api/projects/"+project.ProjectID+"/waves/W-0001/review", &review)
	if review.Authorization != "stale" {
		t.Fatalf("changed wave did not report stale authorization: %#v", review)
	}
	for _, control := range review.Controls {
		if control.Enabled && strings.HasPrefix(control.Action, "wave ") && control.Action != "wave start" {
			t.Fatalf("stale wave projected an enabled non-start wave control: %#v", review.Controls)
		}
	}
}
