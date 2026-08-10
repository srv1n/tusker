package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func completionReactorLayer(name, path, mode string) tuskerConfigLayer {
	raw := map[string]any{"automation": map[string]any{"completion_reactor": map[string]any{"mode": mode}}}
	return tuskerConfigLayer{Name: name, Path: path, Present: true, Raw: raw}
}

func TestCompletionReactorModeResolution(t *testing.T) {
	tests := []struct {
		name              string
		layers            []tuskerConfigLayer
		automationEnabled bool
		wantConfigured    string
		wantEffective     string
		wantProvenance    string
		wantWarning       string
	}{
		{
			name:              "absent disabled is fresh default",
			automationEnabled: false,
			wantConfigured:    "disabled",
			wantEffective:     "disabled",
			wantProvenance:    "fresh default",
		},
		{
			name:              "absent enabled preserves legacy authority",
			automationEnabled: true,
			wantEffective:     "legacy",
			wantProvenance:    "legacy enabled config without completion_reactor.mode",
			wantWarning:       legacyCompletionReactorModeWarning,
		},
		{
			name:              "explicit disabled",
			layers:            []tuskerConfigLayer{completionReactorLayer(configSourceProject, "tusker.yaml", "disabled")},
			automationEnabled: true,
			wantConfigured:    "disabled",
			wantEffective:     "disabled",
			wantProvenance:    configSourceProject,
		},
		{
			name:              "explicit shadow",
			layers:            []tuskerConfigLayer{completionReactorLayer(configSourceProject, "tusker.yaml", "shadow")},
			automationEnabled: true,
			wantConfigured:    "shadow",
			wantEffective:     "shadow",
			wantProvenance:    configSourceProject,
		},
		{
			name:              "explicit authoritative",
			layers:            []tuskerConfigLayer{completionReactorLayer(configSourceProject, "tusker.yaml", "authoritative")},
			automationEnabled: true,
			wantConfigured:    "authoritative",
			wantEffective:     "authoritative",
			wantProvenance:    configSourceProject,
		},
		{
			name: "local explicit mode wins over project",
			layers: []tuskerConfigLayer{
				completionReactorLayer(configSourceProject, "tusker.yaml", "shadow"),
				completionReactorLayer(configSourceLocal, "tusker.local.yaml", "disabled"),
			},
			automationEnabled: true,
			wantConfigured:    "disabled",
			wantEffective:     "disabled",
			wantProvenance:    configSourceLocal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCompletionReactorMode(resolvedTuskerConfig{Layers: tt.layers}, tt.automationEnabled)
			if err != nil {
				t.Fatal(err)
			}
			assertEqual(t, tt.wantConfigured, got.Configured, "configured mode")
			assertEqual(t, tt.wantEffective, got.Effective, "effective mode")
			assertEqual(t, tt.wantProvenance, got.Provenance, "provenance")
			assertEqual(t, tt.wantWarning, got.Warning, "warning")
		})
	}
}

func TestCompletionReactorModeRejectsInvalidConfiguredValue(t *testing.T) {
	resolved := resolvedTuskerConfig{Layers: []tuskerConfigLayer{completionReactorLayer(configSourceProject, "tusker.yaml", "go_for_it")}}
	if _, err := resolveCompletionReactorMode(resolved, false); err == nil || !strings.Contains(err.Error(), "completion_reactor.mode") {
		t.Fatalf("expected invalid completion-reactor mode error, got %v", err)
	}

	root := t.TempDir()
	if err := writeText(filepath.Join(root, "tusker.yaml"), "automation:\n  completion_reactor:\n    mode: go_for_it\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveTuskerConfigForRepo(root, true); err == nil || !strings.Contains(err.Error(), "completion_reactor.mode") {
		t.Fatalf("invalid mode was silently accepted: %v", err)
	}
}

func TestCompletionReactorModeFreshConfigAndDoctorWarningAreSideEffectFree(t *testing.T) {
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
	if !strings.Contains(config, "completion_reactor:\n    mode: disabled") {
		t.Fatalf("fresh config did not explicitly disable completion reactor:\n%s", config)
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
	finding := findingByCode(report, "legacy_completion_reactor_mode")
	if finding == nil || finding.Changed || finding.Repairable || finding.Action != legacyCompletionReactorModeRepair {
		t.Fatalf("expected read-only legacy completion mode warning, got %#v", finding)
	}
	after, err := readText(configPath)
	if err != nil {
		t.Fatal(err)
	}
	assertEqual(t, before, after, "doctor must not rewrite completion authority")
}

func TestAutomationStatusSummaryExposesCompletionReactorMode(t *testing.T) {
	wf := defaultWorkflow()
	wf.CompletionReactor = completionReactorModeProjection{Effective: "shadow", Provenance: configSourceProject}
	summary := automationSummarizeProject(RegisteredProject{ProjectID: "project", ProjectKey: "APP"}, WorkflowFile{Data: wf}, nil, true)
	assertEqual(t, "shadow", summary.CompletionReactor.Effective, "status completion reactor mode")
	assertEqual(t, configSourceProject, summary.CompletionReactor.Provenance, "status completion reactor provenance")
}
