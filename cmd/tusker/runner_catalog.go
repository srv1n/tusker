package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// RunnerCatalog is deliberately a machine-local observation, never project policy.
type RunnerCatalog struct {
	ObservedAt string                 `json:"observed_at"`
	Harnesses  []RunnerCatalogHarness `json:"harnesses"`
}

type RunnerCatalogHarness struct {
	Harness    string               `json:"harness"`
	Source     string               `json:"source"`
	Confidence string               `json:"confidence"`
	Version    string               `json:"version,omitempty"`
	Available  bool                 `json:"available"`
	Models     []RunnerCatalogModel `json:"models,omitempty"`
	Error      string               `json:"error,omitempty"`
}

type RunnerCatalogModel struct {
	Model        string   `json:"model"`
	DisplayName  string   `json:"display_name,omitempty"`
	Description  string   `json:"description,omitempty"`
	Efforts      []string `json:"efforts"`
	ServiceTiers []string `json:"service_tiers,omitempty"`
	Default      bool     `json:"default"`
	DefaultKnown bool     `json:"default_known"`
	Visibility   string   `json:"visibility"`
	Hidden       bool     `json:"hidden"`
}

var runnerCatalogNow = func() time.Time { return time.Now().UTC() }
var runnerCatalogCommand = func(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

func runnerCatalogCmd(args Args) error {
	catalog := discoverRunnerCatalog(args.Bool("bundled"))
	if args.Bool("json") {
		emitJSON(catalog)
		return nil
	}
	for _, harness := range catalog.Harnesses {
		fmt.Printf("%s: %s (%s)\n", harness.Harness, harness.Source, harness.Confidence)
		if harness.Error != "" {
			fmt.Printf("  error: %s\n", harness.Error)
		}
		for _, model := range harness.Models {
			fmt.Printf("  %s [%s]\n", model.Model, strings.Join(model.Efforts, ", "))
		}
	}
	return nil
}

func discoverRunnerCatalog(bundled bool) RunnerCatalog {
	result := RunnerCatalog{ObservedAt: runnerCatalogNow().Format(time.RFC3339), Harnesses: []RunnerCatalogHarness{}}
	result.Harnesses = append(result.Harnesses, discoverCodexCatalog(bundled))
	claude := RunnerCatalogHarness{Harness: "claude-code", Source: "declared", Confidence: "lower", Available: true, Models: []RunnerCatalogModel{{Model: "fable", Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Visibility: "declared"}, {Model: "opus", Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Visibility: "declared"}, {Model: "sonnet", Efforts: []string{"low", "medium", "high", "xhigh", "max"}, Visibility: "declared"}}}
	if out, err := runnerCatalogCommand("claude", "--version"); err != nil {
		claude.Available = false
		claude.Error = err.Error()
	} else {
		claude.Version = strings.TrimSpace(string(out))
	}
	result.Harnesses = append(result.Harnesses, claude)
	return result
}

func discoverCodexCatalog(bundled bool) RunnerCatalogHarness {
	output, err := codexCatalogOutput(bundled)
	if bundled && err != nil {
		return bundledCodexCatalog()
	}
	if err != nil {
		return RunnerCatalogHarness{Harness: "codex_exec", Source: "live", Confidence: "none", Available: false, Error: err.Error()}
	}
	models := parseCodexModels(output)
	source := "live"
	confidence := "high"
	if bundled {
		source = "bundled"
		confidence = "medium"
	}
	if len(models) == 0 {
		return RunnerCatalogHarness{Harness: "codex_exec", Source: source, Confidence: "none", Error: "codex debug models returned no parseable models"}
	}
	return RunnerCatalogHarness{Harness: "codex_exec", Source: source, Confidence: confidence, Available: true, Version: codexVersion(), Models: models}
}

func codexCatalogOutput(bundled bool) ([]byte, error) {
	args := []string{"debug", "models"}
	if bundled {
		args = append(args, "--bundled")
	}
	return runnerCatalogCommand("codex", args...)
}

func codexVersion() string {
	out, err := runnerCatalogCommand("codex", "--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func bundledCodexCatalog() RunnerCatalogHarness {
	return RunnerCatalogHarness{Harness: "codex_exec", Source: "bundled", Confidence: "medium", Available: false, Models: []RunnerCatalogModel{{Model: "gpt-5.x", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, Default: true, DefaultKnown: true, Visibility: "visible"}}}
}

func parseCodexModels(raw []byte) []RunnerCatalogModel {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var rows []any
	switch v := value.(type) {
	case []any:
		rows = v
	case map[string]any:
		if m, ok := v["models"].([]any); ok {
			rows = m
		}
	}
	models := make([]RunnerCatalogModel, 0, len(rows))
	for _, row := range rows {
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(stringAny(m["id"]), stringAny(m["model"]), stringAny(m["slug"]))
		if name == "" {
			continue
		}
		efforts := stringSliceAny(m["efforts"])
		if len(efforts) == 0 {
			efforts = stringSliceAny(m["reasoning_efforts"])
		}
		if len(efforts) == 0 {
			efforts = stringSliceAny(m["supported_reasoning_levels"])
		}
		if len(efforts) == 0 {
			continue
		}
		defaultEffort := stringAny(m["default_reasoning_level"])
		if defaultEffort != "" {
			efforts = uniqueCatalogStrings(append([]string{defaultEffort}, efforts...))
		}
		visibility := strings.ToLower(firstNonEmpty(stringAny(m["visibility"]), "visible"))
		defaultValue, defaultKnown := boolFieldAny(m, "is_default", "isDefault", "default")
		models = append(models, RunnerCatalogModel{Model: name, DisplayName: firstNonEmpty(stringAny(m["display_name"]), stringAny(m["displayName"])), Description: stringAny(m["description"]), Efforts: efforts, ServiceTiers: stringSliceAny(m["service_tiers"]), Default: defaultValue, DefaultKnown: defaultKnown, Visibility: visibility, Hidden: visibility == "hide" || visibility == "hidden" || boolAny(m["hidden"])})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Model < models[j].Model })
	return models
}

func stringAny(value any) string { out, _ := value.(string); return strings.TrimSpace(out) }
func boolAny(value any) bool     { out, _ := value.(bool); return out }
func boolFieldAny(values map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			out, ok := value.(bool)
			return out, ok
		}
	}
	return false, false
}
func stringSliceAny(value any) []string {
	raw, _ := value.([]any)
	out := []string{}
	for _, v := range raw {
		if s := firstNonEmpty(stringAny(v), stringAny(catalogMapStringAny(v)["effort"]), stringAny(catalogMapStringAny(v)["id"])); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func catalogMapStringAny(value any) map[string]any { out, _ := value.(map[string]any); return out }
func uniqueCatalogStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func runnerProfilesBootstrapCmd(args Args) error {
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	path := filepath.Join(v7RepoRoot(vault), "tusker.yaml")
	rawText, err := readText(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(rawText), &raw); err != nil {
		return err
	}
	profiles := semanticBootstrapProfiles(discoverRunnerCatalog(args.Bool("bundled")))
	automation := mapAny(raw["automation"])
	if automation == nil {
		automation = map[string]any{}
		raw["automation"] = automation
	}
	existing := mapAny(automation["profiles"])
	if existing == nil {
		existing = map[string]any{}
		automation["profiles"] = existing
	}
	added := []string{}
	for name, profile := range profiles {
		if _, found := existing[name]; !found {
			existing[name] = profile
			added = append(added, name)
		}
	}
	sort.Strings(added)
	if _, present := automation["enabled"]; !present {
		automation["enabled"] = false
	}
	if _, present := automation["default_profile"]; !present {
		automation["default_profile"] = "execute-standard"
	}
	automationEnabled := boolAny(automation["enabled"])
	report := map[string]any{
		"write":               args.Bool("write"),
		"path":                path,
		"added_profiles":      added,
		"preserved_profiles":  sortedBootstrapMapKeys(existing, added),
		"semantic_profiles":   profiles,
		"default_profile":     stringAny(automation["default_profile"]),
		"automation_enabled":  automationEnabled,
		"selection_policy":    "fast prefers Luna; standard, complex, review, and repair prefer Terra; planner and frontier prefer Sol; every role falls back to a visible capable model and nearest supported effort",
		"configuration_scope": "project policy; the observed harness catalog remains machine-local",
	}
	if args.Bool("write") {
		out, err := yaml.Marshal(raw)
		if err != nil {
			return err
		}
		if err := writeText(path, string(out)); err != nil {
			return err
		}
	}
	if args.Bool("json") {
		emitJSON(report)
	} else {
		fmt.Printf("profiles %s: %s\n", map[bool]string{true: "written", false: "previewed"}[args.Bool("write")], strings.Join(added, ", "))
	}
	return nil
}

func mapAny(value any) map[string]any { out, _ := value.(map[string]any); return out }
func sortedBootstrapMapKeys(values map[string]any, exclude []string) []string {
	skip := map[string]bool{}
	for _, k := range exclude {
		skip[k] = true
	}
	out := []string{}
	for k := range values {
		if !skip[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func semanticBootstrapProfiles(catalog RunnerCatalog) map[string]any {
	harness := "codex_exec"
	efforts := map[string]string{"planner": "high", "execute-fast": "low", "execute-standard": "medium", "execute-complex": "high", "execute-frontier": "xhigh", "review-independent": "high", "repair-complex": "high"}
	models := []RunnerCatalogModel{{Model: "gpt-5.x", Efforts: []string{"low", "medium", "high", "xhigh"}}}
	for _, entry := range catalog.Harnesses {
		if entry.Harness == harness && len(entry.Models) > 0 {
			models = entry.Models
			break
		}
	}
	out := map[string]any{}
	for name, effort := range efforts {
		model := semanticModelFor(name, effort, models)
		mode := "workspace-write"
		preset := "workspace-write-offline"
		network := false
		if name == "review-independent" {
			mode = "read-only"
			preset = "read-only"
		}
		out[name] = map[string]any{"harness": harness, "model": model.Model, "effort": semanticEffortFor(effort, model.Efforts), "permission_preset": preset, "sandbox": map[string]any{"mode": mode, "network": network}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0}}
	}
	return out
}

func semanticModelFor(role, effort string, models []RunnerCatalogModel) RunnerCatalogModel {
	preferences := []string{""}
	switch role {
	case "execute-fast":
		preferences = []string{"luna", "mini", "spark", ""}
	case "planner", "execute-frontier":
		preferences = []string{"sol", ""}
	default:
		preferences = []string{"terra", ""}
	}
	for _, preference := range preferences {
		for _, model := range models {
			if !model.Hidden && catalogContainsString(model.Efforts, effort) && (preference == "" || strings.Contains(strings.ToLower(model.Model), preference)) {
				return model
			}
		}
	}
	for _, preference := range preferences {
		for _, model := range models {
			if !model.Hidden && (preference == "" || strings.Contains(strings.ToLower(model.Model), preference)) {
				return model
			}
		}
	}
	for _, model := range models {
		if !model.Hidden {
			return model
		}
	}
	return RunnerCatalogModel{Model: "gpt-5.x", Efforts: []string{"low", "medium", "high", "xhigh"}}
}
func semanticEffortFor(want string, supported []string) string {
	if catalogContainsString(supported, want) {
		return want
	}
	for _, effort := range []string{"high", "medium", "low", "xhigh", "max", "ultra"} {
		if catalogContainsString(supported, effort) {
			return effort
		}
	}
	return "medium"
}
func catalogContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func printRunnerHelp() {
	fmt.Println(`Usage:
  tusker runner catalog [--bundled] [--json]
  tusker runner profiles [--bundled] [--write] [--json]
  tusker runner route <TASK-ID> --lane execute|review --json

Catalog observes installed harnesses without authentication or model launch. --bundled
uses Tusker's explicit offline Codex fallback. Profiles previews an additive semantic
profile bootstrap; --write updates tusker.yaml without enabling automation.`)
}
