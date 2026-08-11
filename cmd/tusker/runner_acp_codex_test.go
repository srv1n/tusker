package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tusker/internal/acp"
)

func TestCodexACPDescriptorPinsNativeAdapterAndDeclaredAssets(t *testing.T) {
	descriptor, request, receipt := codexACPTestVerifiedBundle(t)
	argv, err := descriptor.LaunchArgv(request, receipt)
	if err != nil {
		t.Fatal(err)
	}
	wantAdapter, err := codexACPManifestPath(descriptor.Adapter.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 1 || argv[0] != wantAdapter {
		t.Fatalf("argv=%q, want exact absolute native adapter", argv)
	}
	if !filepath.IsAbs(argv[0]) || strings.Contains(strings.Join(argv, " "), "npx") {
		t.Fatalf("descriptor allowed a non-direct launch: %q", argv)
	}
	unsealACPAdapterBundle(t, request.BundleRoot)
	if err := os.WriteFile(descriptor.Assets[0].Path, []byte("changed runtime asset"), 0o600); err != nil {
		t.Fatal(err)
	}
	sealACPAdapterBundle(t, request.BundleRoot)
	if _, err := descriptor.LaunchArgv(request, receipt); err == nil || !strings.Contains(err.Error(), "fingerprint drift") {
		t.Fatalf("mutable declared asset was accepted: %v", err)
	}
}

func TestCodexACPDescriptorRejectsShellShebangAndSymlinkLaunchers(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	if err := os.WriteFile(descriptor.Adapter.Path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	adapterFP, err := acpExecutableFingerprint(descriptor.Adapter.Path)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.Adapter.Fingerprint = adapterFP
	descriptor.ManifestFingerprint, err = descriptor.ManifestFingerprintForDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if err := descriptor.Validate(); err == nil || !strings.Contains(err.Error(), "shebang") {
		t.Fatalf("shebang adapter accepted: %v", err)
	}

	descriptor = codexACPTestDescriptor(t)
	link := filepath.Join(t.TempDir(), "codex-acp")
	if err := os.Symlink(descriptor.Adapter.Path, link); err != nil {
		t.Fatal(err)
	}
	linkFP, err := acpExecutableFingerprint(descriptor.Adapter.Path)
	if err != nil {
		t.Fatal(err)
	}
	descriptor.Adapter = CodexACPImmutableAsset{Path: link, Fingerprint: linkFP}
	descriptor.ManifestFingerprint, err = descriptor.ManifestFingerprintForDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if err := descriptor.Validate(); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink adapter accepted: %v", err)
	}
}

func TestCodexACPModeAndPositiveEnvironmentMapping(t *testing.T) {
	if got, err := CodexACPModeForPermissionPreset("read-only"); err != nil || got != CodexACPModeReadOnly {
		t.Fatalf("read-only mode=%q err=%v", got, err)
	}
	for _, preset := range []string{"workspace-write-network", "workspace-write-offline"} {
		if got, err := CodexACPModeForPermissionPreset(preset); err != nil || got != CodexACPModeWorkspaceWrite {
			t.Fatalf("workspace mode for %q=%q err=%v", preset, got, err)
		}
	}
	for _, blocked := range []string{"danger-full-access"} {
		if _, err := CodexACPModeForPermissionPreset(blocked); err == nil {
			t.Fatalf("unsafe permission preset %q was admitted", blocked)
		}
	}
	if _, err := CodexACPModeForPermissionPreset("unbounded-yolo"); err == nil {
		t.Fatal("unknown permission preset was mapped")
	}
	descriptor := codexACPTestDescriptor(t)
	descriptor.Mode = CodexACPModeWorkspaceWrite
	if err := descriptor.Validate(); err != nil {
		t.Fatalf("workspace-write descriptor rejected: %v", err)
	}
	descriptor.Mode = CodexACPModeFullAccess
	if err := descriptor.Validate(); err == nil || !strings.Contains(err.Error(), "read-only or workspace-write") {
		t.Fatalf("full-access descriptor bypassed admission: %v", err)
	}

	descriptor = codexACPTestDescriptor(t)
	principal := codexACPTestPrincipalDigest("account-1")
	environment, err := descriptor.CodexACPEnvironment([]string{
		"HOME=/users/test", "OPENAI_API_KEY=fixture-key", "CODEX_API_KEY=must-not-forward",
		"CODEX_HOME=/must-not-forward", "CODEX_PATH=/must-not-forward", "TUSKER_STATE_ROOT=/not-forwarded",
		"PATH=/attacker/bin", "CODEX_CONFIG={\"provider\":\"attacker\"}", "UNRELATED=value",
	}, CodexACPAuthContract{Source: CodexACPAuthOpenAIAPIKey, PrincipalDigest: principal})
	if err != nil {
		t.Fatal(err)
	}
	values := environmentValues(t, environment.Variables)
	if values["PATH"] != codexACPDefaultFixedPath || values["OPENAI_API_KEY"] != "fixture-key" {
		t.Fatalf("environment was not positive and fixed: %#v", values)
	}
	for _, key := range []string{"HOME", "CODEX_API_KEY", "CODEX_HOME", "CODEX_PATH", "TUSKER_STATE_ROOT", "UNRELATED"} {
		if _, exists := values[key]; exists {
			t.Fatalf("environment leaked %s: %#v", key, values)
		}
	}
	if environment.Auth.Method != string(CodexACPAuthOpenAIAPIKey) || environment.Auth.PrincipalDigest != principal {
		t.Fatalf("non-secret auth receipt=%#v", environment.Auth)
	}
	if values["INITIAL_AGENT_MODE"] != "read-only" {
		t.Fatalf("initial agent mode=%q", values["INITIAL_AGENT_MODE"])
	}
	wantConfig := `{"model":"gpt-5.3-codex","model_reasoning_effort":"high","allow_login_shell":false,"shell_environment_policy":{"inherit":"core","ignore_default_excludes":false,"exclude":["OPENAI_API_KEY","CODEX_API_KEY","CODEX_HOME"],"experimental_use_profile":false}}`
	if values["CODEX_CONFIG"] != wantConfig {
		t.Fatalf("CODEX_CONFIG=%s, want exact Codex 0.147.0 shell isolation policy %s", values["CODEX_CONFIG"], wantConfig)
	}
}

func TestCodexACPEnvironmentForwardsExactlySelectedAuth(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	codexHome := t.TempDir()
	resolvedCodexHome, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		auth     CodexACPAuthContract
		selected string
		value    string
	}{
		{name: "OpenAI API key", auth: CodexACPAuthContract{Source: CodexACPAuthOpenAIAPIKey, PrincipalDigest: codexACPTestPrincipalDigest("openai")}, selected: "OPENAI_API_KEY", value: "openai-secret"},
		{name: "Codex API key", auth: CodexACPAuthContract{Source: CodexACPAuthCodexAPIKey, PrincipalDigest: codexACPTestPrincipalDigest("codex")}, selected: "CODEX_API_KEY", value: "codex-secret"},
		{name: "ChatGPT session", auth: CodexACPAuthContract{Source: CodexACPAuthChatGPTSession, PrincipalDigest: codexACPTestPrincipalDigest("chatgpt")}, selected: "CODEX_HOME", value: resolvedCodexHome},
	} {
		t.Run(test.name, func(t *testing.T) {
			environment, err := descriptor.CodexACPEnvironment([]string{
				"OPENAI_API_KEY=openai-secret", "CODEX_API_KEY=codex-secret", "CODEX_HOME=" + codexHome,
			}, test.auth)
			if err != nil {
				t.Fatal(err)
			}
			values := environmentValues(t, environment.Variables)
			for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_HOME"} {
				got, exists := values[key]
				if key == test.selected {
					if !exists || got != test.value {
						t.Fatalf("selected adapter auth %s=%q exists=%t, want %q", key, got, exists, test.value)
					}
				} else if exists {
					t.Fatalf("unselected adapter auth %s leaked as %q", key, got)
				}
			}
		})
	}
}

func TestCodexACPNetworkPolicyBindingIsExact(t *testing.T) {
	for _, test := range []struct {
		name     string
		policy   CodexPolicy
		expected bool
		wantErr  bool
	}{
		{name: "offline explicit", policy: CodexPolicy{TurnSandboxNetwork: boolPtr(false)}, expected: false},
		{name: "offline default", policy: CodexPolicy{}, expected: false},
		{name: "offline request widened", policy: CodexPolicy{TurnSandboxNetwork: boolPtr(true)}, expected: false, wantErr: true},
		{name: "network request omitted", policy: CodexPolicy{}, expected: true, wantErr: true},
		{name: "network explicit", policy: CodexPolicy{TurnSandboxNetwork: boolPtr(true)}, expected: true},
		{name: "network request narrowed", policy: CodexPolicy{TurnSandboxNetwork: boolPtr(false)}, expected: true, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateCodexACPNetworkBinding(test.policy, test.expected)
			if (err != nil) != test.wantErr {
				t.Fatalf("network binding error=%v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestCodexACPDescriptorRejectsLaunchKindArgvShapeWithoutPanic(t *testing.T) {
	descriptor, request, receipt := codexACPTestVerifiedBundle(t)
	plan := CodexACPProviderPlan{
		Schema:              codexACPProviderPlanSchema,
		BundleRoot:          request.BundleRoot,
		ManifestPath:        request.ManifestPath,
		ManifestSHA256:      request.ExpectedManifestSHA256,
		AdapterVersion:      descriptor.AdapterVersion,
		AdapterLaunchKind:   ACPAdapterBundleLaunchInterpreter,
		AuthSource:          string(CodexACPAuthOpenAIAPIKey),
		AuthPrincipalSHA256: codexACPTestPrincipalDigest("malformed-argv"),
		Mode:                CodexACPModeReadOnly,
		BundleReceipt:       receipt,
	}
	plan.BundleReceipt.Argv = []string{receipt.Argv[0]}
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("malformed launch argv panicked: %v", recovered)
		}
	}()
	if _, err := plan.descriptor(); err == nil || !strings.Contains(err.Error(), "launch receipt") {
		t.Fatalf("malformed interpreter argv was not rejected: %v", err)
	}
}

func TestCodexACPEnvironmentRequiresExactlyOneSelectedAuthPath(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	principal := codexACPTestPrincipalDigest("account-1")
	if _, err := descriptor.CodexACPEnvironment([]string{"OPENAI_API_KEY=one", "OPENAI_API_KEY=two"}, CodexACPAuthContract{Source: CodexACPAuthOpenAIAPIKey, PrincipalDigest: principal}); err == nil {
		t.Fatal("duplicated selected auth source was accepted")
	}
	if _, err := descriptor.CodexACPEnvironment([]string{"CODEX_API_KEY=wrong-source"}, CodexACPAuthContract{Source: CodexACPAuthOpenAIAPIKey, PrincipalDigest: principal}); err == nil {
		t.Fatal("missing selected auth source fell back to another key")
	}
	codexHome := t.TempDir()
	result, err := descriptor.CodexACPEnvironment([]string{"CODEX_HOME=" + codexHome, "OPENAI_API_KEY=must-not-forward"}, CodexACPAuthContract{Source: CodexACPAuthChatGPTSession, PrincipalDigest: principal})
	if err != nil {
		t.Fatal(err)
	}
	values := environmentValues(t, result.Variables)
	if values["CODEX_HOME"] == "" || values["OPENAI_API_KEY"] != "" || result.Auth.Method != string(CodexACPAuthChatGPTSession) {
		t.Fatalf("ChatGPT auth selection leaked/fell back: env=%#v auth=%#v", values, result.Auth)
	}
}

func TestCodexACPConfigPlanRequiresAdvertisedAndVerifiedOptions(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	plan, err := descriptor.ConfigPlan([]CodexACPAdvertisedConfigOption{
		{ID: "model", AllowedValues: []string{"gpt-5.3-codex", "gpt-5.2-codex"}},
		{ID: "reasoning_effort", AllowedValues: []string{"low", "high"}},
		{ID: "mode", AllowedValues: []string{"read-only", "agent", "agent-full-access"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("plan=%#v", plan)
	}
	applied := make([]CodexACPAdvertisedConfigOption, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		applied = append(applied, CodexACPAdvertisedConfigOption{ID: step.OptionID, Value: step.Value})
	}
	if err := plan.VerifyApplied(applied); err != nil {
		t.Fatal(err)
	}
	applied[2].Value = "agent"
	if err := plan.VerifyApplied(applied); err == nil || !strings.Contains(err.Error(), "not applied exactly") {
		t.Fatalf("unsafe mode fallback was accepted: %v", err)
	}
	if _, err := descriptor.ConfigPlan([]CodexACPAdvertisedConfigOption{{ID: "model", AllowedValues: []string{"gpt-5.3-codex"}}, {ID: "mode", AllowedValues: []string{"agent"}}}); err == nil {
		t.Fatal("missing reasoning option was accepted")
	}
	if _, err := descriptor.ConfigPlan([]CodexACPAdvertisedConfigOption{
		{ID: "model", AllowedValues: []string{"gpt-5.3-codex"}}, {ID: "model_reasoning_effort", AllowedValues: []string{"high"}},
		{ID: "mode", AllowedValues: []string{"read-only"}},
	}); err == nil {
		t.Fatal("unofficial reasoning option alias was accepted")
	}
	if _, err := descriptor.ConfigPlan([]CodexACPAdvertisedConfigOption{
		{ID: "model", AllowedValues: []string{"gpt-5.2-codex"}}, {ID: "reasoning_effort", AllowedValues: []string{"low"}}, {ID: "mode", AllowedValues: []string{"agent"}},
	}); err == nil {
		t.Fatal("unadvertised model/mode/effort values were accepted")
	}
}

func TestCodexACPPermissionNormalizeExecuteEditAndOtherFailClosed(t *testing.T) {
	workspace := t.TempDir()
	base := acp.PermissionRequest{
		SessionID: "session-1", ToolCallID: "tool-1",
		Options: []acp.PermissionOption{{ID: "allow_once", Kind: "allow_once"}},
	}
	policy := ACPPermissionPolicy{
		AllowedToolKinds: map[string]bool{"read": true, "write": true, "execute": true, "network": true},
		BudgetAuthorized: true, AllowWorkspaceWrite: true, AllowExecute: true, AllowNetwork: true,
	}
	for _, test := range []struct {
		name       string
		raw        string
		wantKind   string
		want       ACPPermissionOutcome
		wantReason string
	}{
		// Public Codex ACP edit callbacks contain no safely scoped file target;
		// they normalize to write, then the generic broker rejects the missing
		// target instead of fabricating one from raw arguments.
		{name: "official edit shape binds workspace", raw: `{"sessionId":"session-1","toolCall":{"toolCallId":"tool-1","kind":"edit","rawInput":{}},"options":[],"_meta":{}}`, wantKind: "write", want: ACPPermissionAllowOnce, wantReason: ACPPermissionReasonAllowed},
		{name: "official execute shape binds cwd", raw: fmt.Sprintf(`{"sessionId":"session-1","toolCall":{"toolCallId":"tool-1","kind":"execute","rawInput":{"command":"git status","cwd":%q}},"options":[],"_meta":{}}`, workspace), wantKind: "execute", want: ACPPermissionAllowOnce, wantReason: ACPPermissionReasonAllowed},
		{name: "official network request remains policy-bound", raw: `{"sessionId":"session-1","toolCall":{"toolCallId":"tool-1","kind":"other","rawInput":{}},"options":[],"_meta":{"codex":{"params":{"permissions":{"network":{"enabled":true}}}}}}`, wantKind: "network", want: ACPPermissionAllowOnce, wantReason: ACPPermissionReasonAllowed},
		{name: "official other shape remains non-authorizing", raw: `{"sessionId":"session-1","toolCall":{"toolCallId":"tool-1","kind":"other","rawInput":{}},"options":[],"_meta":{}}`, wantKind: "other", want: ACPPermissionReject, wantReason: ACPPermissionReasonInvalidRequest},
		{name: "mismatched official identity is other", raw: `{"sessionId":"session-1","toolCall":{"toolCallId":"forged","kind":"edit","rawInput":{}},"options":[],"_meta":{}}`, wantKind: "other", want: ACPPermissionReject, wantReason: ACPPermissionReasonInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := base
			req.Raw = []byte(test.raw)
			normalized := DecodeCodexACPPermission(req)
			broker := normalized.BrokerRequest("attempt-1", "session-1", workspace, false)
			if broker.ToolKind != test.wantKind {
				t.Fatalf("kind=%q, want %q", broker.ToolKind, test.wantKind)
			}
			decision := EvaluateACPPermission(broker, policy)
			if decision.Outcome != test.want || decision.ReasonCode != test.wantReason {
				t.Fatalf("decision=%#v", decision)
			}
		})
	}
}

func TestCodexACPSessionReferenceBindsProviderVersionAndManifest(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	binding := codexACPTestAuthorityBinding(t)
	encoded, err := descriptor.EncodeSessionRef("provider-session-1", binding)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := descriptor.DecodeSessionRef(encoded, binding)
	if err != nil || decoded.Provider != codexACPProvider || decoded.ProviderSessionID != "provider-session-1" {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
	other := descriptor
	other.AdapterVersion = "0.66.1"
	other.ManifestFingerprint, err = other.ManifestFingerprintForDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.DecodeSessionRef(encoded, binding); err == nil {
		t.Fatal("session ref survived a version/manifest change")
	}
	if _, err := descriptor.DecodeSessionRef("tusker.codex-acp-session/v1:not-base64", binding); err == nil {
		t.Fatal("malformed session ref decoded")
	}
}

func TestCodexACPSessionReferenceRejectsCrossAuthorityCopies(t *testing.T) {
	descriptor := codexACPTestDescriptor(t)
	binding := codexACPTestAuthorityBinding(t)
	encoded, err := descriptor.EncodeSessionRef("provider-session-1", binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*CodexACPAuthorityBinding)
	}{
		{name: "project", mutate: func(b *CodexACPAuthorityBinding) { b.ProjectID = "project-2" }},
		{name: "workspace", mutate: func(b *CodexACPAuthorityBinding) { b.WorkspacePath = t.TempDir() }},
		{name: "profile", mutate: func(b *CodexACPAuthorityBinding) { b.RunnerProfile = "other-profile" }},
		{name: "principal", mutate: func(b *CodexACPAuthorityBinding) { b.AuthPrincipalDigest = codexACPTestPrincipalDigest("account-2") }},
		{name: "attempt", mutate: func(b *CodexACPAuthorityBinding) { b.OriginAttemptID = "attempt-2" }},
		{name: "work revision", mutate: func(b *CodexACPAuthorityBinding) { b.WorkRevision++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			expected := binding
			test.mutate(&expected)
			if _, err := descriptor.DecodeSessionRef(encoded, expected); err == nil {
				t.Fatal("session reference copied across authority context")
			}
		})
	}
}

func TestCodexACPStopDeliveryUnknownForbidsAutomaticRecovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		result acp.PromptResult
		err    error
	}{
		{name: "explicit", result: acp.PromptResult{Outcome: acp.OutcomeDeliveryUnknown, Delivery: acp.DeliveryWriteComplete}, err: acp.ErrDeliveryUnknown},
		{name: "contradictory error", result: acp.PromptResult{Outcome: acp.OutcomeCompleted, Delivery: acp.DeliveryWriteComplete}, err: acp.ErrDeliveryUnknown},
		{name: "completed without terminal", result: acp.PromptResult{Outcome: acp.OutcomeCompleted, Delivery: acp.DeliveryWriteComplete}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stop := CodexACPMapStop(test.result, test.err)
			if stop.Outcome != AttemptOutcomeFailed || stop.AutoRetry || stop.AutoResume || stop.DeliveryKnown || !strings.Contains(stop.Reason, "no automatic retry or resume") {
				t.Fatalf("delivery unknown stop=%#v", stop)
			}
		})
	}
	completed := CodexACPMapStop(acp.PromptResult{Outcome: acp.OutcomeCompleted, Delivery: acp.DeliveryTerminalReceived}, nil)
	if completed.Outcome != AttemptOutcomeNone || !completed.DeliveryKnown {
		t.Fatalf("completed stop=%#v", completed)
	}
}

func codexACPTestVerifiedBundle(t *testing.T) (CodexACPDescriptor, ACPAdapterBundleValidationRequest, ACPAdapterBundleVerificationReceipt) {
	t.Helper()
	request, manifest := newACPAdapterBundleFixture(t)
	unsealACPAdapterBundle(t, request.BundleRoot)
	physicalRoot, err := filepath.EvalSymlinks(request.BundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	request.BundleRoot = physicalRoot
	request.ExpectedFinalRoot = physicalRoot
	if err := os.Remove(filepath.Join(request.BundleRoot, "adapter.js")); err != nil {
		t.Fatal(err)
	}
	adapterPath := filepath.Join(physicalRoot, "bin", "node")
	codexAdapterPath := filepath.Join(physicalRoot, "bin", "codex-acp")
	if err := os.Rename(adapterPath, codexAdapterPath); err != nil {
		t.Fatal(err)
	}
	manifest.Provider, manifest.Adapter, manifest.Version = codexACPProvider, "codex-acp", "1.1.14"
	manifest.Argv = []string{codexAdapterPath}
	manifest.Assets = []ACPAdapterBundleAsset{
		{Path: "bin/codex-acp", SHA256: testACPAdapterBundleFileDigest(t, codexAdapterPath), Role: "executable"},
		{Path: "lib/runtime.js", SHA256: testACPAdapterBundleFileDigest(t, filepath.Join(physicalRoot, "lib", "runtime.js")), Role: "asset"},
	}
	request.ExpectedDescriptor = ACPAdapterBundleDescriptorPolicy{Provider: codexACPProvider, Adapter: "codex-acp", Version: manifest.Version, LaunchKind: ACPAdapterBundleLaunchNative}
	writeACPAdapterBundleManifest(t, &request, manifest)
	sealACPAdapterBundle(t, request.BundleRoot)
	receipt, err := ValidateACPAdapterBundle(request)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := CodexACPDescriptor{
		AdapterVersion: manifest.Version,
		Adapter:        CodexACPImmutableAsset{Path: codexAdapterPath, Fingerprint: manifest.Assets[0].SHA256},
		Assets:         []CodexACPImmutableAsset{{Path: filepath.Join(physicalRoot, "lib", "runtime.js"), Fingerprint: manifest.Assets[1].SHA256}},
		Model:          "gpt-5.3-codex",
		Effort:         "high",
		Mode:           CodexACPModeReadOnly,
	}
	descriptor.ManifestFingerprint, err = descriptor.ManifestFingerprintForDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	return descriptor, request, receipt
}

func TestCodexACPObservationBridgeUsesRawProviderSessionWithoutRunMutation(t *testing.T) {
	store := executionLedgerStore(t)
	defer store.Close()
	parent, err := store.CreateDirectExecution(DirectExecutionInput{ProjectID: "project-1", Source: "codex_acp", Provider: "codex", Creator: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AttachExecution(ExecutionAttachmentInput{
		ProjectID: "project-1", ExecutionID: parent.ExecutionID, Provider: "codex", ProviderSessionID: "provider-session-1", SessionRef: "prior-local-ref", Actor: "operator",
	}); err != nil {
		t.Fatal(err)
	}
	descriptor := codexACPTestDescriptor(t)
	binding := codexACPTestAuthorityBinding(t)
	binding.ProjectID = "project-1"
	sessionRef, err := descriptor.EncodeSessionRef("provider-session-1", binding)
	if err != nil {
		t.Fatal(err)
	}
	run := RunStatus{ProjectID: "project-1", Runner: string(codexACPRunnerName), RunnerProfile: binding.RunnerProfile, WorkspacePath: binding.WorkspacePath, SessionRef: sessionRef}
	changed, err := descriptor.ObserveCodexACPUpdate(store, run, binding, acp.Update{
		Method: "session/update", Params: []byte(`{"child_id":"child-1","agent_type":"reviewer","status":"running","timestamp":"2026-08-11T10:00:00Z"}`),
	}, 1)
	if err != nil || !changed {
		t.Fatalf("changed=%t err=%v", changed, err)
	}
	if run.SessionRef != sessionRef {
		t.Fatalf("observation mutated caller run: %#v", run)
	}
	var runs int
	if err := store.queryRowScan(`SELECT COUNT(*) FROM runs`, nil, &runs); err != nil || runs != 0 {
		t.Fatalf("provider observation changed runtime runs=%d err=%v", runs, err)
	}
	var rawSession string
	if err := store.queryRowScan(`SELECT parent_provider_session_id FROM provider_execution_observations WHERE child_handle = 'child-1'`, nil, &rawSession); err != nil || rawSession != "provider-session-1" {
		t.Fatalf("provider correlation=%q err=%v", rawSession, err)
	}
}

func TestCodexACPReadinessSeparatesAuthFromTaskAdmission(t *testing.T) {
	requirements := CodexACPReadinessRequirements()
	joined := ""
	for _, requirement := range requirements {
		if !requirement.Required || requirement.ID == "" || requirement.Description == "" {
			t.Fatalf("invalid readiness requirement: %#v", requirement)
		}
		joined += requirement.ID + " "
	}
	for _, want := range []string{"adapter_manifest", "acp_conformance", "codex_auth", "task_authorization", "permission_parity"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("readiness misses %q: %s", want, joined)
		}
	}
}

func codexACPTestDescriptor(t *testing.T) CodexACPDescriptor {
	t.Helper()
	dir := t.TempDir()
	adapter := codexACPWriteAsset(t, dir, "codex-acp", []byte("pinned native codex-acp fixture\n"), 0o700)
	asset := codexACPWriteAsset(t, dir, "runtime.json", []byte(`{"adapter":"codex-acp"}`), 0o600)
	descriptor := CodexACPDescriptor{
		AdapterVersion: "0.66.0",
		Adapter:        adapter,
		Assets:         []CodexACPImmutableAsset{asset},
		Model:          "gpt-5.3-codex",
		Effort:         "high",
		Mode:           CodexACPModeReadOnly,
	}
	var err error
	descriptor.ManifestFingerprint, err = descriptor.ManifestFingerprintForDescriptor()
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func codexACPTestAuthorityBinding(t *testing.T) CodexACPAuthorityBinding {
	t.Helper()
	return CodexACPAuthorityBinding{
		ProjectID: "project-1", WorkspacePath: t.TempDir(), RunnerProfile: "codex-read-only",
		AuthPrincipalDigest: codexACPTestPrincipalDigest("account-1"), OriginAttemptID: "attempt-1", WorkRevision: 7,
	}
}

func codexACPTestPrincipalDigest(identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func codexACPWriteAsset(t *testing.T, dir, name string, contents []byte, mode os.FileMode) CodexACPImmutableAsset {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := acpExecutableFingerprint(path)
	if err != nil {
		t.Fatal(err)
	}
	return CodexACPImmutableAsset{Path: path, Fingerprint: fingerprint}
}

func environmentValues(t *testing.T, env []string) map[string]string {
	t.Helper()
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			t.Fatalf("malformed environment entry %q", entry)
		}
		if _, exists := values[key]; exists {
			t.Fatalf("duplicate environment key %q", key)
		}
		values[key] = value
	}
	return values
}
