package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerRoutePreview(t *testing.T) {
	vault := automationTestVault(t)
	id := "APP-T-0001"
	if err := newV7Task(Args{"vault": vault, "quiet": "true", "epic": "APP", "title": "Route preview", "risk": "low", "priority": "p1", "v7": "true"}); err != nil {
		t.Fatal(err)
	}
	repo := v7RepoRoot(vault)
	gitDirOutput(t, repo, "init")
	gitDirOutput(t, repo, "config", "user.email", "test@example.com")
	gitDirOutput(t, repo, "config", "user.name", "Test")
	gitDirOutput(t, repo, "add", ".")
	gitDirOutput(t, repo, "commit", "-m", "fixture")
	profiles := map[string]RunnerProfileDefinition{}
	for _, name := range []string{"default", "task", "rule", "lane", "execute-fast", "execute-standard", "execute-complex", "execute-frontier", "review-independent"} {
		profiles[name] = RunnerProfileDefinition{
			Harness:          string(RunnerCodexExec),
			Model:            name + "-model",
			Effort:           "medium",
			PermissionPreset: "guarded-yolo",
			Sandbox:          RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)},
			Subagents:        RunnerSubagentPolicyDefinition{Allowed: boolPtr(true), MaxConcurrent: 2},
		}
	}
	wf := defaultWorkflow()
	wf.RunnerProfiles, wf.RunnerDefaultProfile = profiles, "default"
	wf.RunnerLaneProfiles = map[string]string{runLaneExecute: "lane"}
	wf.RunnerRouting = []RunnerRoutingRule{{Name: "risk", Profile: "rule", Match: RunnerRoutingMatch{Risk: "high"}}}

	for _, tc := range []struct {
		name, lane, complexity, override, risk, want, source string
	}{
		{"task wins", runLaneExecute, "frontier", "task", "high", "task", "task frontmatter"},
		{"rule wins", runLaneExecute, "frontier", "", "high", "rule", "automation.routing"},
		{"lane wins", runLaneExecute, "frontier", "", "low", "lane", "automation.lane_profiles"},
		{"semantic execute", runLaneExecute, "complex", "", "low", "execute-complex", "task complexity"},
		{"semantic review", runLaneReview, "frontier", "", "low", "review-independent", "task complexity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := wf
			if tc.want == "execute-complex" || tc.want == "review-independent" {
				local.RunnerLaneProfiles = nil
			}
			note := Note{Data: map[string]any{"id": id, "title": "route", "risk": tc.risk, "complexity": tc.complexity, "runner_profile": tc.override}}
			preview := routePreviewForNote(note, local, tc.lane)
			assertEqual(t, tc.want, preview.Profile, "profile")
			assertEqual(t, tc.source, preview.Source, "source")
			assertEqual(t, tc.want+"-model", preview.ProfileDefinition.Model, "profile definition model")
			assertEqual(t, "guarded-yolo", preview.ProfileDefinition.PermissionPreset, "profile definition permission preset")
			assertEqual(t, "workspace-write", preview.ProfileDefinition.Sandbox.Mode, "profile definition sandbox mode")
			if preview.ProfileDefinition.Sandbox.Network == nil || *preview.ProfileDefinition.Sandbox.Network {
				t.Fatalf("profile definition sandbox network missing or wrong: %#v", preview.ProfileDefinition.Sandbox)
			}
			if preview.ProfileDefinition.Subagents.Allowed == nil || !*preview.ProfileDefinition.Subagents.Allowed || preview.ProfileDefinition.Subagents.MaxConcurrent != 2 {
				t.Fatalf("profile definition subagent policy missing or wrong: %#v", preview.ProfileDefinition.Subagents)
			}
			if !preview.ReadOnly || len(preview.Precedence) != 5 || len(preview.Blockers) != 0 {
				t.Fatalf("bad preview: %#v", preview)
			}
		})
	}

	missing := wf
	missing.RunnerLaneProfiles = nil
	delete(missing.RunnerProfiles, "execute-frontier")
	blocked := routePreviewForNote(Note{Data: map[string]any{"id": id, "complexity": "frontier"}}, missing, runLaneExecute)
	if len(blocked.Blockers) != 1 || len(blocked.Precedence) != 5 || !strings.Contains(blocked.Blockers[0], "execute-frontier") {
		t.Fatalf("missing role was silently substituted: %#v", blocked)
	}
	invalid := routePreviewForNote(Note{Data: map[string]any{"id": id, "complexity": "turbo"}}, wf, runLaneExecute)
	if len(invalid.Blockers) != 1 || len(invalid.Precedence) != 5 || !strings.Contains(invalid.Blockers[0], "invalid task complexity") {
		t.Fatalf("invalid complexity was accepted: %#v", invalid)
	}

	setAutomationV7TaskFields(t, vault, id, map[string]any{"complexity": "routine"})
	taskPath := filepath.Join(vault, "work", "tasks", id+".md")
	beforeTask, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(v7RepoRoot(vault), "tusker.yaml")
	beforeConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	freshState := filepath.Join(t.TempDir(), "fresh-runtime")
	t.Setenv("TUSKER_STATE_ROOT", freshState)
	beforeRef := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD"))
	output := captureStdout(t, func() {
		if err := runnerRouteCmd(Args{"vault": vault, "id": id, "lane": runLaneExecute, "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var payload runnerRoutePreview
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Schema != "tusker.runner-route/v1" || !payload.ReadOnly {
		t.Fatalf("route output did not declare stable read-only schema: %s", output)
	}
	afterTask, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeTask) != string(afterTask) {
		t.Fatal("route preview mutated task")
	}
	afterConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforeConfig) != string(afterConfig) {
		t.Fatal("route preview mutated config")
	}
	if _, err := os.Stat(freshState); !os.IsNotExist(err) {
		t.Fatalf("route preview created runtime state: %v", err)
	}
	if afterRef := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "HEAD")); afterRef != beforeRef {
		t.Fatal("route preview moved Git ref")
	}
}
