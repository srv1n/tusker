package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"tusker/internal/v7schema"

	"gopkg.in/yaml.v3"
)

const (
	configSourceBuiltIn    = "built-in defaults"
	configSourceUserGlobal = "user-global config"
	configSourceProject    = "project config"
	configSourceLocal      = "machine-local config"
)

const (
	managedTuskerConfigName      = "config.yaml"
	managedTuskerLocalConfigName = "config.local.yaml"
	legacyTuskerConfigName       = "tusker.yaml"
	legacyTuskerLocalConfigName  = "tusker.local.yaml"
)

func managedTuskerConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, managedTuskerConfigName)
}

func managedTuskerLocalConfigPath(vaultPath string) string {
	return filepath.Join(vaultPath, managedTuskerLocalConfigName)
}

func legacyTuskerConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, legacyTuskerConfigName)
}

func legacyTuskerLocalConfigPath(repoRoot string) string {
	return filepath.Join(repoRoot, legacyTuskerLocalConfigName)
}

// preferredTuskerConfigPath retains root-level config only as a compatibility
// read path. New repositories and all new local overrides live in the vault.
func preferredTuskerConfigPath(vaultPath string) string {
	managed := managedTuskerConfigPath(vaultPath)
	if fileExists(managed) {
		return managed
	}
	legacy := legacyTuskerConfigPath(v7RepoRoot(vaultPath))
	if fileExists(legacy) {
		return legacy
	}
	return managed
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
	Harness          string                         `yaml:"harness" json:"harness"`
	Model            string                         `yaml:"model" json:"model"`
	Effort           string                         `yaml:"effort" json:"effort"`
	PermissionPreset string                         `yaml:"permission_preset,omitempty" json:"permission_preset,omitempty"`
	Command          string                         `yaml:"command,omitempty" json:"command,omitempty"`
	Sandbox          RunnerSandboxDefinition        `yaml:"sandbox" json:"sandbox"`
	Subagents        RunnerSubagentPolicyDefinition `yaml:"subagents" json:"subagents"`
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
			Harness:          string(RunnerCodexACP),
			Model:            "gpt-5.x",
			Effort:           "medium",
			PermissionPreset: "workspace-write-offline",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: boolPtr(false)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(true), MaxConcurrent: 2},
		},
		"execute-cheap": {
			Harness:          string(RunnerCodexACP),
			Model:            "gpt-5.x",
			Effort:           "low",
			PermissionPreset: "workspace-write-network",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: boolPtr(true)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(false), MaxConcurrent: 0},
		},
		"review-frontier": {
			Harness:          string(RunnerClaude),
			Model:            "claude-opus-4-8",
			Effort:           "high",
			PermissionPreset: "read-only",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "read-only", Network: boolPtr(false)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(false), MaxConcurrent: 0},
		},
		"unrestricted-high": {
			// Direct Codex remains an explicit emergency profile. It is never an
			// automatic retry or fallback after an ACP prompt may have been sent.
			Harness:          string(RunnerCodexExec),
			Model:            "gpt-5.x",
			Effort:           "high",
			PermissionPreset: "danger-full-access",
			Sandbox:          v7schema.TuskerRunnerSandboxConfig{Mode: "danger-full-access", Network: boolPtr(true)},
			Subagents:        v7schema.TuskerRunnerSubagentPolicyConfig{Allowed: boolPtr(true), MaxConcurrent: 2},
		},
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
			// Old root-level files remain lower-precedence compatibility inputs.
			tuskerConfigLayer{Name: configSourceProject, Path: legacyTuskerConfigPath(repoRoot)},
			tuskerConfigLayer{Name: configSourceProject, Path: managedTuskerConfigPath(vaultPath)},
			tuskerConfigLayer{Name: configSourceLocal, Path: legacyTuskerLocalConfigPath(repoRoot)},
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
	// A project may explicitly clear the inherited profile map while relying
	// on machine-local reconciliation to repopulate it.  In that transitional
	// state an inherited default_profile would be a dangling reference and
	// should not make the config unreadable before the reconciler can repair it.
	// Preserve an explicitly configured default_profile so validation still
	// rejects a genuine user typo.
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
			if _, explicitDefault := automation["default_profile"]; !explicitDefault {
				if effectiveAutomation := mapAny(effectiveRaw["automation"]); effectiveAutomation != nil {
					delete(effectiveAutomation, "default_profile")
				}
			}
		}
		break
	}
	effective, err := decodeTuskerConfigRaw(effectiveRaw, "effective configuration")
	if err != nil {
		return resolvedTuskerConfig{}, err
	}
	if err := validateResolvedTuskerConfig(effective, layers); err != nil {
		return resolvedTuskerConfig{}, err
	}
	return resolvedTuskerConfig{Config: effective, Layers: layers, Raw: effectiveRaw}, nil
}

func builtInTuskerConfigRaw() map[string]any {
	cfg := builtInTuskerConfig()
	return map[string]any{
		"tier": cfg.Tier,
		"automation": map[string]any{
			"default_profile": "default",
			"profiles":        configRawMap(cfg)["automation"].(map[string]any)["profiles"],
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

func mergeTuskerAutomationConfig(dst *v7TuskerConfigFile, src v7TuskerConfigFile) {
	if src.Automation.Enabled != nil {
		dst.Automation.Enabled = src.Automation.Enabled
	}
	if strings.TrimSpace(src.Automation.DispatchScope) != "" {
		dst.Automation.DispatchScope = strings.TrimSpace(src.Automation.DispatchScope)
	}
	if strings.TrimSpace(src.Automation.CompletionReactor.Mode) != "" {
		dst.Automation.CompletionReactor.Mode = strings.TrimSpace(src.Automation.CompletionReactor.Mode)
	}
	if len(src.Automation.TriggerStates) > 0 {
		dst.Automation.TriggerStates = append([]string{}, src.Automation.TriggerStates...)
	}
	if strings.TrimSpace(src.Automation.LegacyProfile) != "" {
		dst.Automation.LegacyProfile = src.Automation.LegacyProfile
	}
	if strings.TrimSpace(src.Automation.DefaultRunner) != "" {
		dst.Automation.DefaultRunner = src.Automation.DefaultRunner
	}
	if len(src.Automation.EnabledRunners) > 0 {
		dst.Automation.EnabledRunners = append([]string{}, src.Automation.EnabledRunners...)
	}
	if strings.TrimSpace(src.Automation.DefaultProfile) != "" {
		dst.Automation.DefaultProfile = src.Automation.DefaultProfile
	}
	if len(src.Automation.LaneProfiles) > 0 {
		if dst.Automation.LaneProfiles == nil {
			dst.Automation.LaneProfiles = map[string]string{}
		}
		for lane, profile := range src.Automation.LaneProfiles {
			dst.Automation.LaneProfiles[strings.TrimSpace(lane)] = strings.TrimSpace(profile)
		}
	}
	if len(src.Automation.Profiles) > 0 {
		if dst.Automation.Profiles == nil {
			dst.Automation.Profiles = map[string]v7schema.TuskerRunnerProfileConfig{}
		}
		for name, profile := range src.Automation.Profiles {
			dst.Automation.Profiles[strings.TrimSpace(name)] = profile
		}
	}
	if len(src.Automation.Routing) > 0 {
		dst.Automation.Routing = append([]v7schema.TuskerAutomationRoutingRuleConfig{}, src.Automation.Routing...)
	}
	if len(src.Automation.Denylist) > 0 {
		dst.Automation.Denylist = append([]v7schema.TuskerAutomationDenyRuleConfig{}, src.Automation.Denylist...)
	}
	if strings.TrimSpace(src.Automation.Workspace.Root) != "" {
		dst.Automation.Workspace.Root = src.Automation.Workspace.Root
	}
	if strings.TrimSpace(src.Automation.Workspace.Strategy) != "" {
		dst.Automation.Workspace.Strategy = src.Automation.Workspace.Strategy
	}
	if src.Automation.Concurrency.MaxActiveRuns > 0 {
		dst.Automation.Concurrency.MaxActiveRuns = src.Automation.Concurrency.MaxActiveRuns
	}
	if src.Automation.Concurrency.MaxActiveRunsPerProject > 0 {
		dst.Automation.Concurrency.MaxActiveRunsPerProject = src.Automation.Concurrency.MaxActiveRunsPerProject
	}
	if src.Automation.Concurrency.MaxContinuationRetries > 0 {
		dst.Automation.Concurrency.MaxContinuationRetries = src.Automation.Concurrency.MaxContinuationRetries
	}
	if src.Automation.Concurrency.MaxConcurrentByState != nil {
		dst.Automation.Concurrency.MaxConcurrentByState = src.Automation.Concurrency.MaxConcurrentByState
	}
	if src.Automation.Budget.Enabled != nil {
		dst.Automation.Budget.Enabled = src.Automation.Budget.Enabled
	}
	if src.Automation.Budget.PerAttemptInputTokens > 0 {
		dst.Automation.Budget.PerAttemptInputTokens = src.Automation.Budget.PerAttemptInputTokens
	}
	if src.Automation.Budget.PerAttemptOutputTokens > 0 {
		dst.Automation.Budget.PerAttemptOutputTokens = src.Automation.Budget.PerAttemptOutputTokens
	}
	if src.Automation.Budget.PerTaskInputTokens > 0 {
		dst.Automation.Budget.PerTaskInputTokens = src.Automation.Budget.PerTaskInputTokens
	}
	if src.Automation.Budget.PerTaskOutputTokens > 0 {
		dst.Automation.Budget.PerTaskOutputTokens = src.Automation.Budget.PerTaskOutputTokens
	}
	if src.Automation.Budget.DailyInputTokens > 0 {
		dst.Automation.Budget.DailyInputTokens = src.Automation.Budget.DailyInputTokens
	}
	if src.Automation.Budget.DailyOutputTokens > 0 {
		dst.Automation.Budget.DailyOutputTokens = src.Automation.Budget.DailyOutputTokens
	}
	if src.Automation.ExternalLoop.MaxCycles > 0 {
		dst.Automation.ExternalLoop.MaxCycles = src.Automation.ExternalLoop.MaxCycles
	}
	if src.Automation.ExternalLoop.MaxRepairContinuations > 0 {
		dst.Automation.ExternalLoop.MaxRepairContinuations = src.Automation.ExternalLoop.MaxRepairContinuations
	}
	if src.Automation.ExternalLoop.MaxExternalThreads > 0 {
		dst.Automation.ExternalLoop.MaxExternalThreads = src.Automation.ExternalLoop.MaxExternalThreads
	}
	if src.Automation.ExternalLoop.WallClockTimeoutHours > 0 {
		dst.Automation.ExternalLoop.WallClockTimeoutHours = src.Automation.ExternalLoop.WallClockTimeoutHours
	}
	if len(src.Automation.Runners) > 0 {
		if dst.Automation.Runners == nil {
			dst.Automation.Runners = map[string]v7schema.TuskerAutomationRunnerConfig{}
		}
		for name, runner := range src.Automation.Runners {
			dst.Automation.Runners[strings.TrimSpace(name)] = runner
		}
	}
	if src.Automation.Fanout.Enabled {
		dst.Automation.Fanout.Enabled = true
		dst.Automation.Fanout.MaxChildren = src.Automation.Fanout.MaxChildren
		dst.Automation.Fanout.AllowedChildTypes = append([]string{}, src.Automation.Fanout.AllowedChildTypes...)
		dst.Automation.Fanout.MergeRule = src.Automation.Fanout.MergeRule
	}
}

func validateTuskerConfigLayer(layer tuskerConfigLayer) error {
	if !layer.Present {
		return nil
	}
	_, tierPresent := lookupConfigValue(appliedConfigRaw(layer), "tier")
	if (tierPresent || layer.Config.Tier != 0) && (layer.Config.Tier < 1 || layer.Config.Tier > 5) {
		return tuskerError(errorConfigInvalid, "tier must be between 1 and 5", withPath(layer.Path))
	}
	if err := validateCompletionReactorModeLayer(layer); err != nil {
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

func validateResolvedTuskerConfig(cfg v7TuskerConfigFile, layers []tuskerConfigLayer) error {
	profiles := runnerProfilesFromSchema(cfg.Automation.Profiles)
	for name, profile := range profiles {
		if err := validateRunnerProfileDefinition(strings.TrimSpace(name), profile, sourcePathForConfigKey(layers, "automation.profiles."+strings.TrimSpace(name))); err != nil {
			return err
		}
	}
	if strings.TrimSpace(cfg.Automation.DefaultProfile) != "" {
		if _, ok := profiles[strings.TrimSpace(cfg.Automation.DefaultProfile)]; !ok {
			path := sourcePathForConfigKey(layers, "automation.default_profile")
			return tuskerError(errorConfigInvalid, "automation.default_profile references unknown profile "+cfg.Automation.DefaultProfile, withPath(path))
		}
	}
	for lane, profile := range cfg.Automation.LaneProfiles {
		if _, ok := profiles[strings.TrimSpace(profile)]; !ok {
			path := sourcePathForConfigKey(layers, "automation.lane_profiles."+strings.TrimSpace(lane))
			return tuskerError(errorConfigInvalid, "automation.lane_profiles."+lane+" references unknown profile "+profile, withPath(path))
		}
	}
	for _, rule := range cfg.Automation.Routing {
		if strings.TrimSpace(rule.Profile) == "" {
			path := sourcePathForConfigKey(layers, "automation.routing")
			return tuskerError(errorConfigInvalid, "automation.routing rule "+rule.Name+" is missing profile", withPath(path))
		}
		if _, ok := profiles[strings.TrimSpace(rule.Profile)]; !ok {
			path := sourcePathForConfigKey(layers, "automation.routing")
			return tuskerError(errorConfigInvalid, "automation.routing rule "+rule.Name+" references unknown profile "+rule.Profile, withPath(path))
		}
	}
	return nil
}

func validateRunnerProfileDefinition(name string, profile RunnerProfileDefinition, path string) error {
	if strings.TrimSpace(name) == "" {
		return tuskerError(errorConfigInvalid, "automation.profiles contains an empty profile name", withPath(path))
	}
	harness := RunnerName(strings.TrimSpace(profile.Harness))
	switch harness {
	case RunnerCodexAppServer:
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.harness uses retired value %q", name, profile.Harness), withPath(path), withHint("run `tusker acp setup` and migrate the profile harness to codex_acp; keep codex_exec only in an explicit emergency/danger profile"))
	case RunnerCodex, RunnerCodexExec, RunnerCodexCloud, RunnerClaude, RunnerCodexACP:
	default:
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.harness has unsupported value %q", name, profile.Harness), withPath(path), withHint("use codex_acp after `tusker acp setup`, or explicitly select codex_exec, codex_cloud, or claude-code"))
	}
	if !validRunnerModelName(profile.Model) {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.model has unsupported value %q", name, profile.Model), withPath(path), withHint("use a known model family such as gpt-5.x, claude-opus-4-8, claude-fable-5, sonnet-4.6, or glm-5.2"))
	}
	if !validRunnerEffort(profile.Effort) || (harness == RunnerClaude && strings.EqualFold(strings.TrimSpace(profile.Effort), "ultra")) {
		allowed := "low, medium, high, xhigh, max, ultra"
		if harness == RunnerClaude {
			allowed = "low, medium, high, xhigh, max"
		}
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.effort must be one of %s", name, allowed), withPath(path))
	}
	if !validRunnerSandboxMode(profile.Sandbox.Mode) {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.sandbox.mode must be one of read-only, workspace-write, danger-full-access", name), withPath(path))
	}
	if !validPermissionPreset(profile.PermissionPreset) {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.permission_preset has unsupported value %q", name, profile.PermissionPreset), withPath(path))
	}
	if profile.Subagents.MaxConcurrent < 0 {
		return tuskerError(errorConfigInvalid, fmt.Sprintf("automation.profiles.%s.subagents.max_concurrent must be >= 0", name), withPath(path))
	}
	return nil
}

func validRunnerModelName(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" || strings.ContainsAny(model, " \t\r\n") {
		return false
	}
	if model == "fable" || model == "opus" || model == "sonnet" {
		return true
	}
	for _, prefix := range []string{"gpt-", "claude-", "opus-", "sonnet-", "fable-", "glm-", "o3", "o4"} {
		if strings.HasPrefix(model, prefix) {
			return true
		}
	}
	return false
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
	for _, field := range []string{
		"harness",
		"model",
		"effort",
		"permission_preset",
		"sandbox.mode",
		"sandbox.network",
		"subagents.allowed",
		"subagents.max_concurrent",
	} {
		if _, present := lookupConfigValue(raw, base+field); !present {
			return false
		}
	}
	return true
}

func runnerProfileFromSchema(profile v7schema.TuskerRunnerProfileConfig) RunnerProfileDefinition {
	return RunnerProfileDefinition{
		Harness:          strings.TrimSpace(profile.Harness),
		Model:            strings.TrimSpace(profile.Model),
		Effort:           strings.TrimSpace(profile.Effort),
		PermissionPreset: strings.TrimSpace(profile.PermissionPreset),
		Command:          strings.TrimSpace(profile.Command),
		Sandbox: RunnerSandboxDefinition{
			Mode:    strings.TrimSpace(profile.Sandbox.Mode),
			Network: profile.Sandbox.Network,
		},
		Subagents: RunnerSubagentPolicyDefinition{
			Allowed:       profile.Subagents.Allowed,
			MaxConcurrent: profile.Subagents.MaxConcurrent,
		},
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
		return ResolvedRunnerProfile{Name: name, Source: source, Reason: reason, RuleName: ruleName, Definition: profile}, true, nil
	}
	if selected, ok, err := pick(stringField(note.Data, "runner_profile"), "task frontmatter", "runner_profile", ""); ok || err != nil {
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
	if role := semanticRunnerRole(stringField(note.Data, "complexity"), lane); role != "" {
		if selected, ok, err := pick(role, "task complexity", "semantic complexity role", ""); ok || err != nil {
			return selected, err
		}
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
			return ResolvedRunnerProfile{
				Reason: "legacy runner fallback",
				Definition: RunnerProfileDefinition{
					Harness: legacyRunner,
				},
			}, nil
		}
	}
	return selected, nil
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
	switch RunnerName(strings.TrimSpace(profile.Harness)) {
	case RunnerCodex, RunnerCodexAppServer, RunnerCodexExec, RunnerCodexACP:
	default:
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
		if strings.TrimSpace(profile.Sandbox.Mode) == "" {
			policy.ThreadSandbox = "workspace-write"
			policy.TurnSandboxPolicy = "workspace-write"
		}
		policy.TurnSandboxNetwork = boolPtr(true)
	case "workspace-write-offline":
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
	canonical := canonicalConfigLookupKey(key)
	if _, allowed := userGlobalConfigAllowlist[canonical]; !allowed {
		return configResolveReport{}, tuskerError(errorConfigInvalid, "user-global config does not allow behavioral key "+key)
	}
	before, err := configResolveForRepo("", false, key)
	if err != nil {
		return configResolveReport{}, err
	}
	path := userGlobalTuskerConfigPath()
	if err := writeConfigValue(path, key, value); err != nil {
		return configResolveReport{}, err
	}
	after, err := configResolveForRepo("", false, key)
	if err != nil {
		return configResolveReport{}, err
	}
	if !configValueChanged(before.Value, after.Value) {
		return after, tuskerError(errorConfigInvalid, "config setter no-op: effective value for "+key+" is unchanged", withPath(path), withContext(map[string]any{"key": key, "value": after.Value}))
	}
	if after.Source != configSourceUserGlobal {
		return after, tuskerError(errorConfigInvalid, "config setter failed trigger-eval: user-global config did not win for "+key, withPath(path), withContext(map[string]any{"key": key, "winner": after.Source, "value": after.Value}))
	}
	return after, nil
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

func sortedProfileNames(profiles map[string]RunnerProfileDefinition) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
