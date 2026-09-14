package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskAuthoringServeActionRetainsFullTaskContract(t *testing.T) {
	server := newServeFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects: %v %v", projects, err)
	}
	project := projects[0]
	id := "APP-T-0003" // Existing human-gated task with an actual run.
	path := filepath.Join(project.VaultRoot, "work", "tasks", id+".md")
	data, body, err := parseFrontmatterMustRead(path)
	if err != nil {
		t.Fatal(err)
	}
	data["architect"], data["origin"] = "task:APP-T-0001", "task:APP-T-0002"
	data["execute_profile"], data["review_profile"] = "execute-standard", "review-standard"
	body += "\n## Implementation notes\n\nKeep the shared contact store authoritative.\n"
	if _, err := saveV7DocumentCAS(path, data, body, v7FrontmatterOrder["task"], stringField(data, "state_rev")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.store.PutAgentContact(AgentContact{
		ProjectID: project.ProjectID, TaskID: id, Role: "architect",
		Address: AgentAddress{Kind: "task", ID: "APP-T-0001"},
	}, 0); err != nil {
		t.Fatal(err)
	}
	var read serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+id+"?project="+project.ProjectID, &read)
	if read.StateRevision == "" || len(read.Contacts) == 0 || len(read.HumanActions) == 0 || read.AuthoredExecuteProfile == "" || !strings.Contains(read.Body, "Keep the shared contact store authoritative.") {
		t.Fatalf("fixture must contain revision, contacts, human action and explicit routing: %#v", read)
	}
	result := serveActionResult{OK: true}
	server.decorateTaskActionResultForProject(&result, id, project.ProjectID)
	if result.Task == nil {
		t.Fatal("action response dropped the task")
	}
	got := *result.Task
	if got.StateRevision != read.StateRevision || got.AuthoredExecuteProfile != read.AuthoredExecuteProfile || got.AuthoredReviewProfile != read.AuthoredReviewProfile || got.Architect != read.Architect || got.Origin != read.Origin || got.Body != read.Body {
		t.Fatalf("action response lost authored identity or routing: %#v", got)
	}
	for name, values := range map[string][2]any{
		"contacts": {got.Contacts, read.Contacts}, "human actions": {got.HumanActions, read.HumanActions},
		"execute route": {got.EffectiveExecute, read.EffectiveExecute}, "review route": {got.EffectiveReview, read.EffectiveReview},
	} {
		actual, _ := json.Marshal(values[0])
		expected, _ := json.Marshal(values[1])
		if string(actual) != string(expected) {
			t.Fatalf("action response differs for %s: got %s; read %s", name, actual, expected)
		}
	}
}

func TestTaskAuthoringServeStartRechecksReviewerAfterPreview(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	id := "APP-T-0001"
	setAutomationV7TaskFields(t, project.VaultRoot, id, map[string]any{"work_level": "standard"})
	var preview serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+id+"?project="+project.ProjectID, &preview)
	if len(preview.EffectiveExecute.Blockers) > 0 || len(preview.EffectiveReview.Blockers) > 0 {
		t.Fatalf("configured preview is blocked: %#v %#v", preview.EffectiveExecute, preview.EffectiveReview)
	}
	// Simulate a settings change after the UI rendered a usable preview.
	if _, err := setProjectLocalConfigWithReadback(project.VaultRoot, "automation.model_levels.standard.review", []string{}); err != nil {
		t.Fatal(err)
	}
	rec := servePostJSON(t, server, "/api/actions/projects/"+project.ProjectID+"/tasks/"+id+"/start", `{"mode":"background"}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "review route blocked") {
		t.Fatalf("task start did not report the fresh reviewer blocker: %d %s", rec.Code, rec.Body.String())
	}
	directive, err := server.store.RunDirective(project.ProjectID, id)
	if err != nil || directive != nil {
		t.Fatalf("blocked task start must not queue work: %#v %v", directive, err)
	}
}

func TestTaskAuthoringServeClearingTierStaysUnclassified(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var before serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &before)
	body := `{"projectId":"` + project.ProjectID + `","revision":"` + before.StateRevision + `","workLevel":""}`
	rec := servePostJSON(t, server, "/api/tasks/APP-T-0001/route", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("clear tier: %d %s", rec.Code, rec.Body.String())
	}
	var after serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &after)
	if after.EffectiveExecute.WorkLevel != "" || len(after.EffectiveExecute.Blockers) == 0 {
		t.Fatalf("clearing a tier silently selected a legacy default: %#v", after.EffectiveExecute)
	}
}

func TestTaskAuthoringServeValidatesTierChangesBeforeSaving(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var before serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &before)
	for _, fields := range []string{
		`"workLevel":"not-a-tier"`,
		`"reviewLevel":"not-a-tier"`,
		`"reviewLevel":"demanding"`,
	} {
		body := `{"projectId":"` + project.ProjectID + `","revision":"` + before.StateRevision + `",` + fields + `}`
		rec := servePostJSON(t, server, "/api/tasks/APP-T-0001/route", body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid tier change %s: %d %s", fields, rec.Code, rec.Body.String())
		}
		var after serveTaskDetail
		serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &after)
		if after.StateRevision != before.StateRevision {
			t.Fatalf("rejected update changed the task revision: %s", fields)
		}
	}
	// An unrelated routing edit must keep a historical override readable.
	setAutomationV7TaskFields(t, project.VaultRoot, "APP-T-0001", map[string]any{"review_level": "demanding"})
	server.invalidateProjectSnapshot(project.ProjectID)
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &before)
	body := `{"projectId":"` + project.ProjectID + `","revision":"` + before.StateRevision + `","executeProfile":null}`
	rec := servePostJSON(t, server, "/api/tasks/APP-T-0001/route", body)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("unrelated historical route edit: %d %s", rec.Code, rec.Body.String())
	}
}
