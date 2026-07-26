package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunnerCatalogParsesInstalledCodexShape(t *testing.T) {
	models := parseCodexModels([]byte(`{"models":[{"slug":"gpt-5.2","visibility":"visible","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"xhigh"}],"service_tiers":[{"id":"priority"}]}]}`))
	if len(models) != 1 || models[0].Model != "gpt-5.2" || !catalogContainsString(models[0].Efforts, "xhigh") || !catalogContainsString(models[0].ServiceTiers, "priority") {
		t.Fatalf("unexpected parsed models: %#v", models)
	}
}

func TestRunnerProfileBootstrap(t *testing.T) {
	vault := automationTestVault(t)
	root := filepath.Dir(vault)
	path := filepath.Join(root, "tusker.yaml")
	original := "schema: tusker.config/v1\nproject_id: app\nautomation:\n  enabled: true\n  default_profile: custom\n  routing:\n    - name: keep\n      profile: custom\n  profiles:\n    custom:\n      harness: codex_exec\n      model: gpt-5.x\n      effort: low\n      sandbox: {mode: workspace-write, network: false}\n      subagents: {allowed: false, max_concurrent: 0}\n"
	if err := writeText(path, original); err != nil {
		t.Fatal(err)
	}
	originalCommand := runnerCatalogCommand
	defer func() { runnerCatalogCommand = originalCommand }()
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		return []byte(`{"models":[{"slug":"gpt-5-terra","visibility":"visible","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]}]}`), nil
	}
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "json": "true"}); err != nil {
		t.Fatal(err)
	}
	after, err := readText(path)
	if err != nil || after != original {
		t.Fatalf("preview mutated config: %v %q", err, after)
	}
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	after, err = readText(path)
	if err != nil || !strings.Contains(after, "default_profile: custom") || !strings.Contains(after, "enabled: true") || !strings.Contains(after, "execute-standard") || !strings.Contains(after, "name: keep") {
		t.Fatalf("write failed to preserve policy: %v %s", err, after)
	}
	if _, err := loadWorkflow(vault); err != nil {
		t.Fatalf("written bootstrap config is invalid: %v", err)
	}
}

func TestRunnerProfileBootstrapFreshInitIncludesAllRoles(t *testing.T) {
	original := runnerCatalogCommand
	defer func() { runnerCatalogCommand = original }()
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "codex" && len(args) > 0 && args[0] == "debug" {
			return []byte(`{"models":[{"slug":"codex-auto-review","visibility":"hide","supported_reasoning_levels":[{"effort":"high"}]},{"slug":"gpt-5-terra","visibility":"visible","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"}]}]}`), nil
		}
		return []byte("version"), nil
	}
	vault := automationTestVault(t)
	if err := os.Remove(filepath.Join(filepath.Dir(vault), "tusker.yaml")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := writeDefaultRootTuskerConfig(vault); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.AutomationEnabled {
		t.Fatal("fresh init enabled automation")
	}
	if wf.Data.RunnerDefaultProfile != "execute-standard" {
		t.Fatalf("default=%q", wf.Data.RunnerDefaultProfile)
	}
	for _, name := range []string{"planner", "execute-fast", "execute-standard", "execute-complex", "execute-frontier", "review-independent", "repair-complex"} {
		profile, ok := wf.Data.RunnerProfiles[name]
		if !ok || profile.Model == "codex-auto-review" {
			t.Fatalf("bad %s profile: %#v", name, profile)
		}
	}
}

func TestRunnerCatalogHarnessIsolationAndBundled(t *testing.T) {
	original := runnerCatalogCommand
	defer func() { runnerCatalogCommand = original }()
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "--bundled") {
			return []byte(`{"models":[{"slug":"gpt-5.2","supported_reasoning_levels":[{"effort":"low"}]}]}`), nil
		}
		return nil, errCatalogFixture{}
	}
	catalog := discoverRunnerCatalog(false)
	if catalog.Harnesses[0].Error == "" || catalog.Harnesses[1].Source != "declared" {
		t.Fatalf("expected isolated codex failure and declared claude: %#v", catalog)
	}
	catalog = discoverRunnerCatalog(true)
	if catalog.Harnesses[0].Source != "bundled" || len(catalog.Harnesses[0].Models) != 1 {
		t.Fatalf("expected bundled catalog: %#v", catalog.Harnesses[0])
	}
}

func TestRunnerProfileEffortAndComplexity(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "max", "ultra"} {
		if !validRunnerEffort(effort) {
			t.Fatalf("expected %s to be valid", effort)
		}
	}
	if validRunnerEffort("turbo") {
		t.Fatal("unknown effort accepted")
	}
	profiles := semanticBootstrapProfiles(RunnerCatalog{Harnesses: []RunnerCatalogHarness{{Harness: "codex_exec", Source: "live", Available: true, Models: []RunnerCatalogModel{{Model: "hidden", Hidden: true, Efforts: []string{"low"}}, {Model: "gpt-5-terra", Default: true, Efforts: []string{"low", "medium", "high", "xhigh"}}}}}})
	if profiles["review-independent"].(map[string]any)["permission_preset"] != "read-only" {
		t.Fatal("review profile must be read-only")
	}
}

func TestSemanticBootstrapProfilesClaudeOnly(t *testing.T) {
	profiles := semanticBootstrapProfiles(RunnerCatalog{Harnesses: []RunnerCatalogHarness{
		{Harness: "codex_exec", Source: "live", Available: false},
		{Harness: "claude-code", Source: "declared", Available: true, Models: []RunnerCatalogModel{
			{Model: "fable", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
			{Model: "opus", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
			{Model: "sonnet", Efforts: []string{"low", "medium", "high", "xhigh", "max"}},
		}},
	}})
	for role, wantModel := range map[string]string{
		"execute-fast":       "sonnet",
		"execute-standard":   "sonnet",
		"planner":            "fable",
		"execute-frontier":   "fable",
		"execute-complex":    "opus",
		"review-independent": "opus",
		"repair-complex":     "opus",
	} {
		profile, ok := profiles[role].(map[string]any)
		if !ok || profile["harness"] != "claude-code" || profile["model"] != wantModel {
			t.Fatalf("%s = %#v, want claude-code/%s", role, profile, wantModel)
		}
	}
}

func TestSemanticBootstrapProfilesNoUsableHarness(t *testing.T) {
	catalog := RunnerCatalog{Harnesses: []RunnerCatalogHarness{
		{Harness: "codex_exec", Source: "live", Available: true, Models: []RunnerCatalogModel{{Model: "gpt-5-terra", Efforts: []string{"invalid"}}}},
		{Harness: "claude-code", Source: "declared", Available: false, Models: []RunnerCatalogModel{{Model: "sonnet", Efforts: []string{"medium"}}}},
	}}
	if profiles := semanticBootstrapProfiles(catalog); len(profiles) != 0 {
		t.Fatalf("profiles=%#v, want none without a usable harness", profiles)
	}
}

func TestSemanticEffortForNearestSupportedLevel(t *testing.T) {
	for _, tc := range []struct {
		name, harness, want string
		supported           []string
		got                 string
	}{
		{"exact", "codex_exec", "high", []string{"low", "high"}, "high"},
		{"tie prefers lower", "codex_exec", "high", []string{"medium", "xhigh"}, "medium"},
		{"nearest lower", "codex_exec", "xhigh", []string{"low", "medium", "high"}, "high"},
		{"nearest higher", "codex_exec", "medium", []string{"xhigh"}, "xhigh"},
		{"claude rejects ultra", "claude-code", "ultra", []string{"ultra", "max"}, "max"},
		{"no valid effort", "codex_exec", "medium", []string{"turbo"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := semanticEffortFor(tc.harness, tc.want, tc.supported); got != tc.got {
				t.Fatalf("semanticEffortFor(%q, %q, %q) = %q, want %q", tc.harness, tc.want, tc.supported, got, tc.got)
			}
		})
	}
}

func TestFreshBootstrapWithoutUsableHarnessOmitsDefaultProfile(t *testing.T) {
	original := runnerCatalogCommand
	defer func() { runnerCatalogCommand = original }()
	runnerCatalogCommand = func(string, ...string) ([]byte, error) { return nil, errCatalogFixture{} }
	vault := automationTestVault(t)
	path := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := writeDefaultRootTuskerConfig(vault); err != nil {
		t.Fatal(err)
	}
	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "default_profile: execute-standard") {
		t.Fatalf("bootstrap fabricated default profile:\n%s", raw)
	}
}

func TestProfileReconcileWithoutUsableHarnessOmitsDefaultProfile(t *testing.T) {
	original := runnerCatalogCommand
	defer func() { runnerCatalogCommand = original }()
	runnerCatalogCommand = func(string, ...string) ([]byte, error) { return nil, errCatalogFixture{} }
	vault := automationTestVault(t)
	path := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	if err := writeText(path, "schema: tusker.config/v1\nproject_id: app\nautomation:\n  enabled: false\n  profiles: {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "default_profile:") {
		t.Fatalf("reconcile invented a default profile:\n%s", raw)
	}
}

func TestRunnerClaudeAliasesAndEffortArguments(t *testing.T) {
	for _, alias := range []string{"fable", "opus", "sonnet"} {
		if !validRunnerModelName(alias) {
			t.Fatalf("alias %q rejected", alias)
		}
	}
	profile := RunnerProfileDefinition{Harness: string(RunnerClaude), Model: "sonnet", Effort: "xhigh", Sandbox: RunnerSandboxDefinition{Mode: "read-only"}}
	if err := validateRunnerProfileDefinition("review", profile, "tusker.yaml"); err != nil {
		t.Fatal(err)
	}
	if command := commandForRunnerProfile("claude -p", ResolvedRunnerProfile{Definition: profile}); !strings.Contains(command, "--model sonnet") || !strings.Contains(command, "--effort xhigh") {
		t.Fatalf("missing Claude args: %q", command)
	}
	profile.Effort = "ultra"
	if err := validateRunnerProfileDefinition("review", profile, "tusker.yaml"); err == nil {
		t.Fatal("Claude ultra effort accepted")
	}
}

type errCatalogFixture struct{}

func (errCatalogFixture) Error() string { return "missing codex" }
