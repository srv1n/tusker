package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAutomationDispatchScopeFreshDefaultsAndLegacyProjection(t *testing.T) {
	fresh, err := resolveAutomationDispatchScope(resolvedTuskerConfig{}, false)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(automationDispatchScopeArmedWaves), fresh.Configured, "fresh configured scope")
	assertEqual(t, string(automationDispatchScopeArmedWaves), fresh.Effective, "fresh effective scope")

	legacy, err := resolveAutomationDispatchScope(resolvedTuskerConfig{}, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, "", legacy.Configured, "legacy configured scope remains absent")
	assertEqual(t, string(automationDispatchScopeAllEligible), legacy.Effective, "legacy effective scope")
	assertEqual(t, legacyDispatchScopeWarning, legacy.Warning, "legacy warning")
	assertEqual(t, legacyDispatchScopeRepair, legacy.Repair, "legacy repair")

	wf := defaultWorkflow()
	wf.DispatchScope = legacy
	if got := automationDispatchScopeBlocker("", Note{Data: map[string]any{"id": "APP-T-0001"}}, wf, nil); got != "" {
		t.Fatalf("legacy no-wave task lost prior eligibility: %q", got)
	}
	if got := automationDispatchScopeBlocker("", Note{Data: map[string]any{"id": "APP-T-0002", "wave": "W-0001"}}, wf, nil); got != "wave is not durably armed" {
		t.Fatalf("legacy wave task silently widened authority: %q", got)
	}
}

func TestAutomationDispatchScopeExplicitConfigAndEnforcement(t *testing.T) {
	resolved := resolvedTuskerConfig{Layers: []tuskerConfigLayer{{Name: configSourceProject, Path: "tusker.yaml", Present: true, Raw: map[string]any{"automation": map[string]any{"dispatch_scope": "all_eligible"}}}}}
	scope, err := resolveAutomationDispatchScope(resolved, true)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, string(automationDispatchScopeAllEligible), scope.Effective, "explicit broad scope")
	assertEqual(t, configSourceProject, scope.Provenance, "scope provenance")

	wf := defaultWorkflow()
	wf.DispatchScope = scope
	if got := automationDispatchScopeBlocker("", Note{Data: map[string]any{"id": "APP-T-0001"}}, wf, nil); got != "" {
		t.Fatalf("all_eligible unexpectedly blocked unrelated task: %q", got)
	}
	if got := automationDispatchScopeBlocker("", Note{Data: map[string]any{"id": "APP-T-0002", "wave": "W-0001"}}, wf, nil); got != "" {
		t.Fatalf("explicit all_eligible unexpectedly retained legacy wave constraint: %q", got)
	}
	wf.DispatchScope = defaultAutomationDispatchScope()
	got := automationDispatchScopeBlocker("", Note{Data: map[string]any{"id": "APP-T-0001"}}, wf, nil)
	if got != "dispatch scope armed_waves requires task membership in a currently armed wave" {
		t.Fatalf("unexpected armed_waves refusal: %q", got)
	}
}

func TestAutomationDispatchScopeRejectsInvalidValue(t *testing.T) {
	resolved := resolvedTuskerConfig{Layers: []tuskerConfigLayer{{Name: configSourceProject, Path: "tusker.yaml", Present: true, Raw: map[string]any{"automation": map[string]any{"dispatch_scope": "anything_goes"}}}}}
	_, err := resolveAutomationDispatchScope(resolved, false)
	if err == nil || !strings.Contains(err.Error(), "dispatch_scope") {
		t.Fatalf("expected dispatch scope validation error, got %v", err)
	}
}

func TestAutomationDispatchScopeFreshConfigAndDoctorWarningAreSideEffectFree(t *testing.T) {
	root := t.TempDir()
	vault := filepath.Join(root, ".tusker")
	if err := writeText(workflowPath(vault), defaultWorkflowMarkdown()); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultRootTuskerConfig(vault); err != nil {
		t.Fatal(err)
	}
	configPath := managedTuskerConfigPath(vault)
	config, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enabled: false", "dispatch_scope: armed_waves"} {
		if !strings.Contains(config, want) {
			t.Fatalf("fresh config missing %q:\n%s", want, config)
		}
	}
	if fileExists(filepath.Join(root, "tusker.yaml")) {
		t.Fatal("fresh bootstrap wrote a root-level tusker.yaml")
	}
	if err := writeText(configPath, "schema: tusker.config/v1\nproject_id: app\nautomation:\n  enabled: true\n"); err != nil {
		t.Fatal(err)
	}
	before, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	report, err := runSetupDoctor(setupDoctorInput{RepoRoot: root}, true)
	if err != nil {
		t.Fatal(err)
	}
	finding := findingByCode(report, "legacy_dispatch_scope")
	if finding == nil || finding.Changed || finding.Repairable || finding.Action != legacyDispatchScopeRepair {
		t.Fatalf("expected read-only legacy scope warning, got %#v", finding)
	}
	after, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, before, after, "doctor must not rewrite dispatch authority")
}

func TestDaemonStatusProjectsExposeDispatchScopeProjectionAndLegacyRepair(t *testing.T) {
	vault := automationTestVault(t)
	project := registerAutomationTestProject(t, vault)
	if err := writeText(managedTuskerConfigPath(vault), "schema: tusker.config/v1\nproject_id: app\nautomation:\n  enabled: true\n"); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := daemonStatusCmd(Args{"json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload struct {
		Status struct {
			DispatchScopes []daemonDispatchScopeSummary `json:"dispatch_scopes"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Status.DispatchScopes) != 1 {
		t.Fatalf("expected one dispatch scope projection, got %#v", payload.Status.DispatchScopes)
	}
	scope := payload.Status.DispatchScopes[0]
	assertEqual(t, project.ProjectID, scope.ProjectID, "scope project")
	assertEqual(t, "", scope.DispatchScope.Configured, "legacy configured scope")
	assertEqual(t, string(automationDispatchScopeAllEligible), scope.DispatchScope.Effective, "legacy effective scope")
	assertEqual(t, "legacy enabled config without dispatch_scope", scope.DispatchScope.Provenance, "legacy scope provenance")
	assertEqual(t, legacyDispatchScopeRepair, scope.DispatchScope.Repair, "legacy scope repair")

	text := captureStdout(t, func() {
		if err := daemonStatusCmd(Args{}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"dispatch_scope configured=- effective=all_eligible provenance=legacy enabled config without dispatch_scope", "repair=" + legacyDispatchScopeRepair} {
		if !strings.Contains(text, want) {
			t.Fatalf("daemon status missing %q:\n%s", want, text)
		}
	}
}
