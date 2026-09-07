package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestModelLevelsRoutingAndConfiguration(t *testing.T) {
	vault := automationTestVault(t)
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	report, err := modelLevelsRead(vault)
	if err != nil || report.Schema != modelLevelsSchema || len(report.Levels) != 3 {
		t.Fatalf("initial model levels: %#v err=%v", report, err)
	}
	standard := report.Levels[1]
	if len(standard.Execute.Profiles) == 0 || standard.Execute.Source != configSourceBuiltIn {
		t.Fatalf("built-in provenance missing: %#v", standard)
	}
	compact := compactModelLevelsReport(report)
	if len(compact.Profiles) >= len(report.Profiles) || compact.Profiles["default"].Harness == "" || compact.Profiles["planner"].Harness != "" {
		t.Fatalf("compact report did not retain only referenced profiles: %#v", compact.Profiles)
	}

	profile := RunnerProfileDefinition{Harness: string(RunnerCodexExec), Model: "gpt-test", Effort: "high", PermissionPreset: "workspace-write-offline", Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)}, Subagents: RunnerSubagentPolicyDefinition{Allowed: boolPtr(false)}}
	if _, err := setUserGlobalConfigWithReadback("automation.profiles.global-standard", profile); err != nil {
		t.Fatal(err)
	}
	if _, err := setUserGlobalConfigWithReadback("automation.model_levels.standard.execute", []string{"global-standard", "default"}); err != nil {
		t.Fatal(err)
	}
	global, err := modelLevelsRead(vault)
	if err != nil || global.Levels[1].Execute.Source != configSourceUserGlobal || strings.Join(global.Levels[1].Execute.Profiles, ",") != "global-standard,default" {
		t.Fatalf("global level = %#v err=%v", global.Levels[1], err)
	}

	if _, err := setProjectLocalConfigWithReadback(vault, "automation.model_levels.standard.execute", []string{"default"}); err != nil {
		t.Fatal(err)
	}
	project, err := modelLevelsRead(vault)
	if err != nil || !project.Levels[1].Execute.Overridden || project.Levels[1].Execute.Source != configSourceLocal {
		t.Fatalf("project override = %#v err=%v", project.Levels[1], err)
	}
	stale := project.Revision
	if err := modelsSetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "profiles": "global-standard", "if-revision": "sha256:stale"}); err == nil {
		t.Fatal("stale write was accepted")
	}
	if err := modelsResetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "if-revision": stale}); err != nil {
		t.Fatal(err)
	}
	reset, _ := modelLevelsRead(vault)
	if reset.Levels[1].Execute.Source != configSourceUserGlobal || strings.Join(reset.Levels[1].Execute.Profiles, ",") != "global-standard,default" {
		t.Fatalf("reset did not inherit global: %#v", reset.Levels[1])
	}
}

func TestModelLevelsPrecedenceFallbackAndStableCycle(t *testing.T) {
	wf := Workflow{RunnerProfiles: map[string]RunnerProfileDefinition{
		"primary":          {Harness: string(RunnerCodexExec), Model: "gpt-primary", Effort: "high"},
		"fallback":         {Harness: string(RunnerClaude), Model: "claude-fallback", Effort: "medium"},
		"explicit":         {Harness: string(RunnerCodexExec), Model: "gpt-explicit", Effort: "low"},
		"execute-frontier": {Harness: string(RunnerCodexExec), Model: "gpt-frontier", Effort: "max"},
	}, ModelLevels: map[string]ModelLevelDefinition{"standard": {Execute: []string{"primary", "fallback"}}, "demanding": {Execute: []string{"primary"}}}}

	note := Note{Data: map[string]any{"id": "APP-T-0001", "work_level": "standard", "complexity": "routine"}}
	selected, err := resolveRunnerProfileForNote(note, wf, runLaneExecute)
	if err != nil || selected.Name != "primary" || strings.Join(selected.Fallbacks, ",") != "fallback" || !strings.Contains(selected.Source, "standard") {
		t.Fatalf("level route = %#v err=%v", selected, err)
	}
	note.Data["runner_profile"] = "explicit"
	selected, err = resolveRunnerProfileForNote(note, wf, runLaneExecute)
	if err != nil || selected.Name != "explicit" || selected.Source != "task frontmatter" {
		t.Fatalf("explicit precedence = %#v err=%v", selected, err)
	}
	delete(note.Data, "runner_profile")
	delete(note.Data, "work_level")
	note.Data["complexity"] = "frontier"
	selected, err = resolveRunnerProfileForNote(note, wf, runLaneExecute)
	if err != nil || selected.Name != "execute-frontier" {
		t.Fatalf("legacy frontier changed: %#v err=%v", selected, err)
	}

	run := RunStatus{Lane: runLaneExecute, AttemptCount: 1, RunnerProfile: "primary", RunnerHarness: string(RunnerCodexExec), RunnerModel: "gpt-old", RunnerEffort: "high"}
	changed := ResolvedRunnerProfile{Name: "primary", Definition: RunnerProfileDefinition{Harness: string(RunnerCodexExec), Model: "gpt-new", Effort: "low"}}
	stable := preserveResolvedRunIdentity(run, runLaneExecute, changed)
	if stable.Definition.Model != "gpt-old" || stable.Definition.Effort != "high" || stable.Reason != "retry preserves resolved identity" {
		t.Fatalf("active cycle drifted: %#v", stable)
	}
	reviewer := ResolvedRunnerProfile{Name: "review", Definition: RunnerProfileDefinition{Harness: string(RunnerClaude), Model: "opus", Effort: "high"}}
	if switched := preserveResolvedRunIdentity(run, runLaneReview, reviewer); switched.Name != "review" || switched.Definition.Model != "opus" {
		t.Fatalf("lane switch inherited execute identity: %#v", switched)
	}
}

func TestModelLevelsCatalogProvenanceAndReasoning(t *testing.T) {
	original := runnerCatalogCommand
	t.Cleanup(func() { runnerCatalogCommand = original })
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "codex" && len(args) > 1 && args[0] == "debug" {
			return []byte(`{"models":[{"slug":"gpt-live","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"default_reasoning_level":"high"}]}`), nil
		}
		if name == "codex" {
			return []byte("codex-test"), nil
		}
		return nil, errTestCommandUnavailable
	}
	catalog := discoverRunnerCatalog(false)
	if !catalog.Harnesses[0].Available || catalog.Harnesses[0].Source != "live" || strings.Join(catalog.Harnesses[0].Models[0].Efforts, ",") != "high,low" {
		t.Fatalf("catalog provenance/reasoning = %#v", catalog.Harnesses[0])
	}
	if catalog.Harnesses[1].Available || catalog.Harnesses[1].Error == "" {
		t.Fatalf("unsupported discovery was invented: %#v", catalog.Harnesses[1])
	}
}

func TestModelLevelsFallbackOnlyOnDefinitivePreflightFailure(t *testing.T) {
	candidates := []ResolvedRunnerProfile{{Name: "primary"}, {Name: "fallback"}}
	selected, reason, err := selectModelProfile(candidates, func(profile ResolvedRunnerProfile) (bool, string, error) {
		return profile.Name == "fallback", "runtime missing", nil
	})
	if err != nil || selected.Name != "fallback" || !strings.Contains(reason, "primary") {
		t.Fatalf("explicit fallback=%#v %q %v", selected, reason, err)
	}
	uncertain := errTestCommandUnavailable
	if _, _, err := selectModelProfile(candidates, func(ResolvedRunnerProfile) (bool, string, error) { return false, "", uncertain }); err != uncertain {
		t.Fatalf("uncertain outcome retried: %v", err)
	}
}

var errTestCommandUnavailable = &testModelLevelError{}

type testModelLevelError struct{}

func (*testModelLevelError) Error() string { return "unavailable" }
