package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCodexACPFactoryRejectsUnverifiedBundleBeforeLease(t *testing.T) {
	wf := Workflow{Runners: map[string]RunnerDefinition{
		"codex-acp": {
			Kind:                string(RunnerCodexACP),
			BundleRoot:          "/opt/tusker/acp/codex",
			ManifestPath:        "manifest.json",
			ManifestSHA256:      "sha256:" + strings.Repeat("a", 64),
			AdapterVersion:      "1.1.14",
			AuthSource:          string(CodexACPAuthChatGPTSession),
			AuthPrincipalSHA256: "sha256:" + strings.Repeat("b", 64),
		},
	}}
	_, _, err := runnerForName("codex-acp", wf)
	if err == nil || !strings.Contains(err.Error(), "pre-claim bundle admission") {
		t.Fatalf("codex ACP factory must fail closed before a verified bundle exists, got %v", err)
	}
}

func TestCodexACPReadOnlyFactoryWrapperVertical(t *testing.T) {
	store, wrapper := setupRunnerWrapperRuntime(t)
	request, manifest := newACPAdapterBundleFixture(t)
	unsealACPAdapterBundle(t, request.BundleRoot)
	physicalRoot, err := filepath.EvalSymlinks(request.BundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(request.BundleRoot, "adapter.js")); err != nil {
		t.Fatal(err)
	}
	fakeBytes, err := os.ReadFile(fakeACPBinary(t))
	if err != nil {
		t.Fatal(err)
	}
	adapterPath := filepath.Join(physicalRoot, "bin", "codex-acp")
	if err := os.Remove(filepath.Join(request.BundleRoot, "bin", "node")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(adapterPath, fakeBytes, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(adapterPath, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest.Provider, manifest.Adapter, manifest.Version = codexACPProvider, "codex-acp", "1.1.14"
	manifest.Argv = []string{adapterPath}
	manifest.Assets = []ACPAdapterBundleAsset{
		{Path: "bin/codex-acp", SHA256: testACPAdapterBundleFileDigest(t, adapterPath), Role: "executable"},
		{Path: "lib/runtime.js", SHA256: testACPAdapterBundleFileDigest(t, filepath.Join(physicalRoot, "lib", "runtime.js")), Role: "asset"},
	}
	request.ExpectedDescriptor = ACPAdapterBundleDescriptorPolicy{Provider: codexACPProvider, Adapter: "codex-acp", Version: manifest.Version, LaunchKind: ACPAdapterBundleLaunchNative}
	writeACPAdapterBundleManifest(t, &request, manifest)
	sealACPAdapterBundle(t, request.BundleRoot)

	definition := RunnerDefinition{
		Kind: string(RunnerCodexACP), BundleRoot: request.BundleRoot, ManifestPath: request.ManifestPath,
		ManifestSHA256: request.ExpectedManifestSHA256, AdapterVersion: manifest.Version,
		AuthSource: string(CodexACPAuthOpenAIAPIKey), AuthPrincipalSHA256: codexACPTestPrincipalDigest("codex-acp-fixture"),
	}
	wf := Workflow{Runners: map[string]RunnerDefinition{"codex-acp": definition}}
	runner, command, err := runnerForName("codex-acp", wf)
	if err != nil {
		t.Fatal(err)
	}
	if command != "" || runner.Name() != RunnerCodexACP || runner.Capabilities().ResumeSession {
		t.Fatalf("factory did not return a fresh-only concrete Codex ACP runner: runner=%T command=%q caps=%#v", runner, command, runner.Capabilities())
	}
	codexRunner, ok := runner.(*CodexACPRunner)
	if !ok {
		t.Fatalf("factory returned %T, want concrete Codex ACP runner", runner)
	}
	plan, err := codexRunner.admission.withProfile(ResolvedRunnerProfile{Name: "codex-read-only", Definition: RunnerProfileDefinition{
		Harness: string(RunnerCodexACP), Model: "gpt-5.3-codex", Effort: "high", PermissionPreset: "read-only",
		Sandbox: RunnerSandboxDefinition{Mode: "read-only"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	registerCodexACPWrapperAdmission(t, store, wrapper, definition)
	descriptor, argv, err := plan.descriptorAndArgv()
	if err != nil {
		t.Fatal(err)
	}
	wrapper.Runner = string(RunnerCodexACP)
	wrapper.ContainmentPGID = processGroupID(os.Getpid())
	wrapper.Start.Command = argv[0]
	wrapper.Start.CommandArgv = argv
	wrapper.Start.CommandExecutableFP = descriptor.Adapter.Fingerprint
	wrapper.Start.RawLogMaxBytes = 64 * 1024
	wrapper.Start.RunnerProfile = "codex-read-only"
	wrapper.Start.RunnerHarness = string(RunnerCodexACP)
	wrapper.Start.RunnerModel = descriptor.Model
	wrapper.Start.RunnerEffort = descriptor.Effort
	wrapper.Start.CodexACP = &plan
	run, err := store.FindRun(wrapper.Start.RecordID)
	if err != nil || run == nil {
		t.Fatalf("find wrapper run: run=%#v err=%v", run, err)
	}
	run.Runner = string(RunnerCodexACP)
	run.RunnerProfile = wrapper.Start.RunnerProfile
	run.RunnerHarness = wrapper.Start.RunnerHarness
	run.RunnerModel = wrapper.Start.RunnerModel
	run.RunnerEffort = wrapper.Start.RunnerEffort
	if err := store.UpsertRun(*run); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRunAuthorization(RunAuthorization{
		ProjectID: wrapper.Start.ProjectID, RecordID: wrapper.Start.RecordID, LeaseGeneration: wrapper.Start.LeaseGeneration,
		Source: "test", Actor: "human:codex-acp-test", Trigger: "test", CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual factory Start boundary. The test wrapper exits
	// immediately; the assertion is the durable detached request and receipt,
	// not a second provider process.
	stubWrapper := filepath.Join(t.TempDir(), "codex-acp-detached-wrapper")
	if err := os.WriteFile(stubWrapper, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TUSKER_WRAPPER_EXE", stubWrapper)
	detachedResult, err := runner.Start(context.Background(), wrapper.Start)
	if err != nil {
		t.Fatalf("profile-bound Codex ACP factory start: %v", err)
	}
	if detachedResult.PID <= 0 || detachedResult.StatusPath != wrapper.Start.StatusPath || detachedResult.Capabilities.ResumeSession {
		t.Fatalf("unexpected detached Codex ACP factory receipt: %#v", detachedResult)
	}
	factoryHandoff, err := readRunnerWrapperRequest(wrapper.Start.StatusPath + ".wrapper-request.json")
	if err != nil {
		t.Fatalf("read detached Codex ACP factory handoff: %v", err)
	}
	if factoryHandoff.Runner != string(RunnerCodexACP) || factoryHandoff.Start.CodexACP == nil || factoryHandoff.Start.CodexACP.matchesAdmission(plan) != nil {
		t.Fatalf("factory handoff lost the exact profile-bound Codex ACP plan: %#v", factoryHandoff.Start.CodexACP)
	}

	// The credential is process-local test setup, not wrapper-plan data.
	t.Setenv("OPENAI_API_KEY", "fixture-codex-api-key")
	raw, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "fixture-codex-api-key") || strings.Contains(string(raw), "OPENAI_API_KEY=") {
		t.Fatalf("wrapper request persisted a selected credential: %s", raw)
	}
	var detached runnerWrapperRequest
	if err := json.Unmarshal(raw, &detached); err != nil {
		t.Fatalf("deserialize detached Codex ACP wrapper request: %v", err)
	}

	// A separately sealed native bundle may be valid in isolation, but it is
	// not an admission for this already leased run. The canonical registered
	// workflow/profile must bind the serialized wrapper plan before it can use
	// any caller-provided bundle fields.
	_, attackerRequest, attackerReceipt := codexACPTestVerifiedBundle(t)
	craftedPlan := *detached.Start.CodexACP
	craftedPlan.BundleRoot = attackerRequest.BundleRoot
	craftedPlan.ManifestPath = attackerRequest.ManifestPath
	craftedPlan.ManifestSHA256 = attackerRequest.ExpectedManifestSHA256
	craftedPlan.BundleReceipt = attackerReceipt
	if _, _, err := craftedPlan.descriptorAndArgv(); err != nil {
		t.Fatalf("attacker fixture must be a separately valid bundle: %v", err)
	}
	detached.Start.CodexACP = &craftedPlan
	if err := validateCodexACPWrapperRequest(detached); err == nil || !strings.Contains(err.Error(), "complete factory-admitted") {
		t.Fatalf("wrapper accepted a valid but non-canonical Codex ACP plan: %v", err)
	}
	detached.Start.CodexACP = &plan

	result, err := runnerWrapperStartChild(context.Background(), detached)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.SessionRef, codexACPSessionRefVersion+":") || result.Capabilities.ResumeSession {
		t.Fatalf("unexpected Codex ACP start result: %#v", result)
	}
	waitForStatusFile(t, wrapper.Start.StatusPath)
	events, err := readText(wrapper.Start.EventSinkPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acp_codex_bundle_verified", "acp_codex_auth_selected", "acp_codex_config_applied", "\"authenticate_called\":false", "\"runner\":\"codex_acp\""} {
		if !strings.Contains(events, want) {
			t.Fatalf("Codex ACP vertical receipt omitted %q:\n%s", want, events)
		}
	}
	for _, forbidden := range []string{"fixture-codex-api-key", "session/load", "session/resume", "\"authenticate_called\":true"} {
		if strings.Contains(events, forbidden) {
			t.Fatalf("Codex ACP vertical leaked or widened %q:\n%s", forbidden, events)
		}
	}
}

func TestCodexACPProviderPlanRejectsCorruptReceiptWithoutPanic(t *testing.T) {
	plan := CodexACPProviderPlan{
		Schema:              codexACPProviderPlanSchema,
		BundleRoot:          t.TempDir(),
		ManifestPath:        "manifest.json",
		ManifestSHA256:      "sha256:" + strings.Repeat("a", 64),
		AdapterVersion:      "1.1.14",
		AuthSource:          string(CodexACPAuthOpenAIAPIKey),
		AuthPrincipalSHA256: codexACPTestPrincipalDigest("corrupt-receipt"),
		Mode:                CodexACPModeReadOnly,
		BundleReceipt: ACPAdapterBundleVerificationReceipt{
			Schema: ACPAdapterBundleVerificationSchema,
		},
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("corrupt Codex ACP receipt panicked: %v", recovered)
		}
	}()
	if _, _, err := plan.descriptorAndArgv(); err == nil || !strings.Contains(err.Error(), "no verified bundle receipt") {
		t.Fatalf("corrupt Codex ACP receipt error=%v, want fail-closed receipt rejection", err)
	}
}

func registerCodexACPWrapperAdmission(t *testing.T, store *RuntimeStore, wrapper runnerWrapperRequest, definition RunnerDefinition) {
	t.Helper()
	vault := wrapper.Start.VaultPath
	workflow := fmt.Sprintf(`---
runners:
  codex-acp:
    kind: %q
    bundle_root: %q
    manifest_path: %q
    manifest_sha256: %q
    adapter_version: %q
    auth_source: %q
    auth_principal_sha256: %q
---
Codex ACP wrapper fixture.
`, definition.Kind, definition.BundleRoot, definition.ManifestPath, definition.ManifestSHA256, definition.AdapterVersion, definition.AuthSource, definition.AuthPrincipalSHA256)
	if err := writeText(workflowPath(vault), workflow); err != nil {
		t.Fatalf("write canonical Codex ACP workflow: %v", err)
	}
	config := fmt.Sprintf(`automation:
  profiles:
    codex-read-only:
      harness: %q
      model: %q
      effort: %q
      permission_preset: read-only
      sandbox:
        mode: read-only
        network: false
      subagents:
        allowed: false
        max_concurrent: 0
`, RunnerCodexACP, "gpt-5.3-codex", "high")
	if err := writeText(managedTuskerConfigPath(vault), config); err != nil {
		t.Fatalf("write canonical Codex ACP profile: %v", err)
	}
	if _, err := loadWorkflow(vault); err != nil {
		t.Fatalf("load canonical Codex ACP workflow fixture: %v", err)
	}
	if err := store.UpsertProject(RegisteredProject{
		ProjectID:    wrapper.Start.ProjectID,
		ProjectKey:   "codex-acp-fixture",
		Name:         "Codex ACP fixture",
		RepoRoot:     wrapper.Start.RepoRoot,
		VaultRoot:    vault,
		WorkflowPath: workflowPath(vault),
		Enabled:      true,
		Health:       projectHealthHealthy,
	}); err != nil {
		t.Fatalf("register canonical Codex ACP project: %v", err)
	}
}

func TestACPRunnerConcreteIdentityIsDistinct(t *testing.T) {
	if (&ACPRunner{runner: RunnerCodexACP}).Name() != RunnerCodexACP {
		t.Fatal("concrete Codex ACP transport lost its persisted runner identity")
	}
	if (&ACPRunner{}).Name() != RunnerACP {
		t.Fatal("generic ACP transport identity changed")
	}
}

func TestCodexACPWorkflowAdmissionRejectsSecretsAndMalformedIdentity(t *testing.T) {
	valid := RunnerDefinition{
		Kind:                string(RunnerCodexACP),
		BundleRoot:          "/opt/tusker/acp/codex",
		ManifestPath:        "manifest.json",
		ManifestSHA256:      "sha256:" + strings.Repeat("a", 64),
		AdapterVersion:      "1.1.14",
		AuthSource:          string(CodexACPAuthChatGPTSession),
		AuthPrincipalSHA256: "sha256:" + strings.Repeat("b", 64),
	}
	if err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-acp": valid}}, "workflow.yaml"); err != nil {
		t.Fatalf("valid codex ACP admission was rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*RunnerDefinition)
		want   string
	}{
		{name: "raw secret", mutate: func(d *RunnerDefinition) { d.AuthPrincipalSHA256 = "sk-secret-must-not-persist" }, want: "non-secret canonical sha256"},
		{name: "unknown auth", mutate: func(d *RunnerDefinition) { d.AuthSource = "browser-magic" }, want: "auth_source is unsupported"},
		{name: "absolute manifest", mutate: func(d *RunnerDefinition) { d.ManifestPath = "/tmp/manifest.json" }, want: "bundle-relative"},
		{name: "relative root", mutate: func(d *RunnerDefinition) { d.BundleRoot = "bundles/codex" }, want: "canonical absolute"},
		{name: "bad manifest digest", mutate: func(d *RunnerDefinition) { d.ManifestSHA256 = "sha256:abc" }, want: "canonical sha256"},
		{name: "shell command", mutate: func(d *RunnerDefinition) { d.Command = "npx codex-acp" }, want: "does not accept command"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := valid
			test.mutate(&definition)
			err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-acp": definition}}, "workflow.yaml")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err=%v, want %q", err, test.want)
			}
		})
	}

	direct := RunnerDefinition{Kind: string(RunnerCodexExec), BundleRoot: valid.BundleRoot}
	if err := validateRunnerDefinitions(Workflow{Runners: map[string]RunnerDefinition{"codex-exec": direct}}, "workflow.yaml"); err == nil || !strings.Contains(err.Error(), "codex_acp-only") {
		t.Fatalf("direct runner accepted ACP-only admission fields: %v", err)
	}
}
