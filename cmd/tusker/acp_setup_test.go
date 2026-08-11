package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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
		"codex-safe":         string(RunnerCodexExec),
		"codex-safe-offline": string(RunnerCodexACP),
		"codex-acp":          string(RunnerCodexACP),
		"claude-review":      string(RunnerClaude),
		"emergency":          string(RunnerCodexExec),
		"codex-empty":        string(RunnerCodexExec),
		"codex-inconsistent": string(RunnerCodexExec),
	} {
		profile, ok := overrides[name].(map[string]any)
		if !ok || profile["harness"] != wantHarness {
			t.Fatalf("profile %q was not preserved/migrated correctly: %#v", name, overrides[name])
		}
	}
	emergency := overrides["emergency"].(map[string]any)
	if emergency["permission_preset"] != "danger-full-access" {
		t.Fatalf("danger profile authority changed: %#v", emergency)
	}
	if _, ok := overrides["acp-primary"]; !ok {
		t.Fatalf("safe ACP fallback profile missing: %#v", overrides)
	}

	repeated, repeatedDefault := primaryACPProfileOverrides(resolved)
	if repeatedDefault != defaultProfile || !reflect.DeepEqual(repeated, overrides) {
		t.Fatalf("profile migration is not idempotent: first=%#v/%q second=%#v/%q", overrides, defaultProfile, repeated, repeatedDefault)
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
