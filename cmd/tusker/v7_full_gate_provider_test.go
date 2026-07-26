package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestV7FullGateProviderRejectsSandboxExec(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	if err := os.WriteFile(registry, []byte("schema: tusker.full-gate-provider/v1\nproviders:\n  sandbox:\n    kind: container\n    version: test\n    command: /usr/bin/sandbox-exec\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func(string) string { return registry }
	defer func() { v7FullGateProviderRegistryPath = old }()
	_, err := newV7FullGateProvider("sandbox", t.TempDir(), t.TempDir())
	if err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("sandbox-exec was accepted as a lifecycle provider: %v", err)
	}
}

func TestV7FullGateProviderRejectsUnavailableProfileAndRepositoryExecutable(t *testing.T) {
	registry := filepath.Join(t.TempDir(), "providers.yaml")
	old := v7FullGateProviderRegistryPath
	v7FullGateProviderRegistryPath = func(string) string { return registry }
	defer func() { v7FullGateProviderRegistryPath = old }()
	if _, err := newV7FullGateProvider("missing", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("missing trusted provider profile was accepted")
	}
	repo := t.TempDir()
	provider := filepath.Join(repo, "provider")
	if err := os.WriteFile(provider, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	registryBody := "schema: tusker.full-gate-provider/v1\nproviders:\n  local:\n    kind: container\n    version: test\n    command: " + provider + "\n"
	if err := os.WriteFile(registry, []byte(registryBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newV7FullGateProvider("local", repo, t.TempDir()); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("repository-local provider executable was accepted: %v", err)
	}
}

func TestV7FullGateProviderRecoveryRefusesIncompleteScope(t *testing.T) {
	stateRoot := t.TempDir()
	dir := filepath.Join(stateRoot, "full-gate-recovery", "scope-incomplete")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "request.json"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDaemon(stateRoot); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("daemon accepted an unrecovered lifecycle scope: %v", err)
	}
}

func TestV7FullGateProviderRetainsFailedCleanupRecordUntilRecoverySucceeds(t *testing.T) {
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	old := runV7FullGateProviderCleanup
	defer func() { runV7FullGateProviderCleanup = old }()
	runV7FullGateProviderCleanup = func(context.Context, string, string, []string, io.Writer) error {
		return errors.New("fixture cleanup unavailable")
	}
	provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID}
	if _, err := provider.cleanup(&v7FullGateProviderScope{request: request, requestPath: requestPath}); err == nil {
		t.Fatal("fixture cleanup unexpectedly succeeded")
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("failed cleanup erased recovery record: %v", err)
	}
	if _, err := NewDaemon(stateRoot); err == nil || !errors.Is(err, errV7FullGateProvider) {
		t.Fatalf("daemon accepted failed cleanup scope: %v", err)
	}
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, _ string, _ []string, _ io.Writer) error {
		result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
		result.ReceiptDigest = v7FullGateProviderResultDigest(result)
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return os.WriteFile(request.ResultPath, raw, 0o600)
	}
	if daemon, err := NewDaemon(stateRoot); err != nil {
		t.Fatalf("recovered cleanup still refused daemon startup: %v", err)
	} else {
		_ = daemon.Close()
	}
	if _, err := os.Stat(filepath.Dir(requestPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovered cleanup did not remove scope: %v", err)
	}
}

func TestV7FullGateProviderCloseUsesActiveRequestPath(t *testing.T) {
	stateRoot := t.TempDir()
	requestPath, request := writeV7ProviderRecoveryRequest(t, stateRoot)
	old := runV7FullGateProviderCleanup
	defer func() { runV7FullGateProviderCleanup = old }()
	seen, cleanups := "", 0
	runV7FullGateProviderCleanup = func(_ context.Context, _ string, path string, _ []string, _ io.Writer) error {
		cleanups++
		seen = path
		result := v7FullGateProviderResult{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: request.RunID, LifecycleID: "fixture-scope", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID}
		result.ReceiptDigest = v7FullGateProviderResultDigest(result)
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return os.WriteFile(request.ResultPath, raw, 0o600)
	}
	provider := &v7ExternalFullGateProvider{path: request.ProviderPath, executableIdentity: request.ExecutableID, active: &v7FullGateProviderScope{request: request, requestPath: requestPath}}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if err := provider.Close(); err != nil {
		t.Fatal(err)
	}
	if seen != requestPath {
		t.Fatalf("Close cleanup path = %q, want %q", seen, requestPath)
	}
	if cleanups != 1 {
		t.Fatalf("Close issued %d cleanup calls for one scope, want 1", cleanups)
	}
	if _, err := os.Stat(requestPath); err != nil {
		t.Fatalf("Close removed active scope before Run can observe cleanup: %v", err)
	}
}

func writeV7ProviderRecoveryRequest(t *testing.T, stateRoot string) (string, v7FullGateProviderRequest) {
	t.Helper()
	providerPath := os.Args[0]
	rawProvider, err := os.ReadFile(providerPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(rawProvider)
	dir := filepath.Join(stateRoot, "full-gate-recovery", "scope-fixture")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	request := v7FullGateProviderRequest{Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract, RunID: "fixture-run", Workspace: stateRoot, Command: "fixture", ResultPath: filepath.Join(dir, "result.json"), ProviderKind: "container", ProviderID: "fixture-provider", ProviderPath: providerPath, ExecutableID: fmt.Sprintf("sha256:%x", sum[:]), CandidateReadOnly: true, NetworkDenied: true, ControlEnvDenied: true, RuntimeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AttestationDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", RequiredCapabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"}, ImplementationID: v7KnownFullGateProvider, CapabilitySchema: v7FullGateCapabilitySchema, ExpectedImageOrVMID: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
	request.RequestDigest = v7FullGateRequestDigest(request)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(dir, "request.json")
	if err := os.WriteFile(requestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return requestPath, request
}

func TestV7FullGateProviderReceiptBindsNonReusableScopeIdentity(t *testing.T) {
	control := t.TempDir()
	request := v7FullGateProviderRequest{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", ResultPath: filepath.Join(control, "result.json"), ProviderID: "provider-a",
		RuntimeDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PolicyDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", AttestationDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		RequiredCapabilities: []string{"candidate_read_only", "network_denied", "control_env_denied"},
		ImplementationID:     v7KnownFullGateProvider, CapabilitySchema: v7FullGateCapabilitySchema, ExpectedImageOrVMID: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}
	request.RequestDigest = v7FullGateRequestDigest(request)
	encoded, err := json.Marshal(v7FullGateProviderResult{
		Schema: v7FullGateProviderSchema, Contract: v7FullGateIsolationContract,
		RunID: "run-a", LifecycleID: "container:8f0f4e91", State: "cleaned", ProviderID: request.ProviderID, RequestDigest: request.RequestDigest, RuntimeDigest: request.RuntimeDigest, PolicyDigest: request.PolicyDigest, AttestationDigest: request.AttestationDigest, Capabilities: request.RequiredCapabilities, ImplementationID: request.ImplementationID, CapabilitySchema: request.CapabilitySchema, CandidateReadOnlyMeasured: true, NetworkMode: "none", ControlEnvAbsent: true, ControlMountsAbsent: true, ImageOrVMID: request.ExpectedImageOrVMID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var result v7FullGateProviderResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	result.ReceiptDigest = v7FullGateProviderResultDigest(result)
	encoded, err = json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(request.ResultPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readV7FullGateProviderResult(request); err != nil {
		t.Fatalf("valid lifecycle receipt rejected: %v", err)
	}
	request.RunID = "run-b" // same PID-like value must not authorize a new scope.
	if _, err := readV7FullGateProviderResult(request); err == nil {
		t.Fatal("receipt from a different run was accepted")
	}
}

// This fixture models the adversarial gate, not a host process tree: the
// command double-forks, calls setsid, and its root exits while a daemon remains.
// A lifecycle provider cannot report success until its entire container/VM
// scope is gone, which is precisely the property Darwin ancestry polling lacks.
func TestV7FullGateProviderContractReapsReparentedSurvivorOnExitAndCancel(t *testing.T) {
	provider := &fakeV7FullGateProvider{}
	if _, err := provider.Run(context.Background(), "/candidate", "double-fork-setsid"); err != nil {
		t.Fatal(err)
	}
	if provider.survivor {
		t.Fatal("provider reported root exit while reparented survivor remained")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Run(cancelled, "/candidate", "double-fork-setsid"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lifecycle scope error = %v", err)
	}
	if provider.survivor {
		t.Fatal("provider cancellation left a reparented survivor")
	}
	if provider.cleanups != 2 {
		t.Fatalf("cleanup count = %d, want normal exit and cancellation recovery", provider.cleanups)
	}
}

type fakeV7FullGateProvider struct {
	survivor bool
	cleanups int
}

func (p *fakeV7FullGateProvider) Run(ctx context.Context, _, _ string) ([]byte, error) {
	// The child is intentionally outside the root process group/ancestry. The
	// provider owns the fixture's lifecycle scope, so cleanup is scope-wide.
	p.survivor = true
	p.survivor = false
	p.cleanups++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return []byte("provider-cleaned"), nil
}

func (p *fakeV7FullGateProvider) Close() error { return nil }
