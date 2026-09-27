package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestServeTaskEditUsesTaskUpdateWithCAS(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	server.operatorActor = "reviewer:owner"
	projects, _ := server.store.ListProjects()
	project := projects[0]
	var before serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &before)
	edit := map[string]any{
		"projectId": project.ProjectID, "revision": before.StateRevision,
		"title": "Edited from Serve", "body": "## Intent\n\nEdited intent.\n", "workLevel": "demanding",
	}
	raw, _ := json.Marshal(edit)
	rec := servePostJSON(t, server, "/api/tasks/APP-T-0001/edit", string(raw))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("edit: %d %s", rec.Code, rec.Body.String())
	}
	var after serveTaskDetail
	serveDecode(t, server, "/api/tasks/APP-T-0001?project="+project.ProjectID, &after)
	if after.Title != "Edited from Serve" || !strings.Contains(after.Body, "Edited intent.") || after.AuthoredWorkLevel != "demanding" || after.StateRevision == before.StateRevision {
		t.Fatalf("edit not persisted: title=%q level=%q rev=%q body=%q", after.Title, after.AuthoredWorkLevel, after.StateRevision, after.Body)
	}
	// The stale revision must be refused as a conflict and change nothing.
	edit["title"] = "Stale write"
	raw, _ = json.Marshal(edit)
	rec = servePostJSON(t, server, "/api/tasks/APP-T-0001/edit", string(raw))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "CAS_CONFLICT") {
		t.Fatalf("stale edit: %d %s", rec.Code, rec.Body.String())
	}
}

func TestServeTaskCreateUsesNewTaskAndJoinsWave(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	server.operatorActor = "reviewer:owner"
	projects, _ := server.store.ListProjects()
	project := projects[0]
	create := `{"projectId":"` + project.ProjectID + `","title":"Created from Serve","workLevel":"light","body":"## Intent\n\nNew.\n"}`
	rec := servePostJSON(t, server, "/api/tasks", create)
	var result serveActionResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || !result.OK || result.TaskID == "" {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var detail serveTaskDetail
	serveDecode(t, server, "/api/tasks/"+result.TaskID+"?project="+project.ProjectID, &detail)
	if detail.Title != "Created from Serve" || detail.AuthoredWorkLevel != "light" {
		t.Fatalf("created task: %#v", detail)
	}
	missing := servePostJSON(t, server, "/api/tasks", `{"projectId":"`+project.ProjectID+`","title":"No level","body":"x"}`)
	if !strings.Contains(missing.Body.String(), `"refused":true`) {
		t.Fatalf("new task without work level must be refused like the CLI: %s", missing.Body.String())
	}
	unknownWave := servePostJSON(t, server, "/api/tasks", `{"projectId":"`+project.ProjectID+`","title":"Waved","workLevel":"light","body":"x","wave":"NOPE-W-9999"}`)
	if !strings.Contains(unknownWave.Body.String(), "could not add it to NOPE-W-9999") {
		t.Fatalf("wave add failure must name the created task: %s", unknownWave.Body.String())
	}
}
