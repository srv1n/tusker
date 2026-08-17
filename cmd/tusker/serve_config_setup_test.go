package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserGlobalBehavioralConfigIsIgnoredWithProvenance(t *testing.T) {
	vault := automationTestVault(t)
	global := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_CONFIG_HOME", global)
	if err := ensureDir(filepath.Join(global, "tusker")); err != nil {
		t.Fatal(err)
	}
	if err := writeText(filepath.Join(global, "tusker", "config.yaml"), "automation:\n  enabled: true\n  workspace:\n    strategy: clone\ntier: 1\n"); err != nil {
		t.Fatal(err)
	}

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
