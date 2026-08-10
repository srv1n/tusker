package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	plan := factoryBootstrapDogfoodPlan()
	plan.Concurrency = 1
	context, err := buildDeliveryPlanningContextForScope(vault, strings.Join(plan.SpecRefs, ","), plan.Scope)
	if err != nil {
		t.Fatal(err)
	}
	plan.ContextFingerprint = context.ContextFingerprint
	plan.FactoryIntakeContractSchema = context.PlanContract.FactoryIntakeContract.Schema
	plan.FactoryIntakeContractVersion = context.PlanContract.FactoryIntakeContract.Version
	plan.FactoryIntakeContractFingerprint = context.PlanContract.FactoryIntakeContract.Fingerprint
	planPath := writeDeliveryV2TestPlan(t, vault, plan)
	doctor, err := deliveryPlanDoctor(vault, planPath)
	if err != nil || !doctor.OK {
		t.Fatalf("doctor rejected dogfood plan: report=%#v err=%v", doctor, err)
	}
	if got := doctor.Frontiers; len(got) != 4 || strings.Join(got[0], ",") != "goodbye,hello" || strings.Join(got[1], ",") != "router" || strings.Join(got[2], ",") != "docs,e2e" || strings.Join(got[3], ",") != "integration-gate" || doctor.Concurrency != 1 {
		t.Fatalf("unexpected deterministic frontiers: %#v concurrency=%d", got, doctor.Concurrency)
	}
	automationOff := greenWaveEnvironment()
	automationOff.ProjectEnabled = false
	review, err := buildDeliveryReviewWithInspector(vault, planPath, fixedWaveEnvironmentInspector(automationOff))
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderDeliveryReview(review)
	if review.Start.Readiness != "blocked" || review.Start.StateLabel != "Project automation is off" || !strings.Contains(rendered, "What will be delivered") || !strings.Contains(rendered, "How it will be proven") || !strings.Contains(rendered, "How work flows") {
		t.Fatalf("review did not truthfully render the automation-off boundary: %#v\n%s", review.Start, rendered)
	}
	dryRun := captureStdout(t, func() {
		if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "dry-run": "true", "json": "true"}); err != nil {
			t.Fatal(err)
		}
	})
	var dryPayload struct {
		Delivery deliveryImportReport `json:"delivery"`
		Inert    bool                 `json:"inert"`
		OK       bool                 `json:"ok"`
	}
	if err := json.Unmarshal([]byte(dryRun), &dryPayload); err != nil || !dryPayload.OK || !dryPayload.Inert || !dryPayload.Delivery.DryRun || dryPayload.Delivery.ExpectedConcurrency != 1 || len(dryPayload.Delivery.Frontiers) != 4 {
		t.Fatalf("dry run omitted deterministic frontier/concurrency report: payload=%#v err=%v", dryPayload, err)
	}
	if err := deliveryImportCmd(Args{"vault": vault, "plan": planPath, "quiet": "true"}); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ id, complexity, lane, profile, model string }{
		{"DOG-T-0001", "routine", runLaneExecute, "execute-fast", "gpt-5.6-luna"},
		{"DOG-T-0002", "standard", runLaneExecute, "execute-standard", "gpt-5.6-terra"},
		{"DOG-T-0003", "complex", runLaneExecute, "execute-complex", "gpt-5.6-terra"},
		{"DOG-T-0005", "standard", runLaneReview, "review-independent", "gpt-5.6-terra"},
	} {
		note, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "tasks", check.id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		preview := routePreviewForNote(Note{Data: note}, wf, check.lane)
		if preview.Profile != check.profile || preview.Model != check.model || preview.Source != "task complexity" || len(preview.Blockers) != 0 {
			t.Fatalf("route %s: %#v", check.id, preview)
		}
	}
	wave, _, err := parseFrontmatterMustRead(filepath.Join(vault, "work", "waves", "W-0001.md"))
	if err != nil {
		t.Fatal(err)
	}
	if stringField(wave, "status") != "open" || stringField(wave, "authorization") != "disarmed" {
		t.Fatalf("imported wave gained authority: %#v", wave)
	}
	if got := gitDirOutput(t, repo, "show-ref"); got != beforeRefs || strings.TrimSpace(gitDirOutput(t, repo, "rev-parse", "refs/heads/main")) != beforeMain {
		t.Fatal("inert import moved or created a Git ref")
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

func factoryBootstrapDogfoodPlan() deliveryPlanV2 {
	plan := deliveryPlanV2{
		Schema: deliveryPlanV2Schema, Scope: "factory-bootstrap-disposable-dogfood/v1", Title: "Factory bootstrap disposable dogfood",
		EpicContract: &deliveryEpicContract{SourceKey: "factory-bootstrap-dogfood", AcronymHint: "DOG", Title: "Factory bootstrap disposable dogfood", Domains: []string{"project"}},
		SpecRefs:     []string{"docs/specs/factory-bootstrap-dogfood.md"},
		Summary:      "Prove a held automation-off bootstrap journey without daemon, model, release, spend, or ref authority.", Concurrency: 2,
		Requirements: []deliveryRequirement{{"R1", "Catalog provenance is observable without a model launch."}, {"R2", "Generated profiles are deterministic and editable."}, {"R3", "Automation remains disabled."}, {"R4", "Complexity stays model-neutral."}, {"R5", "The V2 plan is source-keyed."}, {"R6", "Route preview is read-only."}, {"R7", "The disposable journey remains inert."}},
	}
	newTask := func(key, req, title, outcome, complexity, artifact string, deps ...string) deliveryPlanTask {
		t := deliveryPlanTask{SourceKey: key, RequirementRefs: []string{req}, Title: title, Outcome: outcome, Complexity: complexity, Acceptance: []deliveryAcceptance{{ID: "A1", Outcome: outcome}}, Verification: []deliveryVerification{{Covers: "A1", Check: "command: go test ./cmd/tusker -run '^TestFactoryBootstrapDisposableDogfood$' -count=1"}}, Artifact: deliveryArtifactContract{Kind: "behavior_matrix", Path: artifact, Summary: outcome, AcceptanceIDs: []string{"A1"}}, OwnedPaths: []string{artifact}, Risk: "low", Priority: "p0", Size: "s", Domains: []string{"project"}}
		for _, dep := range deps {
			t.Dependencies = append(t.Dependencies, deliveryDependency{Task: dep, Kind: "hard"})
		}
		return t
	}
	plan.Tasks = []deliveryPlanTask{
		newTask("hello", "R1", "Inspect catalog", "A visible lower-tier catalog is observed without execution.", "routine", "cmd/tusker/runner_catalog.go"),
		newTask("goodbye", "R2", "Generate profiles", "Seven semantic profiles select lower-tier execution defaults.", "standard", "cmd/tusker/runner_profiles.go"),
		newTask("router", "R4", "Route semantic work", "Complexity routes through profiles without a provider model in the task contract.", "complex", "cmd/tusker/runner_route_preview.go", "hello", "goodbye"),
		newTask("docs", "R5", "Document V2 plan", "The source-keyed V2 contract renders durable product flow.", "standard", "docs/specs/factory-bootstrap-dogfood.md", "router"),
		newTask("e2e", "R6", "Preview route", "Read-only review routing selects an independent profile.", "standard", "cmd/tusker/runner_route_preview_test.go", "router"),
		newTask("integration-gate", "R7", "Audit held import", "The final held audit proves no daemon, run, release, spend, or ref movement.", "complex", "cmd/tusker/factory_bootstrap_dogfood_test.go", "docs", "e2e"),
	}
	// R3 is intentionally covered by the final inert audit as well as bootstrap.
	plan.Tasks[5].RequirementRefs = []string{"R3", "R7"}
	return plan
}
