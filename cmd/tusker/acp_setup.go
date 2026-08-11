package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const acpSetupReportSchema = "tusker.acp-setup-report/v1"

var acpSetupRuntimeProbe = probePackagedCodexACP

type ACPSetupRequest struct {
	StateRoot     string
	VaultPath     string
	NPMPrefix     string
	NodePath      string
	AuthSource    CodexACPAuthSource
	AuthPrincipal string
}

type ACPSetupReport struct {
	Schema              string                      `json:"schema"`
	Runner              string                      `json:"runner"`
	Primary             bool                        `json:"primary"`
	ConfigPath          string                      `json:"config_path"`
	AuthSource          string                      `json:"auth_source"`
	AuthPrincipalSHA256 string                      `json:"auth_principal_sha256"`
	Package             ACPAdapterNPMPackageReceipt `json:"package"`
}

// SetupCodexACP packages an already-installed exact npm closure and writes the
// machine-local project admission that makes codex_acp the primary runner. It
// performs no npm, network, login, provider prompt, or daemon operation.
func SetupCodexACP(request ACPSetupRequest) (ACPSetupReport, error) {
	vault, err := canonicalACPSetupVault(request.VaultPath)
	if err != nil {
		return ACPSetupReport{}, err
	}
	authSource := request.AuthSource
	if authSource == "" {
		authSource = CodexACPAuthChatGPTSession
	}
	authIdentity, err := resolveACPSetupAuthIdentity(authSource, request.AuthPrincipal, os.Environ())
	if err != nil {
		return ACPSetupReport{}, err
	}
	nodePath := strings.TrimSpace(request.NodePath)
	if nodePath == "" {
		nodePath, err = exec.LookPath("node")
		if err != nil {
			return ACPSetupReport{}, fmt.Errorf("ACP setup requires a local Node executable: %w", err)
		}
		if nodePath, err = filepath.Abs(nodePath); err != nil {
			return ACPSetupReport{}, err
		}
	}
	packaged, err := PackageACPAdapterNPM(ACPAdapterNPMPackageRequest{
		StateRoot: request.StateRoot,
		Prefix:    request.NPMPrefix,
		NodePath:  nodePath,
	})
	if err != nil {
		return ACPSetupReport{}, err
	}
	if err := acpSetupRuntimeProbe(packaged); err != nil {
		return ACPSetupReport{}, fmt.Errorf("verify sealed Codex ACP runtime: %w", err)
	}
	principalDigest := acpAdapterBundleDigest([]byte("tusker.codex-acp-auth-principal/v1\x00" + string(authSource) + "\x00" + authIdentity))
	configPath, err := configurePrimaryCodexACP(vault, packaged, authSource, principalDigest)
	if err != nil {
		return ACPSetupReport{}, err
	}
	return ACPSetupReport{
		Schema: acpSetupReportSchema, Runner: string(RunnerCodexACP), Primary: true,
		ConfigPath: configPath, AuthSource: string(authSource), AuthPrincipalSHA256: principalDigest,
		Package: packaged,
	}, nil
}

type acpSetupProbeBuffer struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (buffer *acpSetupProbeBuffer) Write(p []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	const limit = 8 << 10
	remaining := limit - len(buffer.data)
	if remaining > 0 {
		copyLength := len(p)
		if copyLength > remaining {
			copyLength = remaining
		}
		buffer.data = append(buffer.data, p[:copyLength]...)
	}
	if len(p) > remaining {
		buffer.overflow = true
	}
	return len(p), nil
}

func probePackagedCodexACP(packaged ACPAdapterNPMPackageReceipt) error {
	if len(packaged.Bundle.Argv) != 2 || packaged.Bundle.LaunchKind != ACPAdapterBundleLaunchInterpreter {
		return fmt.Errorf("sealed npm bundle has no exact interpreter entrypoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, packaged.Bundle.Argv[0], packaged.Bundle.Argv[1], "--version")
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	var output acpSetupProbeBuffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("sealed adapter version probe timed out")
	}
	if err != nil {
		return fmt.Errorf("sealed adapter version probe failed: %w: %s", err, strings.TrimSpace(string(output.data)))
	}
	if output.overflow {
		return fmt.Errorf("sealed adapter version probe exceeded output limit")
	}
	expected := "@agentclientprotocol/codex-acp " + packaged.AdapterVersion
	if strings.TrimSpace(string(output.data)) != expected {
		return fmt.Errorf("sealed adapter version = %q, want %q", strings.TrimSpace(string(output.data)), expected)
	}
	return nil
}

func canonicalACPSetupVault(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("ACP setup requires an absolute Tusker vault path")
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil || !isVaultDir(physical) {
		return "", fmt.Errorf("ACP setup requires an existing canonical Tusker vault")
	}
	return filepath.Clean(physical), nil
}

func resolveACPSetupAuthIdentity(source CodexACPAuthSource, principal string, inherited []string) (string, error) {
	principal = strings.TrimSpace(principal)
	switch source {
	case CodexACPAuthChatGPTSession:
		value, err := exactCodexACPEnvironmentValue(inherited, "CODEX_HOME")
		if err != nil {
			value = filepath.Join(userHomeDir(), ".codex")
		}
		resolved, err := filepath.EvalSymlinks(value)
		info, statErr := os.Stat(resolved)
		if err != nil || statErr != nil || !filepath.IsAbs(resolved) || !info.IsDir() {
			return "", fmt.Errorf("ChatGPT session setup requires CODEX_HOME or an existing ~/.codex")
		}
		return filepath.Clean(resolved), nil
	case CodexACPAuthCodexAPIKey, CodexACPAuthOpenAIAPIKey:
		key, _ := (CodexACPAuthContract{Source: source}).environmentKey()
		if _, err := exactCodexACPEnvironmentValue(inherited, key); err != nil {
			return "", err
		}
		if principal == "" || len(principal) > 256 || containsControl(principal) {
			return "", fmt.Errorf("API-key ACP setup requires a short non-secret --auth-principal label")
		}
		return principal, nil
	default:
		return "", fmt.Errorf("unsupported Codex ACP auth source %q", source)
	}
}

func configurePrimaryCodexACP(vault string, packaged ACPAdapterNPMPackageReceipt, authSource CodexACPAuthSource, principalDigest string) (string, error) {
	if packaged.Schema != acpAdapterNPMReceiptSchema || packaged.Bundle.LaunchKind != ACPAdapterBundleLaunchInterpreter || !v7CloseAuthorityDigest(principalDigest, "sha256:") {
		return "", fmt.Errorf("ACP setup requires a complete verified npm bundle receipt")
	}
	resolved, err := resolveTuskerConfig(vault)
	if err != nil {
		return "", err
	}
	path := managedTuskerLocalConfigPath(vault)
	previous, existed, err := readConfigText(path)
	if err != nil {
		return "", err
	}
	raw := map[string]any{}
	if existed && strings.TrimSpace(previous) != "" {
		if err := yaml.Unmarshal([]byte(previous), &raw); err != nil {
			return "", fmt.Errorf("parse machine-local config before ACP setup: %w", err)
		}
	}
	setNestedConfigValue(raw, "automation.default_runner", string(RunnerCodexACP))
	setNestedConfigValue(raw, "automation.enabled_runners", primaryACPEnabledRunners(resolved.Config.Automation.EnabledRunners))
	setNestedConfigValue(raw, "automation.runners."+string(RunnerCodexACP), map[string]any{
		"kind":                  string(RunnerCodexACP),
		"bundle_root":           packaged.Bundle.BundleRoot,
		"manifest_path":         packaged.Bundle.ManifestPath,
		"manifest_sha256":       packaged.ManifestSHA256,
		"adapter_version":       packaged.AdapterVersion,
		"adapter_launch_kind":   string(ACPAdapterBundleLaunchInterpreter),
		"auth_source":           string(authSource),
		"auth_principal_sha256": principalDigest,
	})
	profiles, defaultProfile := primaryACPProfileOverrides(resolved)
	setNestedConfigValue(raw, "automation.profiles", profiles)
	setNestedConfigValue(raw, "automation.default_profile", defaultProfile)
	encoded, err := yaml.Marshal(raw)
	if err != nil {
		return "", err
	}
	repoRoot := v7RepoRoot(vault)
	if _, err := resolveTuskerConfigForPathsWithOverrides(repoRoot, vault, true, map[string]map[string]any{path: raw}); err != nil {
		return "", err
	}
	if err := writeConfigTextAtomically(path, string(encoded)); err != nil {
		return "", err
	}
	rollback := func(cause error) (string, error) {
		if restoreErr := restoreConfigText(path, previous, existed); restoreErr != nil {
			return "", fmt.Errorf("%v; rollback machine-local config: %w", cause, restoreErr)
		}
		return "", cause
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		return rollback(err)
	}
	if wfFile.Data.Agents.Default != string(RunnerCodexACP) {
		return rollback(fmt.Errorf("ACP setup did not become the effective primary runner"))
	}
	if _, _, err := runnerForName(string(RunnerCodexACP), wfFile.Data); err != nil {
		return rollback(fmt.Errorf("ACP setup runner admission failed: %w", err))
	}
	return path, nil
}

func primaryACPEnabledRunners(existing []string) []string {
	seen := map[string]bool{string(RunnerCodexACP): true}
	out := []string{string(RunnerCodexACP)}
	for _, name := range existing {
		name = strings.TrimSpace(name)
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func primaryACPProfileOverrides(resolved resolvedTuskerConfig) (map[string]any, string) {
	overrides := map[string]any{}
	names := make([]string, 0, len(resolved.Config.Automation.Profiles))
	for name := range resolved.Config.Automation.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		profile := resolved.Config.Automation.Profiles[name]
		harness := RunnerName(strings.TrimSpace(profile.Harness))
		if harness == RunnerCodexExec && acpSetupProfileEligible(runnerProfileFromSchema(profile)) {
			profile.Harness = string(RunnerCodexACP)
		}
		overrides[name] = runnerProfileConfigMap(profile)
	}
	defaultProfile := strings.TrimSpace(resolved.Config.Automation.DefaultProfile)
	defaultDefinition, defaultExists := resolved.Config.Automation.Profiles[defaultProfile]
	defaultHarness := RunnerName(strings.TrimSpace(defaultDefinition.Harness))
	defaultEligible := defaultExists && (defaultHarness == RunnerCodexExec || defaultHarness == RunnerCodexACP) && acpSetupProfileEligible(runnerProfileFromSchema(defaultDefinition))
	if !defaultEligible {
		defaultProfile = "acp-primary"
		overrides[defaultProfile] = map[string]any{
			"harness": string(RunnerCodexACP), "model": "gpt-5.6-terra", "effort": "medium",
			"permission_preset": "workspace-write-offline",
			"sandbox":           map[string]any{"mode": "workspace-write", "network": false},
			"subagents":         map[string]any{"allowed": false, "max_concurrent": 0},
		}
	}
	return overrides, defaultProfile
}

func acpSetupProfileEligible(profile RunnerProfileDefinition) bool {
	switch strings.TrimSpace(profile.PermissionPreset) {
	case "read-only":
		return strings.TrimSpace(profile.Sandbox.Mode) == "read-only" && (profile.Sandbox.Network == nil || !*profile.Sandbox.Network)
	case "workspace-write-offline":
		return strings.TrimSpace(profile.Sandbox.Mode) == "workspace-write" && profile.Sandbox.Network != nil && !*profile.Sandbox.Network
	case "workspace-write-network":
		return strings.TrimSpace(profile.Sandbox.Mode) == "workspace-write" && profile.Sandbox.Network != nil && *profile.Sandbox.Network
	default:
		return false
	}
}

func runnerProfileConfigMap(profile interface {
}) map[string]any {
	// Marshal through YAML so field-presence and pointer booleans match the
	// canonical config decoder instead of maintaining a second struct mapping.
	raw, _ := yaml.Marshal(profile)
	out := map[string]any{}
	_ = yaml.Unmarshal(raw, &out)
	return out
}

func acpSetupCommand(args Args) error {
	if err := validateACPAdapterCommandArgs(args, "json", "npm-prefix", "node", "vault", "auth-source", "auth-principal"); err != nil {
		return tuskerError(errorInvalidArg, err.Error())
	}
	if !args.Bool("json") {
		return tuskerError(errorInvalidArg, "acp setup requires --json")
	}
	vault, err := resolveVaultPath(args, false)
	if err != nil {
		return err
	}
	report, err := SetupCodexACP(ACPSetupRequest{
		StateRoot: DefaultStateRoot(), VaultPath: vault, NPMPrefix: args.String("npm-prefix"), NodePath: args.String("node"),
		AuthSource: CodexACPAuthSource(args.String("auth-source")), AuthPrincipal: args.String("auth-principal"),
	})
	if err != nil {
		return tuskerError(errorConfigInvalid, err.Error())
	}
	emitJSON(map[string]any{"ok": true, "setup": compactACPSetupReport(report)})
	return nil
}

func compactACPSetupReport(report ACPSetupReport) map[string]any {
	return map[string]any{
		"schema": report.Schema, "runner": report.Runner, "primary": report.Primary,
		"config_path": report.ConfigPath, "auth_source": report.AuthSource,
		"auth_principal_sha256": report.AuthPrincipalSHA256,
		"bundle_digest":         report.Package.BundleDigest, "bundle_root": report.Package.Bundle.BundleRoot,
		"manifest_sha256":         report.Package.ManifestSHA256,
		"verified_content_digest": report.Package.Bundle.VerifiedContentDigest,
		"adapter_version":         report.Package.AdapterVersion, "codex_version": report.Package.CodexVersion,
		"platform_package": report.Package.PlatformPackage, "platform_version": report.Package.PlatformVersion,
		"asset_count": len(report.Package.Bundle.Assets),
	}
}
