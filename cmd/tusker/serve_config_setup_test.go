package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUserGlobalBehavioralConfigIsIgnoredWithProvenance(t *testing.T) {
	vault := automationTestVault(t)
	global := filepath.Join(t.TempDir(), "global-config.yaml")
	if err := writeText(global, "automation:\n  enabled: true\n  workspace:\n    strategy: clone\ntier: 1\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_CONFIG", global)

	for _, key := range []string{"automation.enabled", "tier", "workspace.strategy"} {
		report, err := configResolve(vault, key)
		if err != nil {
			t.Fatalf("resolve %s: %v", key, err)
		}
		if report.Source == configSourceUserGlobal {
			t.Fatalf("behavioral key %s unexpectedly won globally: %#v", key, report)
		}
		foundNote := false
		for _, source := range report.Sources {
			if source.Source == configSourceUserGlobal && source.Present && strings.Contains(source.Note, "ignored") {
				foundNote = true
			}
		}
		if !foundNote {
			t.Fatalf("resolve %s did not retain an ignored global provenance note: %#v", key, report.Sources)
		}
	}
	automation, err := configResolve(vault, "automation.enabled")
	if err != nil || automation.Value != false {
		t.Fatalf("automation global override resolved to %#v, want false", automation)
	}
	tier, err := configResolve(vault, "tier")
	if err != nil || tier.Value != 5 {
		t.Fatalf("tier global override resolved to %#v, want built-in 5", tier)
	}
}

func TestServeProjectSettingsAllowlistSupportsTierAndRejectsUnknown(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	project := projects[0]
	var tier serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/settings", `{"key":"tier","value":3}`, &tier)
	if !tier.OK {
		t.Fatalf("tier setting failed: %#v", tier)
	}
	report, err := configResolve(project.VaultRoot, "tier")
	if err != nil || report.Value != 3 || report.Source != configSourceLocal {
		t.Fatalf("tier readback = %#v, want local 3", report)
	}
	var unknown serveActionResult
	servePost(t, server, "/api/projects/"+project.ProjectID+"/settings", `{"key":"automation.enabled","value":true}`, &unknown)
	if unknown.OK || !unknown.Refused || !strings.Contains(unknown.Reason, "unsupported") {
		t.Fatalf("unknown setting was accepted: %#v", unknown)
	}
}

func TestServeProjectAutomationDisableWithoutExplicitKeyIsIdempotent(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	project, err := server.store.ListProjects()
	if err != nil || len(project) != 1 {
		t.Fatalf("fixture project: %v %#v", err, project)
	}
	id := project[0].ProjectID
	if report, err := configResolve(project[0].VaultRoot, "automation.enabled"); err != nil || report.Value != false {
		t.Fatalf("fixture must omit automation.enabled: %#v err=%v", report, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		var result serveActionResult
		servePost(t, server, "/api/projects/"+id+"/automation", `{"enabled":false}`, &result)
		if !result.OK || result.Refused {
			t.Fatalf("disable attempt %d failed: %#v", attempt+1, result)
		}
	}
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 || projects[0].Enabled {
		t.Fatalf("registry bit was not cleared: %#v err=%v", projects, err)
	}
	report, err := configResolve(project[0].VaultRoot, "automation.enabled")
	if err != nil || report.Value != false || report.Source == configSourceLocal {
		t.Fatalf("absent automation key became an unexpected local override: %#v err=%v", report, err)
	}
}

func TestWalkthroughProjectAutomationUsesSelectedCheckoutRuntimeState(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	primary := projects[0]
	primary.RepositoryKey = "walkthrough-repository"
	primary.Enabled = false
	primary.Health = projectHealthDisabled
	if err := server.store.UpsertProject(primary); err != nil {
		t.Fatal(err)
	}

	childRoot := t.TempDir()
	childVault := filepath.Join(childRoot, ".tusker")
	if err := ensureDir(childVault); err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(childVault), "schema: tusker.config/v1\nproject_id: child\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeText(workflowPath(childVault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	child := primary
	child.ProjectID, child.ProjectKey, child.Name = "walkthrough-child", "child", "child"
	child.RepoRoot, child.VaultRoot, child.WorkflowPath = childRoot, childVault, workflowPath(childVault)
	if err := server.store.UpsertProject(child); err != nil {
		t.Fatal(err)
	}

	var enabled serveActionResult
	servePost(t, server, "/api/projects/"+child.ProjectID+"/automation", `{"enabled":true}`, &enabled)
	if !enabled.OK || enabled.Refused || enabled.AutomationEnabled == nil || !*enabled.AutomationEnabled {
		t.Fatalf("enable response did not return persisted child state: %#v", enabled)
	}
	storedPrimary, err := projectByID(server.store, primary.ProjectID)
	if err != nil || storedPrimary.Enabled {
		t.Fatalf("child enable changed primary state: %#v err=%v", storedPrimary, err)
	}
	storedChild, err := projectByID(server.store, child.ProjectID)
	if err != nil || !storedChild.Enabled {
		t.Fatalf("child enable was not persisted: %#v err=%v", storedChild, err)
	}
	assertWalkthroughAutomationSummary(t, server, primary.ProjectID, false)
	assertWalkthroughAutomationSummary(t, server, child.ProjectID, true)

	var disabled serveActionResult
	servePost(t, server, "/api/projects/"+child.ProjectID+"/automation", `{"enabled":false}`, &disabled)
	if !disabled.OK || disabled.Refused || disabled.AutomationEnabled == nil || *disabled.AutomationEnabled {
		t.Fatalf("disable response did not return persisted child state: %#v", disabled)
	}
	assertWalkthroughAutomationSummary(t, server, child.ProjectID, false)

	if err := os.RemoveAll(childVault); err != nil {
		t.Fatal(err)
	}
	var refused serveActionResult
	servePost(t, server, "/api/projects/"+child.ProjectID+"/automation", `{"enabled":true}`, &refused)
	if refused.OK || !refused.Refused {
		t.Fatalf("invalid child enable reported false success: %#v", refused)
	}
	assertWalkthroughAutomationSummary(t, server, child.ProjectID, false)
}

func TestWalkthroughProjectAutomationSerializesExecutionSettingsWrites(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	vault := projects[0].VaultRoot
	started := make(chan struct{}, 2)
	done := make(chan error, 2)
	projectLocalConfigWriteMu.Lock()
	for _, setting := range []struct {
		key   string
		value any
	}{
		{"automation.enabled", true},
		{"runtime.max_active_runs_per_project", 2},
	} {
		go func(key string, value any) {
			started <- struct{}{}
			_, err := setProjectLocalConfigWithReadback(vault, key, value)
			done <- err
		}(setting.key, setting.value)
	}
	<-started
	<-started
	select {
	case err := <-done:
		t.Fatalf("config write escaped its serialization lock: %v", err)
	default:
	}
	projectLocalConfigWriteMu.Unlock()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("serialized config write did not finish")
		}
	}
	for _, want := range []struct {
		key   string
		value any
	}{
		{"automation.enabled", true},
		{"runtime.max_active_runs_per_project", 2},
	} {
		report, err := configResolve(vault, want.key)
		if err != nil || configValueChanged(report.Value, want.value) {
			t.Fatalf("concurrent setting %s was not preserved: %#v err=%v", want.key, report, err)
		}
	}
}

func assertWalkthroughAutomationSummary(t *testing.T, server *serveServer, projectID string, want bool) {
	t.Helper()
	var summaries []serveProjectSummary
	serveDecode(t, server, "/api/projects", &summaries)
	for _, summary := range summaries {
		if summary.ID == projectID && summary.AutomationEnabled == want {
			return
		}
		for _, checkout := range summary.Checkouts {
			if checkout.ID == projectID && checkout.AutomationEnabled == want {
				return
			}
		}
	}
	t.Fatalf("project %s automation summary did not equal %t: %#v", projectID, want, summaries)
}

func TestServeConfigReturnsValueAndSource(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	project, err := server.store.ListProjects()
	if err != nil || len(project) == 0 {
		t.Fatalf("fixture project: %v %#v", err, project)
	}
	if _, err := setProjectLocalConfigWithReadback(project[0].VaultRoot, "tier", 3); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		OK     bool   `json:"ok"`
		Value  int    `json:"value"`
		Source string `json:"source"`
	}
	serveDecode(t, server, "/api/config?project="+project[0].ProjectID+"&key=tier", &payload)
	if !payload.OK || payload.Value != 3 || payload.Source != configSourceLocal {
		t.Fatalf("config response = %#v", payload)
	}
}

func TestServeProjectRemoveRequiresCapabilityAndUsesCLIPath(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	server.requireCapability = true
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	projectID := projects[0].ProjectID
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/projects/"+projectID+"/remove", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("remove without capability status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	token := serveTestCapability(t, server)
	request = httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/projects/"+projectID+"/remove", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(serveCapabilityHeader, token)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var result serveActionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !result.OK {
		t.Fatalf("remove with capability status=%d result=%#v", recorder.Code, result)
	}
	remaining, err := server.store.ListProjects()
	if err != nil || len(remaining) != 0 {
		t.Fatalf("project remained after remove: %v %#v", err, remaining)
	}
}

func TestServeSetupDoctorReturnsReport(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	server.requireCapability = true
	token := serveTestCapability(t, server)
	request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7420/api/setup/doctor", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(serveCapabilityHeader, token)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var result serveActionResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Report *setupDoctorReport `json:"report"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !result.OK || payload.Report == nil || payload.Report.Schema != setupDoctorSchema {
		t.Fatalf("setup doctor response status=%d result=%#v body=%s", recorder.Code, result, recorder.Body.String())
	}
}

func serveTestCapability(t *testing.T, server *serveServer) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7420/api/capability", nil)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	var payload struct {
		Capability string `json:"capability"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil || payload.Capability == "" {
		t.Fatalf("capability response=%s err=%v", recorder.Body.String(), err)
	}
	return payload.Capability
}

func TestWalkthroughProjectAutomationRollsBackConfigWhenRuntimeWriteRefuses(t *testing.T) {
	server := newServeEmptyNeedsFixture(t)
	projects, err := server.store.ListProjects()
	if err != nil || len(projects) != 1 {
		t.Fatalf("fixture project: %v %#v", err, projects)
	}
	project := projects[0]
	before, existed, err := readConfigText(managedTuskerLocalConfigPath(project.VaultRoot))
	if err != nil {
		t.Fatal(err)
	}
	project.ProjectID = "missing-project"
	if _, err := setProjectAutomationAudited(server.store, project, true, defaultActorName(), "cli"); err == nil {
		t.Fatal("expected missing runtime project refusal")
	}
	after, afterExists, err := readConfigText(managedTuskerLocalConfigPath(project.VaultRoot))
	if err != nil || afterExists != existed || after != before {
		t.Fatalf("runtime refusal leaked automation config: before=(%t,%q) after=(%t,%q) err=%v", existed, before, afterExists, after, err)
	}
}
