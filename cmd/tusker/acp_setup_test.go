package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"tusker/internal/v7schema"
)

func TestPrimaryACPProfileOverridesPreservesNonCodexAndDangerProfiles(t *testing.T) {
	dangerNetwork := true
	profiles := map[string]v7schema.TuskerRunnerProfileConfig{
		"codex-safe": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "high", PermissionPreset: "read-only",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "read-only", Network: &dangerNetwork},
		},
		"codex-safe-offline": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "high", PermissionPreset: "read-only",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "read-only"},
		},
		"codex-acp": {
			Harness: string(RunnerCodexACP), Model: "gpt-5.x", Effort: "medium", PermissionPreset: "read-only",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "read-only"},
		},
		"claude-review": {
			Harness: string(RunnerClaude), Model: "claude-opus-4-8", Effort: "high", PermissionPreset: "read-only",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "read-only"},
		},
		"emergency": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "high", PermissionPreset: "danger-full-access",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "danger-full-access", Network: &dangerNetwork},
		},
		"codex-empty": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "medium",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: &dangerNetwork},
		},
		"codex-inconsistent": {
			Harness: string(RunnerCodexExec), Model: "gpt-5.x", Effort: "medium", PermissionPreset: "workspace-write-offline",
			Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write"},
		},
	}
	resolved := resolvedTuskerConfig{Config: v7schema.TuskerConfigFile{Automation: v7schema.TuskerAutomationConfig{
		DefaultProfile: "claude-review", Profiles: profiles,
	}}}

	overrides, defaultProfile := primaryACPProfileOverrides(resolved)
	if defaultProfile != "acp-primary" {
		t.Fatalf("non-Codex default profile was retained: %q", defaultProfile)
	}
	for name, wantHarness := range map[string]string{
		"codex-safe-offline": string(RunnerCodexACP),
	} {
		profile, ok := overrides[name].(map[string]any)
		if !ok || profile["harness"] != wantHarness {
			t.Fatalf("profile %q was not preserved/migrated correctly: %#v", name, overrides[name])
		}
	}
	for _, name := range []string{"codex-safe", "codex-acp", "claude-review", "emergency", "codex-empty", "codex-inconsistent"} {
		if _, ok := overrides[name]; ok {
			t.Fatalf("unchanged profile %q was snapshotted into local overrides: %#v", name, overrides[name])
		}
	}
	if _, ok := overrides["acp-primary"]; !ok {
		t.Fatalf("safe ACP fallback profile missing: %#v", overrides)
	}

	repeated, repeatedDefault := primaryACPProfileOverrides(resolved)
	if repeatedDefault != defaultProfile || !reflect.DeepEqual(repeated, overrides) {
		t.Fatalf("profile migration is not idempotent: first=%#v/%q second=%#v/%q", overrides, defaultProfile, repeated, repeatedDefault)
	}
}

func TestPrimaryACPProfileOverridesKeepsProjectPrimarySparse(t *testing.T) {
	network := false
	resolved := resolvedTuskerConfig{Config: v7schema.TuskerConfigFile{Automation: v7schema.TuskerAutomationConfig{
		DefaultProfile: "claude-review",
		Profiles: map[string]v7schema.TuskerRunnerProfileConfig{
			"claude-review": {Harness: string(RunnerClaude), Model: "claude-opus-4-8", PermissionPreset: "read-only", Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "read-only"}},
			"acp-primary":   {Harness: string(RunnerCodexExec), Model: "project-model", Effort: "high", PermissionPreset: "workspace-write-offline", Sandbox: v7schema.TuskerRunnerSandboxConfig{Mode: "workspace-write", Network: &network}},
		},
	}}}

	overrides, defaultProfile := primaryACPProfileOverrides(resolved)
	if defaultProfile != "acp-primary" {
		t.Fatalf("default profile = %q", defaultProfile)
	}
	profile := mapAny(overrides["acp-primary"])
	if len(profile) != 1 || profile["harness"] != string(RunnerCodexACP) {
		t.Fatalf("project-owned acp-primary was snapshotted: %#v", profile)
	}
}

func TestSetupCodexACPPackagesAndMakesMachineLocalPrimary(t *testing.T) {
	previousProbe := acpSetupRuntimeProbe
	acpSetupRuntimeProbe = func(ACPAdapterNPMPackageReceipt) error { return nil }
	t.Cleanup(func() { acpSetupRuntimeProbe = previousProbe })
	vault := automationTestVault(t)
	prefix, _ := newACPAdapterNPMFixture(t)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	report, err := SetupCodexACP(ACPSetupRequest{
		StateRoot: DefaultStateRoot(), VaultPath: vault, NPMPrefix: prefix,
		AuthSource: CodexACPAuthChatGPTSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(report.Package.Bundle.BundleRoot) })
	if !report.Primary || report.Runner != string(RunnerCodexACP) || report.Package.Bundle.LaunchKind != ACPAdapterBundleLaunchInterpreter {
		t.Fatalf("setup report is not primary interpreter ACP: %#v", report)
	}
	physicalVault, err := filepath.EvalSymlinks(vault)
	if err != nil {
		t.Fatal(err)
	}
	if report.ConfigPath != managedTuskerLocalConfigPath(physicalVault) || !v7CloseAuthorityDigest(report.AuthPrincipalSHA256, "sha256:") {
		t.Fatalf("setup machine-local identity is incomplete: %#v", report)
	}
	wfFile, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if wfFile.Data.Agents.Default != string(RunnerCodexACP) || !containsString(wfFile.Data.Agents.Enabled, string(RunnerCodexACP)) {
		t.Fatalf("ACP did not become the effective primary runner: %#v", wfFile.Data.Agents)
	}
	definition := wfFile.Data.Runners[string(RunnerCodexACP)]
	if definition.BundleRoot != report.Package.Bundle.BundleRoot || definition.AdapterLaunchKind != string(ACPAdapterBundleLaunchInterpreter) || definition.AuthPrincipalSHA256 != report.AuthPrincipalSHA256 {
		t.Fatalf("machine-local runner definition drift: %#v", definition)
	}
	profile, err := resolveRunProfileForLane(Note{}, wfFile.Data, runLaneExecute, "")
	if err != nil {
		t.Fatal(err)
	}
	if RunnerName(profile.Definition.Harness) != RunnerCodexACP || profile.Definition.PermissionPreset != "workspace-write-offline" {
		t.Fatalf("default execution profile did not cut over to ACP: %#v", profile)
	}
	second, err := SetupCodexACP(ACPSetupRequest{
		StateRoot: DefaultStateRoot(), VaultPath: vault, NPMPrefix: prefix,
		AuthSource: CodexACPAuthChatGPTSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Package.BundleDigest != report.Package.BundleDigest {
		t.Fatalf("idempotent setup changed bundle: first=%s second=%s", report.Package.BundleDigest, second.Package.BundleDigest)
	}
	wfFile, err = loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got := RunnerName(wfFile.Data.RunnerProfiles["review-independent"].Harness); got != RunnerCodexACP {
		t.Fatalf("idempotent setup dropped converted review-independent profile: %s", got)
	}
}

func TestACPSetupKeepsProjectProfileFieldsLive(t *testing.T) {
	previousProbe := acpSetupRuntimeProbe
	acpSetupRuntimeProbe = func(ACPAdapterNPMPackageReceipt) error { return nil }
	t.Cleanup(func() { acpSetupRuntimeProbe = previousProbe })
	vault := automationTestVault(t)
	if err := os.Remove(managedTuskerConfigPath(vault)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	projectConfig := `schema: tusker.config/v1
project_id: app
automation:
  default_profile: execute-standard
  profiles:
    execute-standard:
      harness: codex_exec
      model: gpt-5.6-terra
      effort: medium
      permission_preset: workspace-write-offline
      sandbox: {mode: workspace-write, network: false}
      subagents: {allowed: false, max_concurrent: 0}
`
	projectPath := filepath.Join(filepath.Dir(vault), "tusker.yaml")
	if err := writeText(projectPath, projectConfig); err != nil {
		t.Fatal(err)
	}
	prefix, _ := newACPAdapterNPMFixture(t)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	report, err := SetupCodexACP(ACPSetupRequest{StateRoot: DefaultStateRoot(), VaultPath: vault, NPMPrefix: prefix, AuthSource: CodexACPAuthChatGPTSession})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(report.Package.Bundle.BundleRoot) })
	localText, err := readText(report.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	var local map[string]any
	if err := yaml.Unmarshal([]byte(localText), &local); err != nil {
		t.Fatal(err)
	}
	profiles := mapAny(mapAny(local["automation"])["profiles"])
	profile := mapAny(profiles["execute-standard"])
	if len(profile) != 1 || profile["harness"] != string(RunnerCodexACP) {
		t.Fatalf("setup snapshotted project profile fields: %#v", profile)
	}
	projectConfig = strings.Replace(projectConfig, "model: gpt-5.6-terra", "model: gpt-5.6-sol", 1)
	if err := writeText(projectPath, projectConfig); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got := wf.Data.RunnerProfiles["execute-standard"].Model; got != "gpt-5.6-sol" {
		t.Fatalf("project profile change was masked by setup: model=%q", got)
	}
}

func TestACPSetupRebindsExistingLocalPrimaryAwayFromDirectCLI(t *testing.T) {
	previousProbe := acpSetupRuntimeProbe
	acpSetupRuntimeProbe = func(ACPAdapterNPMPackageReceipt) error { return nil }
	t.Cleanup(func() { acpSetupRuntimeProbe = previousProbe })
	vault := automationTestVault(t)
	localPath := managedTuskerLocalConfigPath(vault)
	localConfig := `schema: tusker.config/v1
automation:
  default_profile: acp-primary
  profiles:
    acp-primary:
      harness: codex_exec
      model: gpt-5.6-terra
      effort: medium
      permission_preset: workspace-write-offline
      sandbox: {mode: workspace-write, network: false}
      subagents: {allowed: false, max_concurrent: 0}
`
	if err := writeText(localPath, localConfig); err != nil {
		t.Fatal(err)
	}
	prefix, _ := newACPAdapterNPMFixture(t)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	report, err := SetupCodexACP(ACPSetupRequest{StateRoot: DefaultStateRoot(), VaultPath: vault, NPMPrefix: prefix, AuthSource: CodexACPAuthChatGPTSession})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = makeACPAdapterBundleWritable(report.Package.Bundle.BundleRoot) })
	wf, err := loadWorkflow(vault)
	if err != nil {
		t.Fatal(err)
	}
	if got := RunnerName(wf.Data.RunnerProfiles["acp-primary"].Harness); got != RunnerCodexACP {
		t.Fatalf("setup left machine-local primary on direct CLI: %s", got)
	}
}

func TestProbePackagedCodexACPRejectsUnlaunchableSealedRuntime(t *testing.T) {
	report := ACPAdapterNPMPackageReceipt{
		AdapterVersion: ACPAdapterNPMAdapterVersion,
		Bundle: ACPAdapterBundleVerificationReceipt{
			LaunchKind: ACPAdapterBundleLaunchInterpreter,
			Argv:       []string{filepath.Join(t.TempDir(), "missing-node"), filepath.Join(t.TempDir(), "index.js")},
		},
	}
	if err := probePackagedCodexACP(report); err == nil || !strings.Contains(err.Error(), "version probe failed") {
		t.Fatalf("unlaunchable sealed runtime error = %v", err)
	}
}

func TestCompactACPSetupReportOmitsFullAssetManifest(t *testing.T) {
	report := ACPSetupReport{Schema: acpSetupReportSchema, Runner: string(RunnerCodexACP), Primary: true}
	report.Package.Bundle.Assets = []ACPAdapterBundleAsset{{Path: "secretly-noisy", SHA256: "sha256:" + strings.Repeat("a", 64)}}
	encoded, err := json.Marshal(compactACPSetupReport(report))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secretly-noisy") || strings.Contains(string(encoded), "assets") {
		t.Fatalf("compact setup report leaked full manifest: %s", encoded)
	}
}

func TestResolveACPSetupAuthIdentityNeverNeedsCredentialPersistence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	physicalHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveACPSetupAuthIdentity(CodexACPAuthChatGPTSession, "", []string{"CODEX_HOME=" + home}); err != nil || got != physicalHome {
		t.Fatalf("chatgpt identity = %q err=%v", got, err)
	}
	if got, err := resolveACPSetupAuthIdentity(CodexACPAuthOpenAIAPIKey, "account-label", []string{"OPENAI_API_KEY=secret-value"}); err != nil || got != "account-label" {
		t.Fatalf("API identity = %q err=%v", got, err)
	}
	if _, err := resolveACPSetupAuthIdentity(CodexACPAuthOpenAIAPIKey, "", []string{"OPENAI_API_KEY=secret-value"}); err == nil {
		t.Fatal("API-key setup accepted a missing non-secret principal label")
	}
}
