package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	runnercore "tusker/internal/runner"
	"tusker/internal/v7schema"

	"gopkg.in/yaml.v3"
)

const (
	configSourceBuiltIn    = "built-in defaults"
	configSourceUserGlobal = "user-global config"
	configSourceProject    = "project config"
	configSourceLocal      = "machine-local config"
)

var projectLocalConfigWriteMu sync.Mutex

// userGlobalConfigWriteMu serializes in-process read-modify-write of the
// shared user-global config (Serve handles saves for many projects at once).
var userGlobalConfigWriteMu sync.Mutex

const (
	managedTuskerConfigName      = "config.yaml"
	managedTuskerLocalConfigName = "config.local.yaml"
)

func managedTuskerConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, managedTuskerConfigName)
}

func managedTuskerLocalConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, managedTuskerLocalConfigName)
}

type RunnerSandboxDefinition struct {
	Mode    string `yaml:"mode" json:"mode"`
	Network *bool  `yaml:"network,omitempty" json:"network,omitempty"`
}

type RunnerSubagentPolicyDefinition struct {
	Allowed       *bool `yaml:"allowed,omitempty" json:"allowed,omitempty"`
	MaxConcurrent int   `yaml:"max_concurrent,omitempty" json:"max_concurrent,omitempty"`
}

type RunnerProfileDefinition struct {
	Disabled          bool                           `yaml:"disabled,omitempty" json:"disabled,omitempty"`
	DisplayName       string                         `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	EligibleTiers     []string                       `yaml:"eligible_tiers" json:"eligible_tiers"`
	Harness           string                         `yaml:"harness" json:"harness"`
	Model             string                         `yaml:"model" json:"model"`
	Effort            string                         `yaml:"effort" json:"effort"`
	PermissionPreset  string                         `yaml:"permission_preset,omitempty" json:"permission_preset,omitempty"`
	Command           string                         `yaml:"command,omitempty" json:"command,omitempty"`
	NativeContainment bool                           `yaml:"native_containment,omitempty" json:"native_containment,omitempty"`
	Sandbox           RunnerSandboxDefinition        `yaml:"sandbox" json:"sandbox"`
	Subagents         RunnerSubagentPolicyDefinition `yaml:"subagents" json:"subagents"`
	Access            *AgentAccessV1                 `yaml:"access,omitempty" json:"access,omitempty"`
}

type RunnerRoutingMatch struct {
	Epic          any `yaml:"epic,omitempty" json:"epic,omitempty"`
	Risk          any `yaml:"risk,omitempty" json:"risk,omitempty"`
	Size          any `yaml:"size,omitempty" json:"size,omitempty"`
	Domains       any `yaml:"domains,omitempty" json:"domains,omitempty"`
	TitleKeywords any `yaml:"title_keywords,omitempty" json:"title_keywords,omitempty"`
}

type RunnerRoutingRule struct {
	Name    string             `yaml:"name" json:"name"`
	Profile string             `yaml:"profile" json:"profile"`
	Match   RunnerRoutingMatch `yaml:"match" json:"match"`
}

type RunnerDenyRule struct {
	// Deprecated compatibility metadata. These regex declarations are retained
	// for config round-tripping and provenance only; native callbacks use the
	// fixed runnercore.CommandPolicy plus adapter-scoped target checks.
	ID                   string `yaml:"id" json:"id"`
	Pattern              string `yaml:"pattern" json:"pattern"`
	Description          string `yaml:"description,omitempty" json:"description,omitempty"`
	CodexExecPolicy      string `yaml:"codex_execpolicy,omitempty" json:"codex_execpolicy,omitempty"`
	ClaudePermissionRule string `yaml:"claude_permission_rule,omitempty" json:"claude_permission_rule,omitempty"`
	PreToolUse           string `yaml:"pre_tool_use,omitempty" json:"pre_tool_use,omitempty"`
}

type ResolvedRunnerProfile struct {
	Name       string                  `json:"name"`
	Source     string                  `json:"source"`
	Reason     string                  `json:"reason"`
	RuleName   string                  `json:"rule_name,omitempty"`
	Warnings   []string                `json:"warnings,omitempty"`
	Fallbacks  []string                `json:"fallbacks,omitempty"`
	Definition RunnerProfileDefinition `json:"definition"`
}

type tuskerConfigLayer struct {
	Name    string
	Path    string
	Present bool
	Config  v7TuskerConfigFile
	Raw     map[string]any
	// AppliedRaw is the subset of Raw that this layer is allowed to affect.
	// User-global config intentionally keeps behavioral declarations visible for
	// provenance while preventing them from changing a project's policy.
	AppliedRaw map[string]any
}

type resolvedTuskerConfig struct {
	Config v7TuskerConfigFile
	Layers []tuskerConfigLayer
	// Raw is the exact field-presence-aware effective document. Config is its
	// typed projection; callers that need provenance must never reconstruct it
	// from Config because Go zero values lose whether a field was set.
	Raw map[string]any
	// Warnings lists non-fatal problems such as references to profiles the
	// global config does not define.
	Warnings []string
}

type configResolveSourceValue struct {
	Source  string `json:"source"`
	Path    string `json:"path,omitempty"`
	Present bool   `json:"present"`
	Winning bool   `json:"winning"`
	Value   any    `json:"value,omitempty"`
	Note    string `json:"note,omitempty"`
}

type configResolveReport struct {
	Key     string                     `json:"key"`
	Lookup  string                     `json:"lookup"`
	Value   any                        `json:"value"`
	Source  string                     `json:"source"`
	Path    string                     `json:"path,omitempty"`
	Sources []configResolveSourceValue `json:"sources"`
}

func boolPtr(value bool) *bool {
	v := value
	return &v
}

func builtInTuskerConfig() v7TuskerConfigFile {
	var cfg v7TuskerConfigFile
	cfg.Tier = 5
	cfg.Automation.DefaultProfile = "default"
	cfg.Automation.Profiles = map[string]v7schema.TuskerRunnerProfileConfig{
		"default": {
			DisplayName:      "Codex Standard",
			EligibleTiers:    []string{"standard", "demanding"},
			Harness:          string(RunnerCodexExec),
			Model:            "gpt-5.x",
			Effort:           "medium",
			PermissionPreset: "workspace-write-offline",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: boolPtr(false)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(true), MaxConcurrent: 2},
		},
		"execute-cheap": {
			DisplayName:      "Codex Light",
			EligibleTiers:    []string{"light", "standard"},
			Harness:          string(RunnerCodexExec),
			Model:            "gpt-5.x",
			Effort:           "low",
			PermissionPreset: "workspace-write-network",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: boolPtr(true)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(false), MaxConcurrent: 0},
		},
		"review-frontier": {
			DisplayName:      "Claude Reviewer",
			EligibleTiers:    []string{"light", "standard", "demanding"},
			Harness:          string(RunnerClaude),
			Model:            "claude-opus-4-8",
			Effort:           "high",
			PermissionPreset: "read-only",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "read-only", Network: boolPtr(false)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(false), MaxConcurrent: 0},
		},
		"unrestricted-high": {
			DisplayName:   "Codex Full Access",
			EligibleTiers: []string{},
			// Direct Codex is the fresh-install default. ACP remains an explicitly
			// configured adapter and is never an automatic fallback.
			Harness:          string(RunnerCodexExec),
			Model:            "gpt-5.x",
			Effort:           "high",
			PermissionPreset: "danger-full-access",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "danger-full-access", Network: boolPtr(true)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(true), MaxConcurrent: 2},
		},
	}
	cfg.Automation.ModelLevels = map[string]v7schema.TuskerModelLevelConfig{
		"light":     {Execute: []string{"execute-cheap"}, Review: []string{"review-frontier"}},
		"standard":  {Execute: []string{"default"}, Review: []string{"review-frontier"}},
		"demanding": {Execute: []string{"default"}, Review: []string{"review-frontier"}},
	}
	cfg.Automation.Denylist = []v7schema.TuskerAutomationDenyRuleConfig{
		{ID: "recursive-rm-outside-workspace", Pattern: `rm\s+-rf\s+/(?!.*\bTUSKER_WORKSPACE\b)`, Description: "block recursive rm outside the workspace", CodexExecPolicy: "deny", ClaudePermissionRule: "deny", PreToolUse: "deny"},
		{ID: "git-push-force", Pattern: `git\s+push\b.*\s--force`, Description: "block force pushes", CodexExecPolicy: "deny", ClaudePermissionRule: "deny", PreToolUse: "deny"},
		{ID: "git-reset-hard", Pattern: `git\s+reset\s+--hard\b`, Description: "block hard resets", CodexExecPolicy: "deny", ClaudePermissionRule: "deny", PreToolUse: "deny"},
		{ID: "destructive-db-migration", Pattern: `(drop|truncate)\s+(table|database)|migrate\b.*\b(down|reset|drop)\b`, Description: "block destructive database migrations", CodexExecPolicy: "deny", ClaudePermissionRule: "deny", PreToolUse: "deny"},
		{ID: "credential-file-write", Pattern: `>\s*(\.env|.*credentials|.*secrets)|tee\s+(\.env|.*credentials|.*secrets)`, Description: "block credential file writes", CodexExecPolicy: "deny", ClaudePermissionRule: "deny", PreToolUse: "deny"},
	}
	cfg.Automation.Concurrency.MaxActiveRuns = 2
	cfg.Automation.Concurrency.MaxActiveRunsPerProject = 1
	return cfg
}

func resolveTuskerConfig(vaultPath string) (resolvedTuskerConfig, error) {
	return resolveTuskerConfigForPaths(v7RepoRoot(vaultPath), vaultPath, true)
}

func resolveTuskerConfigForRepo(repoRoot string, includeProject bool) (resolvedTuskerConfig, error) {
	return resolveTuskerConfigForPaths(repoRoot, filepath.Join(repoRoot, defaultRepoVaultDir), includeProject)
}

func resolveTuskerConfigForPaths(repoRoot, vaultPath string, includeProject bool) (resolvedTuskerConfig, error) {
	return resolveTuskerConfigForPathsWithOverrides(repoRoot, vaultPath, includeProject, nil)
}

// resolveTuskerConfigForPathsWithOverrides resolves the same layer stack as
// the production reader, while allowing setters to validate their complete
// post-write document before atomically replacing the on-disk file.
func resolveTuskerConfigForPathsWithOverrides(repoRoot, vaultPath string, includeProject bool, overrides map[string]map[string]any) (resolvedTuskerConfig, error) {
	layers := []tuskerConfigLayer{
		{Name: configSourceBuiltIn, Present: true, Config: builtInTuskerConfig()},
		{Name: configSourceUserGlobal, Path: userGlobalTuskerConfigPath()},
	}
	if includeProject {
		layers = append(layers,
			tuskerConfigLayer{Name: configSourceProject, Path: managedTuskerConfigPath(vaultPath)},
			tuskerConfigLayer{Name: configSourceLocal, Path: managedTuskerLocalConfigPath(vaultPath)},
		)
	}
	for i := range layers {
		if layers[i].Name == configSourceBuiltIn {
			layers[i].Raw = builtInTuskerConfigRaw()
			layers[i].AppliedRaw = cloneConfigRaw(layers[i].Raw)
			continue
		}
		if raw, ok := overrides[layers[i].Path]; ok {
			layers[i].Raw = cloneConfigRaw(raw)
			layers[i].AppliedRaw = configLayerAppliedRaw(layers[i].Name, raw)
			cfg, err := decodeTuskerConfigRaw(layers[i].AppliedRaw, layers[i].Path)
			if err != nil {
				return resolvedTuskerConfig{}, err
			}
			layers[i].Config = cfg
			layers[i].Present = true
			if err := validateTuskerConfigLayer(layers[i]); err != nil {
				return resolvedTuskerConfig{}, err
			}
			continue
		}
		var (
			cfg     v7TuskerConfigFile
			raw     map[string]any
			present bool
			err     error
		)
		if layers[i].Name == configSourceUserGlobal {
			raw, present, err = readTuskerConfigRawLayer(layers[i].Path)
			if err == nil {
				cfg, err = decodeTuskerConfigRaw(configLayerAppliedRaw(layers[i].Name, raw), layers[i].Path)
			}
		} else {
			cfg, raw, present, err = readTuskerConfigLayer(layers[i].Path)
		}
		if err != nil {
			return resolvedTuskerConfig{}, err
		}
		layers[i].Config = cfg
		layers[i].Raw = raw
		layers[i].AppliedRaw = configLayerAppliedRaw(layers[i].Name, raw)
		layers[i].Present = present
		if err := validateTuskerConfigLayer(layers[i]); err != nil {
			return resolvedTuskerConfig{}, err
		}
	}
	effectiveRaw := cloneConfigRaw(layers[0].Raw)
	for _, layer := range layers[1:] {
		if !layer.Present {
			continue
		}
		mergeConfigRaw(effectiveRaw, appliedConfigRaw(layer))
	}
	applyRemovedProfiles(effectiveRaw)
	// The global layer may explicitly clear the built-in profile map (project
	// layers may not define profiles at all).  In that transitional
	// state inherited profile references would be dangling and should not make
	// the config unreadable before the reconciler can repair it.  Preserve
	// values the same layer sets explicitly so validation still rejects a
	// genuine user typo.
	for i := len(layers) - 1; i >= 1; i-- {
		layer := layers[i]
		if !layer.Present {
			continue
		}
		automation := mapAny(appliedConfigRaw(layer)["automation"])
		profiles, hasProfiles := automation["profiles"]
		if !hasProfiles {
			continue
		}
		if profileMap := mapAny(profiles); profileMap != nil && len(profileMap) == 0 {
			if effectiveAutomation := mapAny(effectiveRaw["automation"]); effectiveAutomation != nil {
				for _, key := range []string{"default_profile", "model_levels", "lane_profiles", "routing"} {
					if _, explicit := automation[key]; !explicit {
						delete(effectiveAutomation, key)
					}
				}
			}
		}
		break
	}
	effective, err := decodeTuskerConfigRaw(effectiveRaw, "effective configuration")
	if err != nil {
		return resolvedTuskerConfig{}, err
	}
	warnings, err := validateResolvedTuskerConfig(effective, layers)
	if err != nil {
		return resolvedTuskerConfig{}, err
	}
	return resolvedTuskerConfig{Config: effective, Layers: layers, Raw: effectiveRaw, Warnings: warnings}, nil
}

func builtInTuskerConfigRaw() map[string]any {
	cfg := builtInTuskerConfig()
	return map[string]any{
		"tier": cfg.Tier,
		"automation": map[string]any{
			"default_profile": "default",
			"profiles":        configRawMap(cfg)["automation"].(map[string]any)["profiles"],
			"model_levels":    configRawMap(cfg)["automation"].(map[string]any)["model_levels"],
			"denylist":        configRawMap(cfg)["automation"].(map[string]any)["denylist"],
			"concurrency": map[string]any{
				"max_active_runs":             2,
				"max_active_runs_per_project": 1,
			},
		},
	}
}

func readTuskerConfigLayer(path string) (v7TuskerConfigFile, map[string]any, bool, error) {
	raw, present, err := readTuskerConfigRawLayer(path)
	if err != nil || !present {
		return v7TuskerConfigFile{}, raw, present, err
	}
	var cfg v7TuskerConfigFile
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return cfg, raw, true, err
	}
	if err := yaml.Unmarshal(encoded, &cfg); err != nil {
		return cfg, raw, true, tuskerError(errorConfigInvalid, "failed to decode config: "+err.Error(), withPath(path))
	}
	return cfg, raw, true, nil
}

func readTuskerConfigRawLayer(path string) (map[string]any, bool, error) {
	if strings.TrimSpace(path) == "" || !fileExists(path) {
		return nil, false, nil
	}
	rawText, err := readText(path)
	if err != nil {
		return nil, false, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(rawText), &raw); err != nil {
		return nil, true, tuskerError(errorConfigInvalid, "failed to parse config: "+err.Error(), withPath(path))
	}
	if _, ok := raw["orchestration"]; ok {
		return raw, true, tuskerError(errorConfigInvalid, "config uses deprecated top-level orchestration; use automation", withPath(path), withHint("rename orchestration: to automation: and keep trigger_states ready,rework"))
	}
	return raw, true, nil
}

func configRawMap(cfg v7TuskerConfigFile) map[string]any {
	raw, _ := yaml.Marshal(cfg)
	var out map[string]any
	_ = yaml.Unmarshal(raw, &out)
	return out
}

func decodeTuskerConfigRaw(raw map[string]any, path string) (v7TuskerConfigFile, error) {
	var cfg v7TuskerConfigFile
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(encoded, &cfg); err != nil {
		return cfg, tuskerError(errorConfigInvalid, "failed to decode config: "+err.Error(), withPath(path))
	}
	return cfg, nil
}

func cloneConfigRaw(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return map[string]any{}
	}
	var clone map[string]any
	if yaml.Unmarshal(encoded, &clone) != nil || clone == nil {
		return map[string]any{}
	}
	return clone
}

// mergeConfigRaw is deliberately defined over YAML values rather than Go
// structs. A key's presence is authority: false, 0, [], and {} all override
// lower layers. Non-empty maps merge recursively so a managed partial policy
// augments (rather than masks) legacy sibling fields.
func mergeConfigRaw(dst, src map[string]any) {
	for key, srcValue := range src {
		srcMap, sourceIsMap := srcValue.(map[string]any)
		dstMap, destinationIsMap := dst[key].(map[string]any)
		if sourceIsMap && destinationIsMap && len(srcMap) > 0 {
			mergeConfigRaw(dstMap, srcMap)
			continue
		}
		dst[key] = cloneConfigValue(srcValue)
	}
}

func applyRemovedProfiles(raw map[string]any) {
	automation := mapAny(raw["automation"])
	if automation == nil {
		return
	}
	removed := cleanProfileList(normalizeList(automation["removed_profiles"]))
	if len(removed) == 0 {
		return
	}
	profiles := mapAny(automation["profiles"])
	for _, name := range removed {
		delete(profiles, name)
	}
	if defaultProfile, _ := automation["default_profile"].(string); containsString(removed, defaultProfile) {
		delete(automation, "default_profile")
	}
	if laneProfiles := mapAny(automation["lane_profiles"]); laneProfiles != nil {
		for lane, name := range laneProfiles {
			if profile, _ := name.(string); containsString(removed, profile) {
				delete(laneProfiles, lane)
			}
		}
	}
	if levels := mapAny(automation["model_levels"]); levels != nil {
		for level, value := range levels {
			mapping := mapAny(value)
			if mapping == nil {
				continue
			}
			for _, lane := range []string{"execute", "review"} {
				mapping[lane] = removeProfileNames(normalizeList(mapping[lane]), removed)
			}
			levels[level] = mapping
		}
	}
	if routing, ok := automation["routing"].([]any); ok {
		filtered := routing[:0]
		for _, value := range routing {
			entry := mapAny(value)
			profile, _ := entry["profile"].(string)
			if !containsString(removed, profile) {
				filtered = append(filtered, value)
			}
		}
		automation["routing"] = filtered
	}
}

func removeProfileNames(profiles, removed []string) []string {
	out := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		if !containsString(removed, profile) {
			out = append(out, profile)
		}
	}
	return out
}

func cloneConfigValue(value any) any {
	encoded, err := yaml.Marshal(value)
	if err != nil {
		return value
	}
	var clone any
	if yaml.Unmarshal(encoded, &clone) != nil {
		return value
	}
	return clone
}

// Keep the machine-wide layer limited to operator-wide capacity knobs. Project
// behavior belongs in project or machine-local config, even when a global file
// happens to declare it.
var userGlobalConfigAllowlist = map[string]struct{}{
	"automation.concurrency.max_active_runs":             {},
	"automation.concurrency.max_active_runs_per_project": {},
	"automation.profiles":                                {},
	"automation.private_folders":                         {},
	"automation.removed_profiles":                        {},
	"automation.model_levels":                            {},
}

func configLayerAppliedRaw(layerName string, raw map[string]any) map[string]any {
	if layerName != configSourceUserGlobal {
		return cloneConfigRaw(raw)
	}
	filtered := map[string]any{}
	for key := range userGlobalConfigAllowlist {
		if value, present := lookupConfigValue(raw, key); present {
			setNestedConfigValue(filtered, key, value)
		}
	}
	return filtered
}

func appliedConfigRaw(layer tuskerConfigLayer) map[string]any {
	if layer.AppliedRaw != nil {
		return layer.AppliedRaw
	}
	return layer.Raw
}

func userGlobalTuskerConfigPath() string {
	if explicit := strings.TrimSpace(os.Getenv("TUSKER_CONFIG")); explicit != "" {
		return explicit
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "tusker", "config.yaml")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "tusker", "config.yaml")
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".config", "tusker", "config.yaml")
	}
	return filepath.Join(".config", "tusker", "config.yaml")
}

func validateTuskerConfigLayer(layer tuskerConfigLayer) error {
	if !layer.Present {
		return nil
	}
	if err := validateAgentAccessRaw(appliedConfigRaw(layer), layer.Path); err != nil {
		return err
	}
	_, tierPresent := lookupConfigValue(appliedConfigRaw(layer), "tier")
	if (tierPresent || layer.Config.Tier != 0) && (layer.Config.Tier < 1 || layer.Config.Tier > 5) {
		return tuskerError(errorConfigInvalid, "tier must be between 1 and 5", withPath(layer.Path))
	}
	if err := validateCompletionReactorModeLayer(layer); err != nil {
		return err
	}
	if err := validateProjectLayerDefinesNoProfiles(layer); err != nil {
		return err
	}
	for _, rule := range layer.Config.Automation.Denylist {
		if strings.TrimSpace(rule.ID) == "" || strings.TrimSpace(rule.Pattern) == "" {
			return tuskerError(errorConfigInvalid, "automation.denylist entries require id and pattern", withPath(layer.Path))
		}
	}
	if layer.Config.Automation.Concurrency.MaxActiveRuns < 0 || layer.Config.Automation.Concurrency.MaxActiveRunsPerProject < 0 {
		return tuskerError(errorConfigInvalid, "automation.concurrency limits must be > 0 when set", withPath(layer.Path))
	}
	return nil
}

// Runner profiles are a machine-wide library: only the user-global config (and
// built-ins) may define or remove them. Project and machine-local layers only
// select global profiles by name.
func validateProjectLayerDefinesNoProfiles(layer tuskerConfigLayer) error {
	if layer.Name != configSourceProject && layer.Name != configSourceLocal {
		return nil
	}
	raw := appliedConfigRaw(layer)
	for _, key := range []string{"automation.profiles", "automation.removed_profiles"} {
		value, present := lookupConfigValue(raw, key)
		if !present {
			continue
		}
		names := sortedBootstrapMapKeys(mapAny(value), nil)
		if key == "automation.removed_profiles" {
			names = cleanProfileList(normalizeList(value))
		}
		what := key
		if len(names) > 0 {
			what += " (" + strings.Join(names, ", ") + ")"
		}
		if key == "automation.removed_profiles" {
			return tuskerError(errorConfigInvalid,
				fmt.Sprintf("%s sets %s; a project cannot remove global runner profiles. To stop using them here, select other profiles in this project's automation.model_levels; to delete them for every project, remove them from the global config %s", layer.Path, what, userGlobalTuskerConfigPath()),
				withPath(layer.Path),
				withHint("delete automation.removed_profiles from "+layer.Path+"; use `tusker models profile-remove --scope global --name <profile>` to remove a profile globally"))
		}
		return tuskerError(errorConfigInvalid,
			fmt.Sprintf("%s defines %s; runner profiles may only be defined in the global config %s. Move the profile definitions there and reference them by name from this project (automation.model_levels, automation.default_profile, or task runner_profile/execute_profile/review_profile)", layer.Path, what, userGlobalTuskerConfigPath()),
			withPath(layer.Path),
			withHint("move automation.profiles entries to "+userGlobalTuskerConfigPath()+" and delete them from "+layer.Path))
	}
	return nil
}

// validateResolvedTuskerConfig rejects structurally invalid config. Profile
// references (default_profile, lane_profiles, model_levels, routing) that name
// a profile the global config does not define are returned as warnings, not
// errors: profiles live only in the machine's global config, so a fresh clone
// on a machine that lacks them must still load. Route resolution (wave start,
// dispatch, work start) refuses the unknown profile when it is actually used.
func validateResolvedTuskerConfig(cfg v7TuskerConfigFile, layers []tuskerConfigLayer) ([]string, error) {
	profiles := runnerProfilesFromSchema(cfg.Automation.Profiles)
	for name, profile := range profiles {
		if err := validateRunnerProfileDefinition(strings.TrimSpace(name), profile, sourcePathForConfigKey(layers, "automation.profiles."+strings.TrimSpace(name))); err != nil {
			return nil, err
		}
	}
	var warnings []string
	unknown := func(key, profile string) {
		if _, ok := profiles[strings.TrimSpace(profile)]; !ok {
			warnings = append(warnings, fmt.Sprintf("%s (%s) references unknown profile %s; define it in the global config %s", key, sourcePathForConfigKey(layers, key), profile, userGlobalTuskerConfigPath()))
		}
	}
	if strings.TrimSpace(cfg.Automation.DefaultProfile) != "" {
		unknown("automation.default_profile", cfg.Automation.DefaultProfile)
	}
	for lane, profile := range cfg.Automation.LaneProfiles {
		unknown("automation.lane_profiles."+strings.TrimSpace(lane), profile)
	}
	for level, mapping := range cfg.Automation.ModelLevels {
		if !validModelLevel(level) {
			return nil, tuskerError(errorConfigInvalid, "automation.model_levels has unknown level "+level)
		}
		for _, profile := range append(append([]string{}, mapping.Execute...), mapping.Review...) {
			unknown("automation.model_levels."+level, profile)
		}
	}
	for _, rule := range cfg.Automation.Routing {
		if strings.TrimSpace(rule.Profile) == "" {
			path := sourcePathForConfigKey(layers, "automation.routing")
			return nil, tuskerError(errorConfigInvalid, "automation.routing rule "+rule.Name+" is missing profile", withPath(path))
		}
		unknown("automation.routing", rule.Profile)
	}
	sort.Strings(warnings)
	return warnings, nil
}

func validateRunnerProfileDefinition(name string, profile RunnerProfileDefinition, path string) error {
	if strings.TrimSpace(name) == "" {
		return tuskerError(errorConfigInvalid, "automation.profiles contains an empty profile name", withPath(path))
	}
	harness := RunnerName(strings.TrimSpace(profile.Harness))
	switch harness {
	case RunnerCodexAppServer:
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.harness uses retired value %q", name, profile.Harness), withPath(path), withHint("migrate to codex_exec or configure an operator-installed acp_v1 endpoint"))
	case RunnerCodex, RunnerCodexExec, RunnerCodexCloud, RunnerMuse, RunnerClaude, RunnerACP, RunnerDevin, RunnerCodexACP:
	default:
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.harness has unsupported value %q", name, profile.Harness), withPath(path), withHint("use codex_exec, muse, claude-code, codex_cloud, or an operator-installed acp_v1 endpoint"))
	}
	if profile.NativeContainment && harness != RunnerACP {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.native_containment is valid only for acp_v1", name), withPath(path))
	}
	if !validRunnerModelName(profile.Model) {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.model has unsupported value %q", name, profile.Model), withPath(path), withHint("use a known model family such as gpt-5.x, claude-opus-4-8, claude-fable-5, sonnet-4.6, or glm-5.2"))
	}
	if strings.TrimSpace(profile.Effort) != "" && (!validRunnerEffort(profile.Effort) || (harness == RunnerClaude && strings.EqualFold(strings.TrimSpace(profile.Effort), "ultra"))) {
		allowed := "low, medium, high, xhigh, max, ultra"
		if harness == RunnerClaude {
			allowed = "low, medium, high, xhigh, max"
		}
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.effort must be one of %s", name, allowed), withPath(path))
	}
	if profile.Access == nil {
		if !validRunnerSandboxMode(profile.Sandbox.Mode) {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.sandbox.mode must be one of read-only, workspace-write, danger-full-access", name), withPath(path))
		}
		if !validPermissionPreset(profile.PermissionPreset) {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.permission_preset has unsupported value %q", name, profile.PermissionPreset), withPath(path))
		}
	}
	if profile.Subagents.MaxConcurrent < 0 {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.subagents.max_concurrent must be >= 0", name), withPath(path))
	}
	for _, tier := range profile.EligibleTiers {
		if !validModelLevel(tier) {
			return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.eligible_tiers contains unsupported tier %q", name, tier), withPath(path), withHint("use light, standard, or demanding"))
		}
	}
	if err := validateAgentAccessDefinition(name, profile.Access, path); err != nil {
		return err
	}
	if profile.Access != nil && strings.TrimSpace(profile.PermissionPreset) != "" {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s cannot define both access and permission_preset", name), withPath(path), withHint("remove permission_preset when using access"))
	}
	return nil
}

func validRunnerModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	return model != "" && !strings.ContainsAny(model, " \t\r\n")
}

func validRunnerEffort(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high", "xhigh", "max", "ultra":
		return true
	default:
		return false
	}
}

func validRunnerSandboxMode(mode string) bool {
	switch strings.TrimSpace(mode) {
	case "read-only", "workspace-write", "danger-full-access":
		return true
	default:
		return false
	}
}

func validPermissionPreset(preset string) bool {
	switch strings.TrimSpace(preset) {
	case "", "read-only", "workspace-write-network", "workspace-write-offline", "danger-full-access":
		return true
	default:
		return false
	}
}

func runnerProfilesFromSchema(in map[string]v7schema.TuskerRunnerProfileConfig) map[string]RunnerProfileDefinition {
	out := map[string]RunnerProfileDefinition{}
	for name, profile := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		out[name] = runnerProfileFromSchema(profile)
	}
	return out
}

func runnerProfileSourcesFromLayers(profiles map[string]RunnerProfileDefinition, layers []tuskerConfigLayer) map[string]string {
	out := make(map[string]string, len(profiles))
	for name := range profiles {
		source := configSourceBuiltIn
		for _, layer := range layers {
			if runnerProfileExplicitInLayer(layer.Raw, name) {
				source = layer.Name
			}
		}
		out[name] = source
	}
	return out
}

func runnerProfileExplicitInLayer(raw map[string]any, name string) bool {
	base := "automation.profiles." + name + "."
	hasAll := func(fields ...string) bool {
		for _, field := range fields {
			if _, present := lookupConfigValue(raw, base+field); !present {
				return false
			}
		}
		return true
	}
	if hasAll(
		"harness",
		"model",
		"effort",
		"permission_preset",
		"sandbox.mode",
		"sandbox.network",
		"subagents.allowed",
		"subagents.max_concurrent",
	) {
		return true
	}
	return hasAll(
		"harness",
		"model",
		"effort",
		"access.schema",
		"access.mode",
		"access.network",
		"access.destructive_actions",
	)
}

func runnerProfileFromSchema(profile v7schema.TuskerRunnerProfileConfig) RunnerProfileDefinition {
	var tiers []string
	if profile.EligibleTiers != nil {
		tiers = cleanProfileList(profile.EligibleTiers)
	}
	return RunnerProfileDefinition{
		Disabled:          profile.Disabled,
		DisplayName:       strings.TrimSpace(profile.DisplayName),
		EligibleTiers:     tiers,
		Harness:           strings.TrimSpace(profile.Harness),
		Model:             strings.TrimSpace(profile.Model),
		Effort:            strings.TrimSpace(profile.Effort),
		PermissionPreset:  strings.TrimSpace(profile.PermissionPreset),
		Command:           strings.TrimSpace(profile.Command),
		NativeContainment: profile.NativeContainment,
		Sandbox: RunnerSandboxDefinition{
			Mode:    strings.TrimSpace(profile.Sandbox.Mode),
			Network: profile.Sandbox.Network,
		},
		Subagents: RunnerSubagentPolicyDefinition{
			Allowed:       profile.Subagents.Allowed,
			MaxConcurrent: profile.Subagents.MaxConcurrent,
		},
		Access: agentAccessFromSchema(profile.Access),
	}
}

func runnerRoutingFromSchema(in []v7schema.TuskerAutomationRoutingRuleConfig) []RunnerRoutingRule {
	out := make([]RunnerRoutingRule, 0, len(in))
	for _, rule := range in {
		out = append(out, RunnerRoutingRule{
			Name:    strings.TrimSpace(rule.Name),
			Profile: strings.TrimSpace(rule.Profile),
			Match: RunnerRoutingMatch{
				Epic:          rule.Match.Epic,
				Risk:          rule.Match.Risk,
				Size:          rule.Match.Size,
				Domains:       rule.Match.Domains,
				TitleKeywords: rule.Match.TitleKeywords,
			},
		})
	}
	return out
}

func runnerDenylistFromSchema(in []v7schema.TuskerAutomationDenyRuleConfig) []RunnerDenyRule {
	out := make([]RunnerDenyRule, 0, len(in))
	for _, rule := range in {
		out = append(out, RunnerDenyRule{
			ID:                   strings.TrimSpace(rule.ID),
			Pattern:              strings.TrimSpace(rule.Pattern),
			Description:          strings.TrimSpace(rule.Description),
			CodexExecPolicy:      strings.TrimSpace(rule.CodexExecPolicy),
			ClaudePermissionRule: strings.TrimSpace(rule.ClaudePermissionRule),
			PreToolUse:           strings.TrimSpace(rule.PreToolUse),
		})
	}
	return out
}

func resolveRunnerProfileForNote(note Note, wf Workflow, lane string) (ResolvedRunnerProfile, error) {
	lane = firstNonEmpty(strings.TrimSpace(lane), runLaneExecute)
	// An explicitly present but blank work_level is an authored refusal to
	// classify the task. Check it before any task profile or legacy runner
	// override so persisted compatibility fields cannot turn an unclassified
	// task into runnable work. This check is deliberately independent of
	// modelLevelForNote: a valid review_level must not hide a blank work_level.
	if explicitUnclassifiedWorkLevel(note) {
		return ResolvedRunnerProfile{}, tuskerError(errorConfigInvalid, "work_level is required for agent work; use light, standard, or demanding")
	}
	// Keep the model-level validator here as well so an explicit invalid review
	// level cannot be hidden by a lane profile override.
	if _, _, levelErr := modelLevelForNote(note, lane); levelErr != nil {
		return ResolvedRunnerProfile{}, levelErr
	}
	if complexity := strings.TrimSpace(stringField(note.Data, "complexity")); complexity != "" && !validTaskComplexity(complexity) {
		return ResolvedRunnerProfile{}, tuskerError(errorConfigInvalid, "task complexity must be routine, standard, complex, or frontier")
	}
	profiles := wf.RunnerProfiles
	if len(profiles) == 0 {
		profiles = runnerProfilesFromSchema(builtInTuskerConfig().Automation.Profiles)
	}
	pick := func(name, source, reason, ruleName string) (ResolvedRunnerProfile, bool, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return ResolvedRunnerProfile{}, false, nil
		}
		profile, ok := profiles[name]
		if !ok {
			return ResolvedRunnerProfile{}, true, tuskerError(errorConfigInvalid, "runner profile "+name+" is not defined")
		}
		if profile.Disabled {
			return ResolvedRunnerProfile{}, true, tuskerError(errorInvalidTransition, "runner profile "+name+" is disabled", withHint("enable it or select another configured profile"))
		}
		return ResolvedRunnerProfile{Name: name, Source: source, Reason: reason, RuleName: ruleName, Definition: profile}, true, nil
	}
	profileField := "execute_profile"
	if lane == runLaneReview {
		profileField = "review_profile"
	}
	if selected, ok, err := pick(stringField(note.Data, profileField), "task frontmatter", profileField, ""); ok || err != nil {
		return selected, err
	}
	if selected, ok, err := pick(stringField(note.Data, "runner_profile"), "task frontmatter", "runner_profile (legacy)", ""); ok || err != nil {
		return selected, err
	}
	for _, rule := range wf.RunnerRouting {
		if runnerRoutingRuleMatches(rule, note) {
			selected, _, err := pick(rule.Profile, "automation.routing", "routing rule", rule.Name)
			return selected, err
		}
	}
	if wf.RunnerLaneProfiles != nil {
		if selected, ok, err := pick(wf.RunnerLaneProfiles[lane], "automation.lane_profiles", "lane mapping", ""); ok || err != nil {
			return selected, err
		}
	}
	levelField := "work_level"
	if lane == runLaneReview && strings.TrimSpace(stringField(note.Data, "review_level")) != "" {
		levelField = "review_level"
	}
	if strings.TrimSpace(stringField(note.Data, levelField)) != "" {
		candidates, levelErr := modelLevelProfiles(note, wf, lane)
		if levelErr != nil {
			return ResolvedRunnerProfile{}, levelErr
		}
		selected := candidates[0]
		for _, candidate := range candidates[1:] {
			selected.Fallbacks = append(selected.Fallbacks, candidate.Name)
		}
		return selected, nil
	}
	if role := semanticRunnerRole(stringField(note.Data, "complexity"), lane); role != "" {
		if selected, ok, err := pick(role, "task complexity", "semantic complexity role", ""); ok || err != nil {
			if err == nil {
				return selected, nil
			}
			// A missing legacy semantic profile allows the three-level mapping to
			// resolve; an explicitly authored but invalid profile remains an error.
			if !strings.Contains(err.Error(), "is not defined") {
				return selected, err
			}
			if len(wf.ModelLevels) == 0 {
				return selected, err
			}
		}
	}
	if candidates, err := modelLevelProfiles(note, wf, lane); err == nil && len(candidates) > 0 {
		selected := candidates[0]
		for _, candidate := range candidates[1:] {
			selected.Fallbacks = append(selected.Fallbacks, candidate.Name)
		}
		return selected, nil
	} else if err != nil && len(wf.ModelLevels) > 0 {
		return ResolvedRunnerProfile{}, err
	}
	defaultSource := "automation.default_profile"
	defaultReason := "project default"
	if strings.TrimSpace(wf.RunnerDefaultProfile) == "default" {
		defaultSource = configSourceBuiltIn
		defaultReason = "built-in default"
	}
	if selected, ok, err := pick(wf.RunnerDefaultProfile, defaultSource, defaultReason, ""); ok || err != nil {
		return selected, err
	}
	selected, _, err := pick("default", configSourceBuiltIn, "built-in default", "")
	return selected, err
}

func validTaskComplexity(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "routine", "standard", "complex", "frontier":
		return true
	default:
		return false
	}
}

func explicitUnclassifiedWorkLevel(note Note) bool {
	_, present := note.Data["work_level"]
	return present && strings.TrimSpace(stringField(note.Data, "work_level")) == ""
}

// An absent complexity preserves compatibility by using the configured default.
// Review remains independent when no stronger explicit policy selected a profile.
func semanticRunnerRole(complexity, lane string) string {
	complexity = strings.ToLower(strings.TrimSpace(complexity))
	if complexity == "" {
		return ""
	}
	if strings.TrimSpace(lane) == runLaneReview {
		return "review-independent"
	}
	switch complexity {
	case "routine":
		return "execute-fast"
	case "standard":
		return "execute-standard"
	case "complex":
		return "execute-complex"
	case "frontier":
		return "execute-frontier"
	default:
		return ""
	}
}

func resolveRunProfileForLane(note Note, wf Workflow, lane, legacyRunner string) (ResolvedRunnerProfile, error) {
	selected, err := resolveRunnerProfileForNote(note, wf, lane)
	if err != nil {
		return selected, err
	}
	legacyRunner = strings.TrimSpace(legacyRunner)
	if selected.Source == configSourceBuiltIn && legacyRunner != "" {
		if strings.TrimSpace(lane) == runLaneReview || legacyRunner != strings.TrimSpace(selected.Definition.Harness) {
			selected = ResolvedRunnerProfile{
				Reason: fmt.Sprintf("legacy runner fallback: using %q because no named profile was selected", legacyRunner),
				Definition: RunnerProfileDefinition{
					Harness: legacyRunner,
				},
			}
		}
	}
	if strings.TrimSpace(lane) == runLaneReview {
		selected.Warnings = reviewerProfileWarnings(note, wf, selected)
		if warning := strings.TrimSpace(wf.Reviewer.FallbackWarning); warning != "" {
			selected.Warnings = append([]string{warning}, selected.Warnings...)
		}
	}
	return selected, nil
}

func runnerVendor(harness string) string {
	harness = strings.ToLower(strings.TrimSpace(harness))
	switch {
	case strings.HasPrefix(harness, "codex"):
		return "codex"
	case strings.HasPrefix(harness, "claude"):
		return "claude"
	case strings.HasPrefix(harness, "muse"):
		return "muse"
	default:
		return harness
	}
}

func reviewerProfileWarnings(note Note, wf Workflow, reviewer ResolvedRunnerProfile) []string {
	implementer, err := resolveRunnerProfileForNote(note, wf, runLaneExecute)
	if err != nil {
		return nil
	}
	var warnings []string
	reviewerVendor := runnerVendor(reviewer.Definition.Harness)
	implementerVendor := runnerVendor(implementer.Definition.Harness)
	if reviewerVendor != "" && reviewerVendor == implementerVendor {
		warnings = append(warnings, fmt.Sprintf("reviewer vendor %q matches implementer vendor %q", reviewerVendor, implementerVendor))
	}
	reviewerModel := strings.TrimSpace(reviewer.Definition.Model)
	implementerModel := strings.TrimSpace(implementer.Definition.Model)
	if reviewerModel != "" && reviewerModel == implementerModel {
		warnings = append(warnings, fmt.Sprintf("reviewer model %q matches implementer model", reviewerModel))
	}
	return uniqueStrings(warnings)
}

func applyResolvedProfileToRun(run RunStatus, selected ResolvedRunnerProfile) RunStatus {
	harness := strings.TrimSpace(selected.Definition.Harness)
	if harness != "" {
		run.Runner = harness
		run.RunnerHarness = harness
	}
	run.RunnerProfile = strings.TrimSpace(selected.Name)
	run.RunnerModel = strings.TrimSpace(selected.Definition.Model)
	run.RunnerEffort = strings.TrimSpace(selected.Definition.Effort)
	if strings.TrimSpace(run.RunnerHarness) == "" {
		run.RunnerHarness = run.Runner
	}
	return run
}

func commandForRunnerProfile(baseCommand string, selected ResolvedRunnerProfile) string {
	profile := selected.Definition
	command := firstNonEmpty(strings.TrimSpace(profile.Command), strings.TrimSpace(baseCommand))
	if command == "" {
		return command
	}
	harness := RunnerName(strings.TrimSpace(profile.Harness))
	model := strings.TrimSpace(profile.Model)
	effort := strings.TrimSpace(profile.Effort)
	switch harness {
	case RunnerCodexExec:
		if model != "" && !commandHasFlag(command, "--model") && !commandHasFlag(command, "-m") {
			command += " --model " + model
		}
		if effort != "" && !commandHasCodexConfig(command, "model_reasoning_effort") {
			command += ` -c 'model_reasoning_effort="` + effort + `"'`
		}
	case RunnerClaude:
		if model != "" && !commandHasFlag(command, "--model") {
			command += " --model " + model
		}
		if effort != "" && !commandHasFlag(command, "--effort") {
			command += " --effort " + effort
		}
	case RunnerMuse:
		if model != "" && !commandHasFlag(command, "--model") {
			command += " --model " + model
		}
		if effort != "" && !commandHasFlag(command, "--reasoning-effort") {
			command += " --reasoning-effort " + effort
		}
	}
	return command
}

func commandHasCodexConfig(command, key string) bool {
	fields := strings.Fields(command)
	for i, field := range fields {
		if (field == "-c" || field == "--config") && i+1 < len(fields) && strings.HasPrefix(fields[i+1], key+"=") {
			return true
		}
		if strings.HasPrefix(field, "--config="+key+"=") {
			return true
		}
	}
	return false
}

func commandHasFlag(command, flag string) bool {
	for _, part := range strings.Fields(command) {
		if part == flag || strings.HasPrefix(part, flag+"=") {
			return true
		}
	}
	return false
}

func codexPolicyForResolvedProfile(base CodexPolicy, lane string, selected ResolvedRunnerProfile) CodexPolicy {
	policy := codexPolicyForLane(base, lane)
	profile := selected.Definition
	if strings.TrimSpace(selected.Name) == "" {
		return policy
	}
	if profile.Access != nil {
		policy.CommandPolicy = runnercore.NewCommandPolicy(profile.Access.Mode == accessModeReview, profile.Access.DestructiveActions)
	}
	switch RunnerName(strings.TrimSpace(profile.Harness)) {
	case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec, RunnerACP, RunnerDevin, RunnerCodexACP:
	default:
		return policy
	}
	if profile.Access != nil {
		policy.ThreadSandbox, policy.TurnSandboxPolicy = "workspace-write", "workspace-write"
		policy.TurnSandboxNetwork = boolPtr(profile.Access.Network)
		if profile.Access.Mode == accessModeReview {
			policy.ApprovalPolicy = "never"
			policy.ThreadSandbox, policy.TurnSandboxPolicy = "read-only", "read-only"
		} else if profile.Access.DestructiveActions == "ask" {
			policy.ApprovalPolicy = "on-request"
		} else {
			policy.ApprovalPolicy = "never"
		}
		return policy
	}
	if strings.TrimSpace(profile.Sandbox.Mode) != "" {
		policy.ThreadSandbox = profile.Sandbox.Mode
		policy.TurnSandboxPolicy = profile.Sandbox.Mode
	}
	if profile.Sandbox.Network != nil {
		policy.TurnSandboxNetwork = profile.Sandbox.Network
	}
	switch strings.TrimSpace(profile.PermissionPreset) {
	case "danger-full-access":
		policy.ApprovalPolicy = "never"
		if strings.TrimSpace(profile.Sandbox.Mode) == "" {
			policy.ThreadSandbox = "danger-full-access"
			policy.TurnSandboxPolicy = "danger-full-access"
		}
	case "workspace-write-network":
		policy.ApprovalPolicy = "never"
		if strings.TrimSpace(profile.Sandbox.Mode) == "" {
			policy.ThreadSandbox = "workspace-write"
			policy.TurnSandboxPolicy = "workspace-write"
		}
		policy.TurnSandboxNetwork = boolPtr(true)
	case "workspace-write-offline":
		policy.ApprovalPolicy = "never"
		if strings.TrimSpace(profile.Sandbox.Mode) == "" {
			policy.ThreadSandbox = "workspace-write"
			policy.TurnSandboxPolicy = "workspace-write"
		}
		policy.TurnSandboxNetwork = boolPtr(false)
	case "read-only":
		if strings.TrimSpace(profile.Sandbox.Mode) == "" {
			policy.ThreadSandbox = "read-only"
			policy.TurnSandboxPolicy = "read-only"
		}
		policy.TurnSandboxNetwork = boolPtr(false)
	}
	return policy
}

func runnerRoutingRuleMatches(rule RunnerRoutingRule, note Note) bool {
	match := rule.Match
	if !routingFieldMatches(match.Epic, stringField(note.Data, "epic")) {
		return false
	}
	if !routingFieldMatches(match.Risk, stringField(note.Data, "risk")) {
		return false
	}
	if !routingFieldMatches(match.Size, stringField(note.Data, "size")) {
		return false
	}
	if len(normalizeList(match.Domains)) > 0 {
		noteDomains := normalizeList(note.Data["domains"])
		if !anyStringOverlap(normalizeList(match.Domains), noteDomains) {
			return false
		}
	}
	keywords := normalizeList(match.TitleKeywords)
	if len(keywords) > 0 {
		title := strings.ToLower(stringField(note.Data, "title"))
		found := false
		for _, keyword := range keywords {
			if strings.Contains(title, strings.ToLower(strings.TrimSpace(keyword))) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func routingFieldMatches(match any, value string) bool {
	values := normalizeList(match)
	if len(values) == 0 {
		return true
	}
	for _, candidate := range values {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(value)) {
			return true
		}
	}
	return false
}

func anyStringOverlap(left, right []string) bool {
	for _, l := range left {
		for _, r := range right {
			if strings.EqualFold(strings.TrimSpace(l), strings.TrimSpace(r)) {
				return true
			}
		}
	}
	return false
}

func configResolve(vaultPath, key string) (configResolveReport, error) {
	return configResolveForPaths(v7RepoRoot(vaultPath), vaultPath, true, key)
}

func configResolveForRepo(repoRoot string, includeProject bool, key string) (configResolveReport, error) {
	vaultPath := filepath.Join(repoRoot, defaultRepoVaultDir)
	if includeProject && strings.TrimSpace(repoRoot) != "" {
		if discovered, err := discoverVault(repoRoot); err == nil && strings.TrimSpace(discovered) != "" {
			vaultPath = discovered
		}
	}
	return configResolveForPaths(repoRoot, vaultPath, includeProject, key)
}

func configResolveForPaths(repoRoot, vaultPath string, includeProject bool, key string) (configResolveReport, error) {
	resolved, err := resolveTuskerConfigForPaths(repoRoot, vaultPath, includeProject)
	if err != nil {
		return configResolveReport{}, err
	}
	lookup := canonicalConfigLookupKey(key)
	effective, effectivePresent := lookupConfigValue(resolved.Raw, lookup)
	// Automation is opt-in at the project layer. Keep the resolved API honest
	// about that default without making the built-in raw document behavioral.
	if lookup == "automation.enabled" && !effectivePresent {
		effective = false
	}
	report := configResolveReport{Key: key, Lookup: lookup, Value: effective}
	var winner *configResolveSourceValue
	for _, layer := range resolved.Layers {
		value, present := lookupConfigValue(layer.Raw, lookup)
		_, applied := lookupConfigValue(appliedConfigRaw(layer), lookup)
		entry := configResolveSourceValue{
			Source:  layer.Name,
			Path:    layer.Path,
			Present: present,
			Value:   value,
		}
		if layer.Name == configSourceUserGlobal && present && !applied {
			entry.Note = "ignored at user-global layer; behavioral settings must be project-local"
		}
		report.Sources = append(report.Sources, entry)
		if applied {
			candidate := report.Sources[len(report.Sources)-1]
			winner = &candidate
		}
	}
	if winner == nil {
		report.Source = configSourceBuiltIn
	} else {
		report.Source = winner.Source
		report.Path = winner.Path
		for i := range report.Sources {
			if report.Sources[i].Source == winner.Source && report.Sources[i].Path == winner.Path {
				report.Sources[i].Winning = true
			}
		}
	}
	return report, nil
}

func writeConfigValue(path, key string, value any) error {
	if strings.TrimSpace(path) == "" {
		return tuskerError(errorConfigInvalid, "config path is empty")
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	raw := map[string]any{}
	if fileExists(path) {
		text, err := readText(path)
		if err != nil {
			return err
		}
		if strings.TrimSpace(text) != "" {
			if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
				return tuskerError(errorConfigInvalid, "failed to parse config before writing: "+err.Error(), withPath(path))
			}
		}
	}
	setNestedConfigValue(raw, canonicalConfigLookupKey(key), value)
	out, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return writeConfigTextAtomically(path, string(out))
}

func writeConfigTextAtomically(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return tuskerError(errorConfigInvalid, "config path is empty")
	}
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.WriteString(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	invalidateCachedNote(path)
	recordCLIVaultMutation(path)
	return nil
}

func setNestedConfigValue(raw map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	current := raw
	for _, part := range parts[:len(parts)-1] {
		next, _ := current[part].(map[string]any)
		if next == nil {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func setProjectLocalConfigWithReadback(vaultPath, key string, value any) (configResolveReport, error) {
	projectLocalConfigWriteMu.Lock()
	defer projectLocalConfigWriteMu.Unlock()
	return setProjectLocalConfigWithReadbackUnlocked(vaultPath, key, value)
}

func setProjectLocalConfigWithReadbackUnlocked(vaultPath, key string, value any) (configResolveReport, error) {
	repoRoot := v7RepoRoot(vaultPath)
	before, err := configResolveForPaths(repoRoot, vaultPath, true, key)
	if err != nil {
		return configResolveReport{}, err
	}
	path := managedTuskerLocalConfigPath(vaultPath)
	previous, existed, err := readConfigText(path)
	if err != nil {
		return configResolveReport{}, err
	}
	postWriteRaw, err := configRawWithValue(path, key, value)
	if err != nil {
		return configResolveReport{}, err
	}
	if _, err := resolveTuskerConfigForPathsWithOverrides(repoRoot, vaultPath, true, map[string]map[string]any{path: postWriteRaw}); err != nil {
		return configResolveReport{}, err
	}
	if err := writeConfigValue(path, key, value); err != nil {
		return configResolveReport{}, err
	}
	after, err := configResolveForPaths(repoRoot, vaultPath, true, key)
	if err != nil {
		_ = restoreConfigText(path, previous, existed)
		return configResolveReport{}, err
	}
	if !configValueChanged(before.Value, after.Value) {
		_ = restoreConfigText(path, previous, existed)
		return after, tuskerError(errorConfigInvalid, "config setter no-op: effective value for "+key+" is unchanged", withPath(path), withContext(map[string]any{"key": key, "value": after.Value}))
	}
	if after.Source != configSourceLocal {
		_ = restoreConfigText(path, previous, existed)
		return after, tuskerError(errorConfigInvalid, "config setter failed trigger-eval: machine-local override did not win for "+key, withPath(path), withContext(map[string]any{"key": key, "winner": after.Source, "value": after.Value}))
	}
	return after, nil
}

func readConfigText(path string) (string, bool, error) {
	if !fileExists(path) {
		return "", false, nil
	}
	text, err := readText(path)
	return text, true, err
}

func restoreConfigText(path, content string, existed bool) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeConfigTextAtomically(path, content)
}

func configRawWithValue(path, key string, value any) (map[string]any, error) {
	raw := map[string]any{}
	if fileExists(path) {
		text, err := readText(path)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(text) != "" {
			if err := yaml.Unmarshal([]byte(text), &raw); err != nil {
				return nil, tuskerError(errorConfigInvalid, "failed to parse config before writing: "+err.Error(), withPath(path))
			}
		}
	}
	setNestedConfigValue(raw, canonicalConfigLookupKey(key), value)
	return raw, nil
}

func setUserGlobalConfigWithReadback(key string, value any) (configResolveReport, error) {
	userGlobalConfigWriteMu.Lock()
	defer userGlobalConfigWriteMu.Unlock()
	canonical := canonicalConfigLookupKey(key)
	if !userGlobalConfigKeyAllowed(canonical) {
		return configResolveReport{}, tuskerError(errorConfigInvalid, "user-global config does not allow behavioral key "+key)
	}
	before, err := configResolveForRepo("", false, key)
	if err != nil {
		return configResolveReport{}, err
	}
	path := userGlobalTuskerConfigPath()
	previous, existed, err := readConfigText(path)
	if err != nil {
		return configResolveReport{}, err
	}
	postWriteRaw, err := configRawWithValue(path, key, value)
	if err != nil {
		return configResolveReport{}, err
	}
	if _, err := resolveTuskerConfigForPathsWithOverrides("", filepath.Join("", defaultRepoVaultDir), false, map[string]map[string]any{path: postWriteRaw}); err != nil {
		return configResolveReport{}, err
	}
	if err := writeConfigValue(path, key, value); err != nil {
		return configResolveReport{}, err
	}
	after, err := configResolveForRepo("", false, key)
	if err != nil {
		_ = restoreConfigText(path, previous, existed)
		return configResolveReport{}, err
	}
	if !configValueChanged(before.Value, after.Value) {
		_ = restoreConfigText(path, previous, existed)
		return after, tuskerError(errorConfigInvalid, "config setter no-op: effective value for "+key+" is unchanged", withPath(path), withContext(map[string]any{"key": key, "value": after.Value}))
	}
	if after.Source != configSourceUserGlobal {
		_ = restoreConfigText(path, previous, existed)
		return after, tuskerError(errorConfigInvalid, "config setter failed trigger-eval: user-global config did not win for "+key, withPath(path), withContext(map[string]any{"key": key, "winner": after.Source, "value": after.Value}))
	}
	return after, nil
}

func userGlobalConfigKeyAllowed(key string) bool {
	if _, ok := userGlobalConfigAllowlist[key]; ok {
		return true
	}
	return strings.HasPrefix(key, "automation.profiles.") || strings.HasPrefix(key, "automation.model_levels.")
}

func canonicalConfigLookupKey(key string) string {
	key = strings.TrimSpace(key)
	switch key {
	case "runtime.max_active_runs":
		return "automation.concurrency.max_active_runs"
	case "runtime.max_active_runs_per_project":
		return "automation.concurrency.max_active_runs_per_project"
	case "workspace.strategy":
		return "automation.workspace.strategy"
	default:
		return key
	}
}

func lookupConfigValue(raw map[string]any, key string) (any, bool) {
	if raw == nil {
		return nil, false
	}
	var current any = raw
	for _, part := range strings.Split(key, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func sourcePathForConfigKey(layers []tuskerConfigLayer, key string) string {
	lookup := canonicalConfigLookupKey(key)
	for i := len(layers) - 1; i >= 0; i-- {
		if _, ok := lookupConfigValue(layers[i].Raw, lookup); ok {
			return layers[i].Path
		}
	}
	return ""
}

func resolvedConfigKeyPresent(resolved resolvedTuskerConfig, key string) bool {
	_, present := lookupConfigValue(resolved.Raw, canonicalConfigLookupKey(key))
	return present
}

func configValueChanged(before, after any) bool {
	return !reflect.DeepEqual(before, after)
}
