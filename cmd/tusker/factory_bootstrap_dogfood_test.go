package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// This is deliberately an in-process product journey: the catalog command is
// replaced, but no runner executable, auth flow, network call, or model is
// reachable from the fixture.
func TestFactoryBootstrapDisposableDogfood(t *testing.T) {
	stateRoot := filepath.Join(t.TempDir(), "fresh-runtime")
	t.Setenv("TUSKER_STATE_ROOT", stateRoot)
	originalCatalog := runnerCatalogCommand
	t.Cleanup(func() { runnerCatalogCommand = originalCatalog })
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		switch name {
		case "codex":
			if len(args) == 1 && args[0] == "--version" {
				return []byte("codex 0.99.0-test\n"), nil
			}
			return []byte(`{"models":[{"slug":"gpt-5.6-luna","visibility":"visible","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"}]},{"slug":"gpt-5.6-terra","visibility":"visible","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}]},{"slug":"gpt-5.6-sol","visibility":"visible","default_reasoning_level":"xhigh","supported_reasoning_levels":[{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]},{"slug":"auto-review","visibility":"hidden","supported_reasoning_levels":[{"effort":"low"}]}]}`), nil
		case "claude":
			return []byte("claude 0.99.0-test\n"), nil
		default:
			return nil, errCatalogFixture{}
		}
	}

	repo := t.TempDir()
	vault := filepath.Join(repo, ".tusker")
	if err := bootstrap(Args{"vault": vault, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultWorkflow(vault); err != nil {
		t.Fatal(err)
	}
	if _, err := setProjectLocalConfigWithReadback(vault, "automation.concurrency.max_active_runs_per_project", 2); err != nil {
		t.Fatal(err)
	}
	// Start the reconcile half of the journey with an intentionally incomplete,
	// user-owned policy. The bootstrapper may add the missing semantic roles, but
	// it must leave this profile, default, and routing rule byte-for-byte stable
	// in their canonical JSON representation.
	userOwnedProfile := RunnerProfileDefinition{
		Harness: string(RunnerCodexExec), Model: "gpt-5.6-luna", Effort: "medium", PermissionPreset: "workspace-write-offline",
		Sandbox:   RunnerSandboxDefinition{Mode: "workspace-write", Network: boolPtr(false)},
		Subagents: RunnerSubagentPolicyDefinition{Allowed: boolPtr(false), MaxConcurrent: 0},
	}
	userOwnedRouting := RunnerRoutingRule{Name: "user-owned-high-risk", Profile: "user-owned", Match: RunnerRoutingMatch{Risk: "high"}}
	userOwnedPolicy := struct {
		Profile RunnerProfileDefinition `json:"profile"`
		Default string                  `json:"default"`
		Routing RunnerRoutingRule       `json:"routing"`
	}{Profile: userOwnedProfile, Default: "user-owned", Routing: userOwnedRouting}
	beforeUserOwnedPolicy, err := json.Marshal(userOwnedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeText(managedTuskerConfigPath(vault), `schema: tusker.config/v1
project_id: fresh-runtime
automation:
  enabled: false
  default_profile: user-owned
  routing:
    - name: user-owned-high-risk
      profile: user-owned
      match:
        risk: high
  profiles:
    user-owned:
      harness: codex_exec
      model: gpt-5.6-luna
      effort: medium
      permission_preset: workspace-write-offline
      sandbox:
        mode: workspace-write
        network: false
      subagents:
        allowed: false
        max_concurrent: 0
`); err != nil {
		t.Fatal(err)
	}
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	runGitDir(t, repo, "init", "-b", "main")
	runGitDir(t, repo, "config", "user.email", "dogfood@example.invalid")
	runGitDir(t, repo, "config", "user.name", "Tusker Dogfood")
	runGitDir(t, repo, "add", ".")
	runGitDir(t, repo, "commit", "-m", "bootstrap disposable dogfood")
	beforeRefs := gitDirOutput(t, repo, "show-ref")
	beforeMain := strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "refs/heads/main"))

	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Config.Automation.Enabled == nil || *resolved.Config.Automation.Enabled || resolved.Config.Automation.DefaultProfile != "user-owned" {
		t.Fatalf("reconcile granted authority or replaced user default: %#v", resolved.Config.Automation)
	}
	profiles := runnerProfilesFromSchema(resolved.Config.Automation.Profiles)
	userOwnedAfter, ok := profiles["user-owned"]
	routingAfter := runnerRoutingFromSchema(resolved.Config.Automation.Routing)
	if !ok || len(routingAfter) != 1 {
		t.Fatalf("reconcile lost user-owned policy: %#v", resolved.Config.Automation)
	}
	afterUserOwnedPolicy, err := json.Marshal(struct {
		Profile RunnerProfileDefinition `json:"profile"`
		Default string                  `json:"default"`
		Routing RunnerRoutingRule       `json:"routing"`
	}{Profile: userOwnedAfter, Default: resolved.Config.Automation.DefaultProfile, Routing: routingAfter[0]})
	if err != nil {
		t.Fatal(err)
	}
	if string(afterUserOwnedPolicy) != string(beforeUserOwnedPolicy) {
		t.Fatalf("reconcile replaced user-owned policy:\n before=%s\n  after=%s", beforeUserOwnedPolicy, afterUserOwnedPolicy)
	}
	semanticRoles := []string{"planner", "execute-fast", "execute-standard", "execute-complex", "execute-frontier", "review-independent", "repair-complex"}
	for _, role := range semanticRoles {
		if _, ok := profiles[role]; !ok {
			t.Fatalf("missing generated semantic profile %q: %#v", role, profiles)
		}
	}
	for _, check := range []struct{ role, model string }{
		{"execute-fast", "gpt-5.6-luna"}, {"execute-standard", "gpt-5.6-terra"}, {"execute-complex", "gpt-5.6-terra"},
		{"planner", "gpt-5.6-sol"}, {"execute-frontier", "gpt-5.6-sol"}, {"review-independent", "gpt-5.6-terra"}, {"repair-complex", "gpt-5.6-terra"},
	} {
		if got := profiles[check.role].Model; got != check.model {
			t.Fatalf("%s selected %q, want %q", check.role, got, check.model)
		}
	}
	for _, profile := range profiles {
		if profile.Model == "auto-review" {
			t.Fatal("hidden catalog model was selected")
		}
	}
	wf := defaultWorkflow()
	wf.AutomationEnabled = false
	wf.RunnerProfiles = profiles
	wf.RunnerDefaultProfile = resolved.Config.Automation.DefaultProfile
	wf.RunnerRouting = routingAfter
	wf.ModelLevels = map[string]ModelLevelDefinition{
		"light":     {Execute: []string{"execute-fast"}, Review: []string{"review-independent"}},
		"standard":  {Execute: []string{"execute-standard"}, Review: []string{"review-independent"}},
		"demanding": {Execute: []string{"execute-complex"}, Review: []string{"review-independent"}},
	}
	userRoute := routePreviewForNote(Note{Data: map[string]any{
		"id": "DOG-T-USER", "title": "Preserve explicit policy", "risk": "high",
	}}, wf, runLaneExecute)
	if userRoute.Profile != "user-owned" || userRoute.Source != "automation.routing" || userRoute.Rule != "user-owned-high-risk" || userRoute.Model != "gpt-5.6-luna" || len(userRoute.Blockers) != 0 {
		t.Fatalf("reconcile preserved dead routing text instead of effective policy: %#v", userRoute)
	}

	spec := filepath.Join(repo, "docs", "specs", "factory-bootstrap-dogfood.md")
	if err := writeText(spec, "# Factory bootstrap disposable dogfood\n\n## Requirements\n\n- R1 through R7 are proven by the held, automation-off fixture.\n"); err != nil {
		t.Fatal(err)
	}
	if err := newV7Epic(Args{"vault": vault, "quiet": "true", "acronym": "DOG", "title": "Factory bootstrap disposable dogfood"}); err != nil {
		t.Fatal(err)
	}
	requestPath := writeDogfoodAuthoringRequest(t, vault)
	created := captureStdout(t, func() {
		if err := waveV7CreateCmd(Args{"vault": vault, "file": requestPath, "request-key": "dogfood", "by": "agent:dogfood", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var createPayload struct {
		Wave struct {
			TaskMapping         map[string]string `json:"taskMapping"`
			Frontiers           [][]string        `json:"frontiers"`
			ExpectedConcurrency int               `json:"expectedConcurrency"`
		} `json:"wave"`
		Inert bool `json:"inert"`
		OK    bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(created), &createPayload); err != nil || !createPayload.OK || !createPayload.Inert {
		t.Fatalf("authoring did not report an inert create: payload=%#v err=%v", createPayload, err)
	}
	if got := createPayload.Wave.Frontiers; len(got) != 4 || len(got[0]) != 2 || createPayload.Wave.ExpectedConcurrency != 1 {
		t.Fatalf("unexpected deterministic frontiers: %#v concurrency=%d", got, createPayload.Wave.ExpectedConcurrency)
	}
	review, err := buildDirectWaveReview(vault, nil, "", "W-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	if review.Authorization != "inert" || len(review.Members) != 7 {
		t.Fatalf("authored wave gained authority or lost members: %#v", review)
	}
	for _, check := range []struct{ id, level, lane, profile, model string }{
		{"DOG-T-0001", "light", runLaneExecute, "execute-fast", "gpt-5.6-luna"},
		{"DOG-T-0002", "standard", runLaneExecute, "execute-standard", "gpt-5.6-terra"},
		{"DOG-T-0003", "demanding", runLaneExecute, "execute-complex", "gpt-5.6-terra"},
		{"DOG-T-0005", "standard", runLaneReview, "review-independent", "gpt-5.6-terra"},
	} {
		note, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", check.id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		preview := routePreviewForNote(Note{Data: note}, wf, check.lane)
		if preview.Profile != check.profile || preview.Model != check.model || !strings.Contains(preview.Source, "model_levels") || len(preview.Blockers) != 0 {
			t.Fatalf("route %s: %#v", check.id, preview)
		}
	}
	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(wave, "status") != "open" || stringField(wave, "authorization") != "disarmed" {
		t.Fatalf("authored wave gained authority: %#v", wave)
	}
	if got := gitDirOutput(t, repo, "show-ref"); got != beforeRefs || strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "refs/heads/main")) != beforeMain {
		t.Fatal("inert authoring moved or created a Git ref")
	}
	if _, err := os.Stat(stateRoot); err == nil {
		store, err := OpenRuntimeStore(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		projects, err := store.ListProjects()
		if err != nil || len(projects) != 0 {
			t.Fatalf("inert flow created daemon authority: projects=%#v err=%v", projects, err)
		}
		runs, err := store.ListRuns()
		if err != nil || len(runs) != 0 {
			t.Fatalf("inert flow created runs: runs=%#v err=%v", runs, err)
		}
		var attempts int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM attempts").Scan(&attempts); err != nil || attempts != 0 {
			t.Fatalf("inert flow created attempts: count=%d err=%v", attempts, err)
		}
	}
}

func writeDogfoodAuthoringRequest(t *testing.T, vault string) string {
	t.Helper()
	newTask := func(key, title, outcome, level, artifact string, deps ...string) map[string]any {
		body := "# " + title + "\n\n## Outcome\n\n" + outcome + "\n\n## Acceptance\n\n| ID | Outcome |\n| --- | --- |\n| A1 | " + outcome + " |\n\n## Verification\n\n| Covers | Check | Result |\n| --- | --- | --- |\n| A1 | command: go test ./cmd/tusker -run '^TestFactoryBootstrapDisposableDogfood$' -count=1 | pending |\n\n## Artifact\n\n- kind: behavior_matrix\n- path: " + artifact + "\n- summary: " + outcome + "\n"
		entry := map[string]any{
			"key": key, "title": title, "work_level": level, "body": body, "epic": "DOG",
			"owned_paths":       []string{artifact},
			"generated_outputs": []string{artifact},
		}
		var edges []map[string]any
		for _, dep := range deps {
			edges = append(edges, map[string]any{"task": dep, "kind": "hard"})
		}
		if len(edges) > 0 {
			entry["dependencies"] = edges
		}
		return entry
	}
	request := map[string]any{
		"schema": "tusker.wave-authoring/v1", "request_key": "dogfood",
		"title":       "Factory bootstrap disposable dogfood",
		"outcome":     "Prove a held automation-off bootstrap journey without daemon, model, release, spend, or ref authority.",
		"spec_refs":   []string{"docs/specs/factory-bootstrap-dogfood.md"},
		"concurrency": 1,
		"tasks": []map[string]any{
			newTask("hello", "Inspect catalog", "A visible lower-tier catalog is observed without execution.", "light", "cmd/tusker/runner_catalog.go"),
			newTask("goodbye", "Generate profiles", "Seven semantic profiles select lower-tier execution defaults.", "standard", "cmd/tusker/runner_profiles.go"),
			newTask("router", "Route semantic work", "Work levels route through profiles without a provider model in the task contract.", "demanding", "cmd/tusker/runner_route_preview.go", "hello", "goodbye"),
			newTask("docs", "Document direct authoring", "The source-keyed authoring contract renders durable product flow.", "standard", "docs/specs/factory-bootstrap-dogfood.md", "router"),
			newTask("e2e", "Preview route", "Read-only review routing selects an independent profile.", "standard", "cmd/tusker/runner_route_preview_test.go", "router"),
			newTask("integration-gate", "Audit held authoring", "The final held audit proves no daemon, run, release, spend, or ref movement.", "demanding", "cmd/tusker/factory_bootstrap_dogfood_test.go", "docs", "e2e"),
		},
	}
	raw, err := yaml.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(v7RepoRoot(vault), ".tusker", "scratch", "dogfood-wave.yaml")
	if err := writeText(path, string(raw)); err != nil {
		t.Fatal(err)
	}
	return path
}
