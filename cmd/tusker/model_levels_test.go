package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestModelLevelsRoutingAndConfiguration(t *testing.T) {
	vault := automationTestVault(t)
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))

	report, err := modelLevelsRead(vault)
	if err != nil || report.Schema != modelLevelsSchema || len(report.Levels) != 3 {
		t.Fatalf("initial model levels: %#v err=%v", report, err)
	}
	if report.ProfileStates["review-frontier"] != "configured_unverified" {
		t.Fatalf("untested Claude profile state: %#v", report.ProfileStates)
	}
	standard := report.Levels[1]
	if len(standard.Execute.Profiles) == 0 || standard.Execute.Source != configSourceBuiltIn {
		t.Fatalf("built-in provenance missing: %#v", standard)
	}
	compact := compactModelLevelsReport(report)
	if len(compact.Profiles) >= len(report.Profiles) || compact.Profiles["default"].Harness == "" || compact.Profiles["planner"].Harness != "" {
		t.Fatalf("compact report did not retain only referenced profiles: %#v", compact.Profiles)
	}

	profile := RunnerProfileDefinition{EligibleTiers: []string{"standard"}, Harness: string(RunnerCodexExec), Model: "gpt-test", Effort: "high", PermissionPreset: "workspace-write-offline", Sandbox: RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)}, Subagents: RunnerSubagentPolicyDefinition{Allowed: boolPtr(false)}}
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

func TestClaudeProfileTestState(t *testing.T) {
	t.Setenv("TUSKER_STATE_ROOT", t.TempDir())
	profile := RunnerProfileDefinition{Harness: string(RunnerClaude), Model: "claude-test", Effort: "high", PermissionPreset: "read-only"}
	if got := profileTestState("claude-profile", profile, "revision"); got != "configured_unverified" {
		t.Fatalf("untested Claude profile state = %q", got)
	}
	receipt, err := json.Marshal(map[string]any{
		"profile_id": "claude-profile", "profile_revision": "revision", "model": profile.Model,
		"effort": profile.Effort, "preset": profile.PermissionPreset, "live": true,
		"ready": true, "valid_until": time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(DefaultStateRoot(), "runner-conformance")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude-profile-read-only.json"), receipt, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := profileTestState("claude-profile", profile, "revision"); got != "tested" {
		t.Fatalf("tested Claude profile state = %q", got)
	}
	profile.Disabled = true
	if got := profileTestState("claude-profile", profile, "revision"); got != "disabled" {
		t.Fatalf("disabled Claude profile state = %q", got)
	}
}

func TestModelLevelsPrecedenceFallbackAndStableCycle(t *testing.T) {
	wf := Workflow{RunnerProfiles: map[string]RunnerProfileDefinition{
		"primary":          {Harness: string(RunnerCodexExec), Model: "gpt-primary", Effort: "high"},
		"fallback":         {Harness: string(RunnerClaude), Model: "claude-fallback", Effort: "medium"},
		"explicit":         {Harness: string(RunnerCodexExec), Model: "gpt-explicit", Effort: "low"},
		"execute-frontier": {Harness: string(RunnerCodexExec), Model: "gpt-frontier", Effort: "max"},
	}, ModelLevels: map[string]ModelLevelDefinition{"standard": {Execute: []string{"primary", "fallback"}, Review: []string{"primary", "fallback"}}, "demanding": {Execute: []string{"primary"}}}}

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
	note.Data["complexity"] = "frontier"
	selected, err = resolveRunnerProfileForNote(note, wf, runLaneReview)
	if err != nil || selected.Name != "primary" || !strings.Contains(selected.Source, "standard") {
		t.Fatalf("review did not inherit explicit work level: %#v err=%v", selected, err)
	}
	delete(note.Data, "work_level")
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
	originalAppServer := runnerCatalogAppServerModels
	t.Cleanup(func() { runnerCatalogCommand, runnerCatalogAppServerModels = original, originalAppServer })
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "codex" && len(args) > 1 && args[0] == "debug" {
			return []byte(`{"models":[{"slug":"gpt-live","supported_reasoning_levels":[{"effort":"low"},{"effort":"high"}],"default_reasoning_level":"high"}]}`), nil
		}
		if name == "codex" {
			return []byte("codex-test"), nil
		}
		return nil, errTestCommandUnavailable
	}
	runnerCatalogAppServerModels = func(context.Context) ([]RunnerCatalogModel, error) {
		return []RunnerCatalogModel{{Model: "gpt-live", Efforts: []string{"high", "low"}, DefaultEffort: "high"}}, nil
	}
	catalog := discoverRunnerCatalog(false)
	if !catalog.Harnesses[0].Available || catalog.Harnesses[0].Source != "live" || strings.Join(catalog.Harnesses[0].Models[0].Efforts, ",") != "high,low" {
		t.Fatalf("catalog provenance/reasoning = %#v", catalog.Harnesses[0])
	}
	if catalog.Harnesses[1].Available || catalog.Harnesses[1].Error == "" {
		t.Fatalf("unsupported discovery was invented: %#v", catalog.Harnesses[1])
	}
}

func TestGeneratedProfileDisplayNameUsesAgentAndModel(t *testing.T) {
	if got := generatedProfileDisplayName(string(RunnerCodexExec), "gpt-5.6-luna", "codex_exec_gpt_5_6_luna"); got != "Codex · 5.6 Luna" {
		t.Fatalf("generated name = %q", got)
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

func TestModelLevelsProfileLifecycleReferencesAndSnapshot(t *testing.T) {
	vault := automationTestVault(t)
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	initial, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	set := Args{"vault": vault, "scope": "global", "name": "manual", "display-name": "Manual reviewer", "eligible-tiers": "light,standard", "harness": "codex_exec", "model": "gpt-manual", "effort": "high", "preset": "workspace-write-offline", "if-revision": initial.Revision, "_no-output": "true"}
	projectScoped := Args{}
	for key, value := range set {
		projectScoped[key] = value
	}
	projectScoped["scope"] = "project"
	if err := modelsProfileSetCmd(projectScoped); err == nil || !strings.Contains(err.Error(), "only be defined in the global config") {
		t.Fatalf("project-scoped profile definition must be rejected: %v", err)
	}
	if err := modelsProfileSetCmd(set); err != nil {
		t.Fatal(err)
	}
	created, _ := modelLevelsRead(vault)
	if err := modelsSetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "profiles": "manual", "if-revision": created.Revision, "_no-output": "true"}); err != nil {
		t.Fatal(err)
	}
	referenced, _ := modelLevelsRead(vault)
	err = modelsProfileLifecycleCmd(Args{"vault": vault, "scope": "global", "name": "manual", "if-revision": referenced.Revision, "_no-output": "true"}, "profile-remove")
	if err == nil || !strings.Contains(err.Error(), "still referenced by model_levels.standard.execute") {
		t.Fatalf("referenced profile removal must be refused: %v", err)
	}
	if err := modelsResetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "if-revision": referenced.Revision, "_no-output": "true"}); err != nil {
		t.Fatal(err)
	}
	unreferenced, _ := modelLevelsRead(vault)
	if err := modelsProfileLifecycleCmd(Args{"vault": vault, "scope": "global", "name": "manual", "if-revision": unreferenced.Revision, "_no-output": "true"}, "profile-remove"); err != nil {
		t.Fatal(err)
	}
	removed, _ := modelLevelsRead(vault)
	if _, present := removed.Profiles["manual"]; present || containsString(removed.Levels[1].Execute.Profiles, "manual") {
		t.Fatalf("removed profile still present: %#v", removed)
	}

	disabledSet := Args{"vault": vault, "scope": "global", "name": "manual-disabled", "display-name": "Manual disabled", "eligible-tiers": "light,standard", "harness": "codex_exec", "model": "gpt-manual", "effort": "high", "preset": "workspace-write-offline", "if-revision": removed.Revision, "_no-output": "true"}
	if err := modelsProfileSetCmd(disabledSet); err != nil {
		t.Fatal(err)
	}
	disabledCreated, _ := modelLevelsRead(vault)
	if err := modelsSetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "profiles": "manual-disabled", "if-revision": disabledCreated.Revision, "_no-output": "true"}); err != nil {
		t.Fatal(err)
	}
	disabledReferenced, _ := modelLevelsRead(vault)
	if err := modelsProfileLifecycleCmd(Args{"vault": vault, "scope": "global", "name": "manual-disabled", "if-revision": disabledReferenced.Revision, "_no-output": "true"}, "profile-disable"); err != nil {
		t.Fatal(err)
	}
	disabled, _ := modelLevelsRead(vault)
	if disabled.ProfileStates["manual-disabled"] != "disabled" {
		t.Fatalf("disabled state=%#v", disabled.ProfileStates)
	}
	wf := Workflow{RunnerProfiles: disabled.Profiles, ModelLevels: map[string]ModelLevelDefinition{"standard": {Execute: []string{"manual-disabled", "default"}}}}
	selected, err := resolveRunnerProfileForNote(Note{Data: map[string]any{"work_level": "standard"}}, wf, runLaneExecute)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := resolvedProfileCandidates(selected, wf)
	if err != nil {
		t.Fatal(err)
	}
	chosen, reason, err := selectModelProfile(candidates, func(profile ResolvedRunnerProfile) (bool, string, error) { return true, "", nil })
	if err != nil || chosen.Name != "default" || !strings.Contains(reason, "disabled") {
		t.Fatalf("disabled fallback=%#v %q %v", chosen, reason, err)
	}
	snapshot := preserveResolvedRunIdentity(RunStatus{Lane: runLaneExecute, AttemptCount: 1, RunnerProfile: "manual-disabled", RunnerHarness: "codex_exec", RunnerModel: "gpt-old", RunnerEffort: "high"}, runLaneExecute, chosen)
	if snapshot.Name != "manual-disabled" || snapshot.Definition.Model != "gpt-old" {
		t.Fatalf("snapshot changed=%#v", snapshot)
	}
}

func TestAgentProfileMigrationMembershipAndGuards(t *testing.T) {
	vault := automationTestVault(t)
	global := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("TUSKER_CONFIG", global)
	legacy := `automation:
  profiles:
    legacy-local:
      harness: codex_exec
      model: gpt-legacy
      effort: high
      permission_preset: workspace-write-offline
      sandbox: {mode: workspace-write, network: false}
      subagents: {allowed: false}
`
	if err := writeConfigTextAtomically(global, legacy); err != nil {
		t.Fatal(err)
	}
	if err := writeConfigTextAtomically(managedTuskerLocalConfigPath(vault), "automation:\n  model_levels:\n    light:\n      execute: [legacy-local]\n"); err != nil {
		t.Fatal(err)
	}
	report, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	profile := report.Profiles["legacy-local"]
	if profile.DisplayName != "Codex · Legacy" || strings.Join(profile.EligibleTiers, ",") != "light" || profile.PermissionPreset != "workspace-write-offline" {
		t.Fatalf("legacy migration changed identity, membership, or access: %#v", profile)
	}
	edit := Args{"vault": vault, "scope": "global", "name": "legacy-local", "display-name": "Legacy worker", "eligible-tiers": "standard", "harness": "codex_exec", "model": "gpt-legacy", "effort": "high", "preset": "workspace-write-offline", "if-revision": report.Revision, "_no-output": "true"}
	if err := modelsProfileSetCmd(edit); err == nil || !strings.Contains(err.Error(), "still assigned to light") {
		t.Fatalf("referenced membership removal=%v", err)
	}
	if err := modelsSetCmd(Args{"vault": vault, "scope": "project", "level": "standard", "lane": "execute", "profiles": "legacy-local", "if-revision": report.Revision, "_no-output": "true"}); err == nil || !strings.Contains(err.Error(), "not eligible") {
		t.Fatalf("ineligible assignment=%v", err)
	}
	edit["eligible-tiers"] = "light,standard,demanding"
	if err := modelsProfileSetCmd(edit); err != nil {
		t.Fatal(err)
	}
	after, _ := modelLevelsRead(vault)
	if got := strings.Join(after.Profiles["legacy-local"].EligibleTiers, ","); got != "light,standard,demanding" {
		t.Fatalf("many-to-many membership did not round trip: %s", got)
	}
}

var errTestCommandUnavailable = &testModelLevelError{}

type testModelLevelError struct{}

func (*testModelLevelError) Error() string { return "unavailable" }

// A global profile referenced only by another registered project's tier
// mapping must not be removable from this project (it would silently empty
// that project's mapping).
func TestModelLevelsProfileRemoveRefusesOtherProjectReference(t *testing.T) {
	vault := automationTestVault(t)
	other := pickupV7TestVault(t)
	setGlobalProfileForTest(t, "shared", directEmergencyRunnerProfileForTest())
	setGlobalProfileForTest(t, "shared.eligible_tiers", []string{"light", "standard"})
	project := registerAutomationTestProject(t, other)
	if _, err := setProjectLocalConfigWithReadback(other, "automation.model_levels.standard.execute", []string{"shared"}); err != nil {
		t.Fatal(err)
	}
	current, err := modelLevelsRead(vault)
	if err != nil {
		t.Fatal(err)
	}
	err = modelsProfileLifecycleCmd(Args{"vault": vault, "scope": "global", "name": "shared", "if-revision": current.Revision, "_no-output": "true"}, "profile-remove")
	if err == nil || !strings.Contains(err.Error(), project.ProjectID+": model_levels.standard.execute") {
		t.Fatalf("cross-project reference must block removal: %v", err)
	}
	if after, _ := modelLevelsRead(vault); after.Profiles["shared"].Harness == "" {
		t.Fatal("refused removal still deleted the profile")
	}
}

// Global-scope model settings writes from two projects must serialize on the
// shared global config, not just each project's own epoch lock.
func TestModelSettingsGlobalLockSpansProjects(t *testing.T) {
	t.Setenv("TUSKER_CONFIG", filepath.Join(t.TempDir(), "config.yaml"))
	first, second := pickupV7TestVault(t), pickupV7TestVault(t)
	held, err := acquireModelSettingsLock(first, "global")
	if err != nil {
		t.Fatal(err)
	}
	if lock, err := acquireModelSettingsLock(second, "project"); err != nil {
		t.Fatalf("project scope must not wait on the global lock: %v", err)
	} else {
		_ = lock.Close()
	}
	acquired := make(chan error, 1)
	go func() {
		lock, err := acquireModelSettingsLock(second, "global")
		if err == nil {
			err = lock.Close()
		}
		acquired <- err
	}()
	select {
	case <-acquired:
		t.Fatal("second project acquired the global model settings lock while held")
	case <-time.After(200 * time.Millisecond):
	}
	_ = held.Close()
	if err := <-acquired; err != nil {
		t.Fatalf("second project global lock after release: %v", err)
	}
}

func TestGlobalProfilesCoverAllTiersIgnoresReadOnlyProfiles(t *testing.T) {
	all := []any{"light", "standard", "demanding"}
	for name, profile := range map[string]map[string]any{
		"preset":  {"eligible_tiers": all, "permission_preset": "read-only"},
		"sandbox": {"eligible_tiers": all, "sandbox": map[string]any{"mode": "read-only"}},
		"access":  {"eligible_tiers": all, "access": map[string]any{"mode": "review_only"}},
	} {
		if globalProfilesCoverAllTiers(map[string]any{name: profile}) {
			t.Fatalf("%s read-only profile counted as execution coverage", name)
		}
	}
	if !globalProfilesCoverAllTiers(map[string]any{"worker": map[string]any{"eligible_tiers": all, "permission_preset": "workspace-write-offline"}}) {
		t.Fatal("writable profile must count as coverage")
	}
}
