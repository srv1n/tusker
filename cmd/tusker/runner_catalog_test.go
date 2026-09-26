package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tusker/internal/acp"
)

func TestRunnerCatalogCommandBoundsOrphanedOutputPipe(t *testing.T) {
	started := time.Now()
	_, err := runnerCatalogCommand("sh", "-c", "sleep 30 & wait")
	if err == nil {
		t.Fatal("expected catalog command timeout")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("catalog command left an output pipe open for %s", elapsed)
	}
}

func TestRunnerCatalogParsesInstalledCodexShape(t *testing.T) {
	models := parseCodexModels([]byte(`{"models":[{"slug":"gpt-5.2","visibility":"visible","default_reasoning_level":"medium","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"xhigh"}],"service_tiers":[{"id":"priority"}]}]}`))
	if len(models) != 1 || models[0].Model != "gpt-5.2" || !catalogContainsString(models[0].Efforts, "xhigh") || !catalogContainsString(models[0].ServiceTiers, "priority") {
		t.Fatalf("unexpected parsed models: %#v", models)
	}
}

func TestParseDevinModelsKeepsAccountCatalogAndACPOnlyChoices(t *testing.T) {
	models, err := parseDevinModels([]byte(`{"families":[{"variants":[{"model_uid":"swe-2-high","label":"SWE-2 High","max_output_tokens":128000,"cost_summary":"priced"},{"model_uid":"swe-1-7","label":"SWE-1.7 Max"}]}]}`), acp.ConfigOption{
		ID: "model", CurrentValue: "swe-1-6-slow", Options: []acp.ConfigOptionValue{{Value: "swe-1-6-slow", Name: "SWE-1.6 Slow"}},
	})
	if err != nil || len(models) != 3 {
		t.Fatalf("Devin models=%#v err=%v", models, err)
	}
	byID := map[string]RunnerCatalogModel{}
	for _, model := range models {
		byID[model.Model] = model
	}
	if !strings.Contains(byID["swe-2-high"].Description, "128000") || byID["swe-2-high"].DefaultEffort != "high" || byID["swe-1-7"].DefaultEffort != "max" || !byID["swe-1-6-slow"].Default {
		t.Fatalf("Devin models=%#v", models)
	}
}

func TestRunnerCatalogOffersFullAccess(t *testing.T) {
	harness := catalogHarness("codex_exec", "Codex", "supported", "")
	if len(harness.Options) != 1 || !catalogContainsString(harness.Options[0].Values, "danger-full-access") {
		t.Fatalf("full access missing from catalog: %#v", harness.Options)
	}
}

func TestCodexAppServerModelListInitializesAndPaginates(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "requests.jsonl")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'codex-test'; exit 0; fi
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$TUSKER_APP_SERVER_LOG"
  case "$line" in
    *'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;;
    *'"method":"model/list"'*'"cursor":"next"'*) printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"data":[{"id":"two","model":"two","displayName":"Two","description":"second","hidden":false,"isDefault":false,"defaultReasoningEffort":"high","supportedReasoningEfforts":[{"reasoningEffort":"high","description":"deep"}]}]}}' ;;
    *'"method":"model/list"'*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"one","model":"one","displayName":"One","description":"first","hidden":false,"isDefault":true,"defaultReasoningEffort":"medium","supportedReasoningEfforts":[{"reasoningEffort":"low","description":"fast"},{"reasoningEffort":"medium","description":"balanced"}]}],"nextCursor":"next"}}' ;;
  esac
done
`
	path := filepath.Join(root, "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("TUSKER_APP_SERVER_LOG", log)
	models, err := codexAppServerModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Model != "one" || models[0].DefaultEffort != "medium" || !models[0].Default || !catalogContainsString(models[1].Efforts, "high") {
		t.Fatalf("models = %#v", models)
	}
	requests, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(requests), `"method":"initialize"`) || !strings.Contains(string(requests), `"method":"initialized"`) || !strings.Contains(string(requests), `"cursor":"next"`) {
		t.Fatalf("App Server protocol was incomplete: %v\n%s", err, requests)
	}
}

func TestCodexAppServerModelListTimeoutCleansUp(t *testing.T) {
	root := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'codex-test'; exit 0; fi
while IFS= read -r line; do
  case "$line" in *'"method":"initialize"'*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;; esac
done
`
	path := filepath.Join(root, "codex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	originalTimeout := codexAppServerDiscoveryTimeout
	codexAppServerDiscoveryTimeout = 20 * time.Millisecond
	t.Cleanup(func() { codexAppServerDiscoveryTimeout = originalTimeout })
	started := time.Now()
	_, err := codexAppServerModels(context.Background())
	if codexDiscoveryErrorKind(err) != "timeout" || time.Since(started) > time.Second {
		t.Fatalf("timeout was not bounded and classified: err=%v elapsed=%s", err, time.Since(started))
	}
}

func TestResolveCodexExecutableSkipsBrokenPATHShim(t *testing.T) {
	root := t.TempDir()
	broken := filepath.Join(root, "broken")
	working := filepath.Join(root, "working")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "codex"), []byte("#!/bin/sh\necho 'spawn vendor ENOENT' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(working, "codex")
	if err := os.WriteFile(good, []byte("#!/bin/sh\nprintf '%s\\n' 'codex-cli fixture'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("PATH", broken+string(os.PathListSeparator)+working)
	if got := resolveCodexExecutable(); got != good {
		t.Fatalf("resolved Codex = %q, want working shim %q", got, good)
	}
}

func TestCodexAppServerFallsThroughVersionValidUnsupportedShim(t *testing.T) {
	root := t.TempDir()
	unsupported := filepath.Join(root, "unsupported")
	working := filepath.Join(root, "working")
	for _, dir := range []string{unsupported, working} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	unsupportedScript := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'codex-cli old'; exit 0; fi
while IFS= read -r line; do
  case "$line" in *initialize*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;; *model/list*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"method not found"}}' ;; esac
done
`
	workingScript := `#!/bin/sh
if [ "$1" = "--version" ]; then printf '%s\n' 'codex-cli new'; exit 0; fi
while IFS= read -r line; do
  case "$line" in *initialize*) printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{}}' ;; *model/list*) printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"data":[{"id":"working","supportedReasoningEfforts":[{"reasoningEffort":"medium"}]}]}}' ;; esac
done
`
	if err := os.WriteFile(filepath.Join(unsupported, "codex"), []byte(unsupportedScript), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, "codex"), []byte(workingScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("PATH", unsupported+string(os.PathListSeparator)+working)
	models, err := codexAppServerModels(context.Background())
	if err != nil || len(models) != 1 || models[0].Model != "working" {
		t.Fatalf("models=%#v err=%v", models, err)
	}
}

func TestCodexAppServerDiscoveryFailureKinds(t *testing.T) {
	for _, tc := range []struct{ name, message, want string }{
		{"unsupported", "Method not found: model/list", "unsupported"},
		{"authentication", "authentication required", "authentication"},
		{"implementation", "protocol response was malformed", "implementation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexDiscoveryErrorKind(classifyCodexDiscoveryError(context.Background(), errors.New(tc.message), "")); got != tc.want {
				t.Fatalf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAgentCapabilityDiscovery(t *testing.T) {
	originalRoot, originalNow := runnerCatalogStateRoot, runnerCatalogNow
	defer func() { runnerCatalogStateRoot, runnerCatalogNow = originalRoot, originalNow }()
	root := t.TempDir()
	runnerCatalogStateRoot = func() string { return root }
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	runnerCatalogNow = func() time.Time { return now }

	calls := 0
	discover := func() RunnerCatalogHarness {
		calls++
		h := catalogHarness("codex_exec", "Codex", "supported", "setup")
		h.Available, h.DiscoveryState, h.Models = true, "available", []RunnerCatalogModel{{Model: "gpt-real", Efforts: []string{"medium"}}}
		return h
	}
	first := discoverCatalogCached("codex_exec", "1", "cli", "installed", false, discover)
	second := discoverCatalogCached("codex_exec", "1", "cli", "installed", false, discover)
	if calls != 1 || first.Models[0].Model != "gpt-real" || second.DiscoveryState != "available" {
		t.Fatalf("fresh cache mismatch: calls=%d first=%#v second=%#v", calls, first, second)
	}

	now = now.Add(25 * time.Hour)
	failed := discoverCatalogCached("codex_exec", "1", "cli", "installed", true, func() RunnerCatalogHarness {
		h := catalogHarness("codex_exec", "Codex", "supported", "setup")
		h.Error = "token=must-not-leak"
		return h
	})
	if failed.DiscoveryState != "stale" || failed.Models[0].Model != "gpt-real" || strings.Contains(failed.Error, "must-not-leak") {
		t.Fatalf("failed refresh did not retain safe stale data: %#v", failed)
	}

	_ = discoverCatalogCached("codex_exec", "2", "cli", "installed", false, discover)
	_ = discoverCatalogCached("codex_exec", "2", "acp_stdio", "installed", false, discover)
	if calls != 3 {
		t.Fatalf("version/transport cache keys collapsed: calls=%d", calls)
	}

	future := futureCatalogHarness("opencode", "OpenCode")
	if future.Available || future.ManualEntry || future.State != "unsupported" {
		t.Fatalf("future adapter became selectable: %#v", future)
	}

	originalCommand := runnerCatalogCommand
	defer func() { runnerCatalogCommand = originalCommand }()
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "claude" && len(args) > 0 && args[0] == "--version" {
			return []byte("2.0.0 (Claude Code)\n"), nil
		}
		return []byte(`{"loggedIn":true}`), nil
	}
	claude := discoverClaudeCatalog()
	if !claude.Available || !claude.ManualEntry || claude.Group != "supported" || claude.Harness != string(RunnerClaude) || len(claude.Models) == 0 {
		t.Fatalf("Claude Code is not selectable: %#v", claude)
	}
	runnerCatalogCommand = func(string, ...string) ([]byte, error) { return nil, errors.New("not found") }
	if missing := discoverClaudeCatalog(); missing.Available || missing.State != "unsupported" {
		t.Fatalf("missing Claude Code reported available: %#v", missing)
	}
	if options := catalogHarness("codex_exec", "Codex", "supported", "setup").Options; len(options) != 1 || options[0].Kind != "enum" || !catalogContainsString(options[0].Values, "read-only") {
		t.Fatalf("typed permission options missing: %#v", options)
	}
}

// stubRunnerCatalogForTest replaces every discovery seam so bootstrap tests
// never launch a real Codex, Muse, or Devin binary.
func stubRunnerCatalogForTest(t *testing.T, codexModels []RunnerCatalogModel) {
	t.Helper()
	command, appServer, muse, devin, executable, stateRoot := runnerCatalogCommand, runnerCatalogAppServerModels, runnerCatalogMuseServerModels, runnerCatalogDevinModels, runnerCatalogCodexExecutable, runnerCatalogStateRoot
	t.Cleanup(func() {
		runnerCatalogCommand, runnerCatalogAppServerModels, runnerCatalogMuseServerModels, runnerCatalogDevinModels, runnerCatalogCodexExecutable, runnerCatalogStateRoot = command, appServer, muse, devin, executable, stateRoot
	})
	root := t.TempDir()
	runnerCatalogStateRoot = func() string { return root }
	runnerCatalogCodexExecutable = func() string { return "codex" }
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "codex" {
			return []byte("codex-test"), nil
		}
		return nil, errCatalogFixture{}
	}
	runnerCatalogAppServerModels = func(context.Context) ([]RunnerCatalogModel, error) {
		if len(codexModels) == 0 {
			return nil, errCatalogFixture{}
		}
		return codexModels, nil
	}
	runnerCatalogMuseServerModels = func(context.Context) ([]RunnerCatalogModel, error) { return nil, errCatalogFixture{} }
	runnerCatalogDevinModels = func(context.Context) ([]RunnerCatalogModel, error) { return nil, errCatalogFixture{} }
}

func TestRunnerProfileBootstrap(t *testing.T) {
	vault := automationTestVault(t)
	global := setGlobalProfileForTest(t, "custom", map[string]any{"harness": "codex_exec", "model": "gpt-5.x", "effort": "low", "sandbox": map[string]any{"mode": "workspace-write", "network": false}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0}})
	projectPath := managedTuskerConfigPath(vault)
	project := "schema: tusker.config/v1\nproject_id: app\nautomation:\n  enabled: true\n  default_profile: custom\n  routing:\n    - name: keep\n      profile: custom\n"
	if err := writeText(projectPath, project); err != nil {
		t.Fatal(err)
	}
	originalGlobal, err := readText(global)
	if err != nil {
		t.Fatal(err)
	}
	stubRunnerCatalogForTest(t, []RunnerCatalogModel{
		{Model: "gpt-6-terra", Efforts: []string{"low", "medium", "high", "xhigh"}},
		{Model: "gpt-5.6-luna", Efforts: []string{"low", "medium", "high", "xhigh"}},
		{Model: "gpt-6-sol", Efforts: []string{"low", "medium", "high", "xhigh"}},
		{Model: "gpt-6-luna", Efforts: []string{"low", "medium", "high", "xhigh"}},
	})
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "json": "true"}); err != nil {
		t.Fatal(err)
	}
	if after, err := readText(global); err != nil || after != originalGlobal {
		t.Fatalf("preview mutated global config: %v %q", err, after)
	}
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	if after, err := readText(projectPath); err != nil || after != project {
		t.Fatalf("bootstrap must never write the project config: %v %q", err, after)
	}
	written, err := readText(global)
	if err != nil || !strings.Contains(written, "custom:") || !strings.Contains(written, "execute-standard") || strings.Contains(written, "terra") || strings.Contains(written, "gpt-5.6") || strings.Contains(written, "codex_acp") {
		t.Fatalf("global write must add codex_exec non-Terra profiles and keep existing ones: %v\n%s", err, written)
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		t.Fatalf("written bootstrap config is invalid: %v", err)
	}
	if resolved.Config.Automation.DefaultProfile != "custom" || resolved.Config.Automation.Profiles["execute-standard"].Harness != string(RunnerCodexExec) {
		t.Fatalf("resolved = %#v", resolved.Config.Automation)
	}
}

func TestRunnerProfileBootstrapSkipsWhenGlobalProfilesCoverTiers(t *testing.T) {
	vault := automationTestVault(t)
	global := setGlobalProfileForTest(t, "codex_exec-gpt-6-sol-medium", map[string]any{"harness": "codex_exec", "model": "gpt-6-sol", "effort": "medium", "permission_preset": "workspace-write-offline", "eligible_tiers": []string{"light", "standard", "demanding"}, "sandbox": map[string]any{"mode": "workspace-write", "network": false}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0}})
	before, err := readText(global)
	if err != nil {
		t.Fatal(err)
	}
	stubRunnerCatalogForTest(t, []RunnerCatalogModel{{Model: "gpt-6-sol", Efforts: []string{"medium"}}})
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	if after, err := readText(global); err != nil || after != before {
		t.Fatalf("bootstrap re-proposed profiles although global profiles cover every tier: %v\n%s", err, after)
	}
}

func TestRunnerProfileBootstrapFreshInitWritesNoProjectProfiles(t *testing.T) {
	stubRunnerCatalogForTest(t, []RunnerCatalogModel{{Model: "gpt-6-sol", Efforts: []string{"low", "medium", "high"}}})
	vault := automationTestVault(t)
	if err := os.Remove(managedTuskerConfigPath(vault)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := writeDefaultTuskerConfig(vault); err != nil {
		t.Fatal(err)
	}
	raw, err := readText(managedTuskerConfigPath(vault))
	if err != nil || strings.Contains(raw, "\n  profiles:") {
		t.Fatalf("fresh init must not define project profiles: %v\n%s", err, raw)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Data.AutomationEnabled {
		t.Fatal("fresh init enabled automation")
	}
	if _, _, err := runnerForName(string(RunnerCodexExec), wf.Data); err != nil {
		t.Fatalf("fresh init did not admit codex_exec: %v", err)
	}
}

func TestRunnerCatalogHarnessIsolationAndBundled(t *testing.T) {
	original := runnerCatalogCommand
	originalAppServer := runnerCatalogAppServerModels
	originalDevin := runnerCatalogDevinModels
	originalStateRoot := runnerCatalogStateRoot
	defer func() {
		runnerCatalogCommand, runnerCatalogAppServerModels, runnerCatalogDevinModels, runnerCatalogStateRoot = original, originalAppServer, originalDevin, originalStateRoot
	}()
	root := t.TempDir()
	runnerCatalogStateRoot = func() string { return root }
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "devin" && len(args) == 1 && args[0] == "--version" {
			return []byte("devin 3000.10.21"), nil
		}
		if strings.Contains(strings.Join(args, " "), "--bundled") {
			return []byte(`{"models":[{"slug":"gpt-5.2","supported_reasoning_levels":[{"effort":"low"}]}]}`), nil
		}
		return nil, errCatalogFixture{}
	}
	runnerCatalogAppServerModels = func(context.Context) ([]RunnerCatalogModel, error) { return nil, errCatalogFixture{} }
	runnerCatalogDevinModels = func(context.Context) ([]RunnerCatalogModel, error) {
		return []RunnerCatalogModel{{Model: "swe-1-6-slow", Efforts: []string{"medium"}}}, nil
	}
	catalog := discoverRunnerCatalog(false)
	if catalog.Harnesses[0].Error == "" || catalog.Harnesses[1].DiscoveryState != "unsupported" {
		t.Fatalf("expected isolated codex failure and declared claude: %#v", catalog)
	}
	catalog = discoverRunnerCatalog(true)
	if catalog.Harnesses[0].Source != "bundled" || len(catalog.Harnesses[0].Models) != 0 {
		t.Fatalf("expected bundled catalog: %#v", catalog.Harnesses[0])
	}
	profiles := semanticBootstrapProfiles(catalog)
	if len(profiles) != 0 {
		t.Fatalf("bundled fallback invented runnable Codex profiles: %#v", profiles)
	}
}

func TestModelHarnessPresets(t *testing.T) {
	original := runnerCatalogCommand
	originalAppServer := runnerCatalogAppServerModels
	originalMuseServer := runnerCatalogMuseServerModels
	originalDevin := runnerCatalogDevinModels
	originalCodexExecutable := runnerCatalogCodexExecutable
	originalStateRoot := runnerCatalogStateRoot
	defer func() {
		runnerCatalogCommand, runnerCatalogAppServerModels, runnerCatalogMuseServerModels, runnerCatalogDevinModels, runnerCatalogCodexExecutable, runnerCatalogStateRoot = original, originalAppServer, originalMuseServer, originalDevin, originalCodexExecutable, originalStateRoot
	}()
	root := t.TempDir()
	t.Setenv("TUSKER_CONFIG", filepath.Join(root, "config.yaml"))
	runnerCatalogStateRoot = func() string { return root }
	runnerCatalogCodexExecutable = func() string { return "codex" }
	runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
		if name == "devin" && len(args) == 1 && args[0] == "--version" {
			return []byte("devin 3000.10.21"), nil
		}
		if name == "muse" && len(args) == 1 && args[0] == "--version" {
			return []byte("Muse Code 1.1.1"), nil
		}
		if name == "claude" {
			return []byte("2.1.280 (Claude Code)"), nil
		}
		if name != "codex" {
			return nil, errCatalogFixture{}
		}
		if strings.Contains(strings.Join(args, " "), "debug models") {
			return []byte(`{"models":[{"slug":"gpt-installed","supported_reasoning_levels":[{"effort":"medium"}]}]}`), nil
		}
		return []byte("codex-test"), nil
	}
	runnerCatalogAppServerModels = func(context.Context) ([]RunnerCatalogModel, error) {
		return []RunnerCatalogModel{{Model: "gpt-installed", Efforts: []string{"medium"}}}, nil
	}
	runnerCatalogMuseServerModels = func(context.Context) ([]RunnerCatalogModel, error) {
		return []RunnerCatalogModel{{Model: "muse-spark-1.3", DisplayName: "Muse Spark 1.3"}}, nil
	}
	runnerCatalogDevinModels = func(context.Context) ([]RunnerCatalogModel, error) {
		return []RunnerCatalogModel{{Model: "swe-1-6-slow", Efforts: []string{"medium"}, Default: true, DefaultKnown: true, DefaultEffort: "medium"}}, nil
	}
	catalog := discoverRunnerCatalog(false)
	if catalog.Schema != "tusker.runner-catalog/v1" || catalog.Version != 1 {
		t.Fatalf("catalog version=%#v", catalog)
	}
	if codex := catalog.Harnesses[0]; !codex.ExecutableDetected || codex.Authentication != "authenticated" || codex.DiscoveryState != "available" || !codex.ManualEntry {
		t.Fatalf("codex observation=%#v", codex)
	}
	if claude := catalog.Harnesses[1]; claude.Harness != string(RunnerClaude) || claude.Group != "supported" || !claude.ManualEntry || !claude.Available || claude.State != "available" || len(claude.Models) != 3 {
		t.Fatalf("Claude Code must be a selectable harness: %#v", claude)
	}
	muse := catalog.Harnesses[2]
	if muse.Harness != string(RunnerMuse) || !muse.Available || !muse.ExecutableDetected || muse.Authentication != "unknown" || muse.DiscoveryState != "available" || len(muse.Models) != 1 || muse.Models[0].Model != "muse-spark-1.3" || muse.Models[0].DefaultEffort != "high" || !muse.ManualEntry || muse.State != "available" {
		t.Fatalf("native Muse CLI must expose its discovered catalog: %#v", muse)
	}
	for _, harness := range catalog.Harnesses[3:] {
		if harness.Harness == string(RunnerDevin) {
			if harness.Group != "supported" || !harness.Available || harness.Authentication != "authenticated" || len(harness.Models) != 1 || len(harness.Transports) != 1 || harness.Transports[0] != "acp_stdio" {
				t.Fatalf("Devin route must remain manually configured: %#v", harness)
			}
			continue
		}
		if harness.Group != "future" || harness.ManualEntry || harness.State != "unsupported" {
			t.Fatalf("future preset must not be selectable: %#v", harness)
		}
	}
	if _, command, err := runnerForName(string(RunnerMuse), defaultWorkflow()); err != nil || command != "muse exec --json" {
		t.Fatalf("Muse route=%q err=%v", command, err)
	}
	if runner, command, err := runnerForName(string(RunnerDevin), defaultWorkflow()); err != nil || runner.Name() != RunnerDevin || command != "devin acp" {
		t.Fatalf("Devin route=%v command=%q err=%v", runner, command, err)
	}
	server := newServeEmptyNeedsFixture(t)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7420/api/models/catalog", nil))
	var fromAPI RunnerCatalog
	if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &fromAPI) != nil || fromAPI.Schema != catalog.Schema || len(fromAPI.Harnesses) != len(catalog.Harnesses) {
		t.Fatalf("catalog API mismatch: status=%d body=%s", recorder.Code, recorder.Body.String())
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
	profiles := semanticBootstrapProfiles(RunnerCatalog{Harnesses: []RunnerCatalogHarness{{Harness: "codex_exec", Source: "live", Available: true, Models: []RunnerCatalogModel{{Model: "hidden", Hidden: true, Efforts: []string{"low"}}, {Model: "gpt-6-sol", Default: true, Efforts: []string{"low", "medium", "high", "xhigh"}}}}}})
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
	originalAppServer := runnerCatalogAppServerModels
	originalCodexExecutable := runnerCatalogCodexExecutable
	defer func() {
		runnerCatalogCommand, runnerCatalogAppServerModels, runnerCatalogCodexExecutable = original, originalAppServer, originalCodexExecutable
	}()
	runnerCatalogCommand = func(string, ...string) ([]byte, error) { return nil, errCatalogFixture{} }
	runnerCatalogAppServerModels = func(context.Context) ([]RunnerCatalogModel, error) { return nil, errCatalogFixture{} }
	runnerCatalogCodexExecutable = func() string { return "" }
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	vault := automationTestVault(t)
	path := managedTuskerConfigPath(vault)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := writeDefaultTuskerConfig(vault); err != nil {
		t.Fatal(err)
	}
	raw, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "default_profile: execute-standard") {
		t.Fatalf("bootstrap fabricated default profile:\n%s", raw)
	}
	if strings.Contains(raw, "profiles: {}") {
		t.Fatalf("bootstrap emitted an explicit empty profile map that clears built-in policy:\n%s", raw)
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		t.Fatalf("bootstrap without a machine catalog must retain a valid built-in profile: %v", err)
	}
	if resolved.Config.Automation.DefaultProfile != "default" {
		t.Fatalf("default profile = %q, want inherited built-in default", resolved.Config.Automation.DefaultProfile)
	}
	if _, ok := resolved.Config.Automation.Profiles["default"]; !ok {
		t.Fatalf("built-in default profile was cleared: %#v", resolved.Config.Automation.Profiles)
	}
}

func TestProfileReconcileWithoutUsableHarnessWritesNothing(t *testing.T) {
	stubRunnerCatalogForTest(t, nil)
	global := filepath.Join(t.TempDir(), "config.yaml")
	t.Setenv("TUSKER_CONFIG", global)
	vault := automationTestVault(t)
	if err := runnerProfilesBootstrapCmd(Args{"vault": vault, "write": "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(global); !os.IsNotExist(err) {
		t.Fatalf("bootstrap without a usable harness must not write the global config: %v", err)
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
