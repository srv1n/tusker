package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return err
	}
	// Bootstrap is a fresh managed write. Seed it with the effective legacy
	// policy so creating config.yaml cannot erase a coexisting root config.
	path := managedTuskerConfigPath(vault)
	raw := cloneConfigRaw(resolved.Raw)
	profiles := semanticBootstrapProfiles(discoverRunnerCatalog(args.Bool("bundled")))
	automation := mapAny(raw["automation"])
	if automation == nil {
		automation = map[string]any{}
		raw["automation"] = automation
	}
	existing := mapAny(automation["profiles"])
	profilesExplicitlyEmpty := false
	if rawProfiles, present := automation["profiles"]; present {
		profilesExplicitlyEmpty = mapAny(rawProfiles) != nil && len(mapAny(rawProfiles)) == 0
	}
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
	// Do not create a dangling policy reference on a machine with no usable
	// harness.  Existing policy remains entirely user-owned.
	if _, present := automation["default_profile"]; !present && hasBootstrapProfile(profiles, "execute-standard") {
		automation["default_profile"] = "execute-standard"
	}
	// An explicit empty profile map is a deliberate project override that
	// clears the built-in profiles.  Do not leave the inherited built-in
	// default_profile pointing at a profile that no longer exists when the
	// machine catalog cannot provide replacements.
	if profilesExplicitlyEmpty && len(profiles) == 0 {
		delete(automation, "default_profile")
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
		"selection_policy":    "live or explicitly bundled Codex prefers Luna for fast work, Terra for standard/complex/review/repair, and Sol for planning/frontier; an available Claude-only machine prefers Sonnet, Opus, and Fable by the corresponding role; every role uses a visible capable model and nearest supported effort",
		"configuration_scope": "project policy; the observed harness catalog remains machine-local",
	}
	if args.Bool("write") {
		out, err := yaml.Marshal(raw)
		if err != nil {
			return err
		}
		if err := writeConfigTextAtomically(path, string(out)); err != nil {
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
	efforts := map[string]string{"planner": "high", "execute-fast": "low", "execute-standard": "medium", "execute-complex": "high", "execute-frontier": "xhigh", "review-independent": "high", "repair-complex": "high"}
	harness, models, ok := bootstrapCatalogHarness(catalog)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{}
	for name, effort := range efforts {
		model, ok := semanticModelFor(harness, name, models)
		if !ok {
			continue
		}
		resolvedEffort := semanticEffortFor(harness, effort, model.Efforts)
		if resolvedEffort == "" {
			continue
		}
		mode := "workspace-write"
		preset := "workspace-write-offline"
		network := false
		if name == "review-independent" {
			mode = "read-only"
			preset = "read-only"
		}
		out[name] = map[string]any{"harness": harness, "model": model.Model, "effort": resolvedEffort, "permission_preset": preset, "sandbox": map[string]any{"mode": mode, "network": network}, "subagents": map[string]any{"allowed": false, "max_concurrent": 0}}
	}
	return out
}

func hasBootstrapProfile(profiles map[string]any, name string) bool {
	_, ok := profiles[name]
	return ok
}

// bootstrapCatalogHarness trusts Codex inventory only when the installed CLI
// returned it, whether from the live endpoint or an explicit --bundled query.
// This command is an explicit compatibility/profile-generation action; it is
// not the runtime default or an automatic fallback from an ACP attempt.
// Claude's aliases remain a truthful fallback when locally available.
func bootstrapCatalogHarness(catalog RunnerCatalog) (string, []RunnerCatalogModel, bool) {
	for _, entry := range catalog.Harnesses {
		if entry.Harness == string(RunnerCodexExec) && (entry.Source == "live" || entry.Source == "bundled") && entry.Available {
			if models := usableCatalogModels(entry.Harness, entry.Models); len(models) > 0 {
				return entry.Harness, models, true
			}
		}
	}
	for _, entry := range catalog.Harnesses {
		if entry.Harness == string(RunnerClaude) && entry.Source == "declared" && entry.Available {
			if models := usableCatalogModels(entry.Harness, entry.Models); len(models) > 0 {
				return entry.Harness, models, true
			}
		}
	}
	return "", nil, false
}

func usableCatalogModels(harness string, models []RunnerCatalogModel) []RunnerCatalogModel {
	out := make([]RunnerCatalogModel, 0, len(models))
	for _, model := range models {
		if model.Hidden || !validRunnerModelName(model.Model) {
			continue
		}
		for _, effort := range model.Efforts {
			if validCatalogEffort(harness, effort) {
				out = append(out, model)
				break
			}
		}
	}
	return out
}

func validCatalogEffort(harness, effort string) bool {
	return validRunnerEffort(effort) && !(harness == string(RunnerClaude) && strings.EqualFold(strings.TrimSpace(effort), "ultra"))
}

func semanticModelFor(harness, role string, models []RunnerCatalogModel) (RunnerCatalogModel, bool) {
	preferences := []string{""}
	if harness == string(RunnerClaude) {
		switch role {
		case "execute-fast":
			preferences = []string{"sonnet", ""}
		case "planner", "execute-frontier":
			preferences = []string{"fable", "opus", ""}
		case "execute-standard":
			preferences = []string{"sonnet", "opus", ""}
		default: // complex execution, independent review, and repair
			preferences = []string{"opus", "fable", "sonnet", ""}
		}
	} else {
		switch role {
		case "execute-fast":
			preferences = []string{"luna", "mini", "spark", ""}
		case "planner", "execute-frontier":
			preferences = []string{"sol", ""}
		default:
			preferences = []string{"terra", ""}
		}
	}
	for _, preference := range preferences {
		for _, model := range models {
			if preference == "" || strings.Contains(strings.ToLower(model.Model), preference) {
				return model, true
			}
		}
	}
	return RunnerCatalogModel{}, false
}

func semanticEffortFor(harness, want string, supported []string) string {
	levels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	wantIndex := -1
	for i, level := range levels {
		if strings.EqualFold(level, strings.TrimSpace(want)) {
			wantIndex = i
			break
		}
	}
	if wantIndex < 0 {
		return ""
	}
	best, bestDistance := "", len(levels)+1
	for i, level := range levels {
		if !catalogContainsString(supported, level) || !validCatalogEffort(harness, level) {
			continue
		}
		distance := i - wantIndex
		if distance < 0 {
			distance = -distance
		}
		// Equal-distance ties select the lower effort (smaller i), avoiding an
		// accidental spend escalation when the exact level is unavailable.
		if distance < bestDistance || (distance == bestDistance && (best == "" || i < effortLevelIndex(best, levels))) {
			best, bestDistance = level, distance
		}
	}
	return best
}

func effortLevelIndex(effort string, levels []string) int {
	for i, level := range levels {
		if level == effort {
			return i
		}
	}
	return len(levels)
}
func catalogContainsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(want)) {
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
profile bootstrap; --write updates the project config without enabling automation.`)
}
